package main

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
)

const (
	introspectTimeout = 8 * time.Second
	maxIntrospectRows = 50000
)

// userError is safe to show to the client. Anything else is replaced with a
// generic message so driver errors can never echo credentials back.
type userError string

func (e userError) Error() string { return string(e) }

type dbTarget struct {
	engine   string
	user     string
	password string
	host     string
	port     string
	database string
	insecure bool // TLS explicitly disabled (needs DB_ALLOW_INSECURE=1)
}

// allowedHosts parses ALLOWED_DB_HOSTS ("host" or "host:port", comma separated).
func allowedHosts() map[string]bool {
	out := map[string]bool{}
	for _, item := range strings.Split(os.Getenv("ALLOWED_DB_HOSTS"), ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			out[item] = true
		}
	}
	return out
}

// parseTarget validates a mysql:// or postgres:// URL. Only the parts we
// rebuild the connection from are kept, so query-string tricks (e.g. host=,
// sslrootcert=, allowAllFiles=) cannot change where or how we connect.
func parseTarget(engine, raw string, allow map[string]bool, allowInsecure bool) (dbTarget, error) {
	var t dbTarget
	if len(allow) == 0 {
		return t, userError("ยังไม่ได้ตั้งค่า ALLOWED_DB_HOSTS บนเซิร์ฟเวอร์ จึงปิดการเชื่อมต่อฐานข้อมูลไว้")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User == nil {
		return t, userError("รูปแบบ connection string ไม่ถูกต้อง ใช้ scheme://user:password@host:port/database")
	}
	switch {
	case engine == "mysql" && u.Scheme == "mysql":
	case engine == "postgres" && (u.Scheme == "postgres" || u.Scheme == "postgresql"):
	case engine == "sqlserver" && u.Scheme == "sqlserver":
	case engine == "mysql" || engine == "postgres" || engine == "sqlserver":
		return t, userError("scheme ของ connection string ไม่ตรงกับระบบฐานข้อมูลที่เลือก")
	default:
		return t, userError("รองรับเฉพาะ MySQL, PostgreSQL และ SQL Server")
	}

	t.engine = engine
	t.user = u.User.Username()
	t.password, _ = u.User.Password()
	t.host = strings.ToLower(u.Hostname())
	t.port = u.Port()
	if t.port == "" {
		t.port = map[string]string{"mysql": "3306", "postgres": "5432", "sqlserver": "1433"}[engine]
	}
	t.database = strings.TrimPrefix(u.Path, "/")
	if t.host == "" || t.user == "" || t.database == "" || strings.Contains(t.database, "/") {
		return t, userError("connection string ต้องมี user, host และชื่อฐานข้อมูล")
	}
	if !allow[t.host] && !allow[net.JoinHostPort(t.host, t.port)] {
		return t, userError("host นี้ไม่อยู่ในรายการที่อนุญาต (ALLOWED_DB_HOSTS)")
	}

	for key, values := range u.Query() {
		value := strings.ToLower(values[0])
		switch {
		case engine == "postgres" && key == "sslmode" && (value == "require" || value == "verify-ca" || value == "verify-full"):
		case engine == "postgres" && key == "sslmode" && value == "disable":
			t.insecure = true
		case engine == "sqlserver" && key == "encrypt" && value == "true":
		case engine == "sqlserver" && key == "encrypt" && value == "disable":
			t.insecure = true
		case engine == "mysql" && key == "tls" && value == "true":
		case engine == "mysql" && key == "tls" && value == "false":
			t.insecure = true
		default:
			return t, userError("พารามิเตอร์ " + key + " ไม่รองรับ (รองรับเฉพาะ sslmode / tls)")
		}
	}
	if t.insecure && !allowInsecure {
		return t, userError("เซิร์ฟเวอร์ไม่อนุญาตให้ปิด TLS (ตั้ง DB_ALLOW_INSECURE=1 หากจำเป็น)")
	}
	return t, nil
}

func (t dbTarget) open() (*sql.DB, error) {
	addr := net.JoinHostPort(t.host, t.port)
	switch t.engine {
	case "mysql":
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName = t.user, t.password, "tcp", addr, t.database
		cfg.Timeout, cfg.ReadTimeout = 5*time.Second, introspectTimeout
		cfg.TLSConfig = "true"
		if t.insecure {
			cfg.TLSConfig = "false"
		}
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			return nil, err
		}
		return sql.OpenDB(connector), nil
	case "sqlserver":
		// Encrypted by default. DB_TRUST_SERVER_CERT=1 accepts a self-signed
		// server certificate (still encrypted, but not verified).
		u := url.URL{Scheme: "sqlserver", User: url.UserPassword(t.user, t.password), Host: addr}
		q := url.Values{"database": {t.database}, "encrypt": {"true"}, "connection timeout": {"5"}}
		if t.insecure {
			q.Set("encrypt", "disable")
		}
		if os.Getenv("DB_TRUST_SERVER_CERT") == "1" {
			q.Set("TrustServerCertificate", "true")
		}
		u.RawQuery = q.Encode()
		return sql.Open("sqlserver", u.String())
	default:
		sslmode := "require"
		u := url.URL{Scheme: "postgres", User: url.UserPassword(t.user, t.password), Host: addr, Path: "/" + t.database}
		if t.insecure {
			sslmode = "disable"
		}
		u.RawQuery = url.Values{"sslmode": {sslmode}, "connect_timeout": {"5"}}.Encode()
		return sql.Open("postgres", u.String())
	}
}

type introspectQueries struct{ tables, columns, pks, fks string }

var introspectSQL = map[string]introspectQueries{
	"sqlserver": {
		tables: `SELECT table_name FROM INFORMATION_SCHEMA.TABLES WHERE table_schema = SCHEMA_NAME() AND table_type = 'BASE TABLE'`,
		columns: `SELECT table_name, column_name, data_type + CASE WHEN character_maximum_length IS NULL THEN '' WHEN character_maximum_length = -1 THEN '(max)' ELSE '(' + CAST(character_maximum_length AS varchar(10)) + ')' END
FROM INFORMATION_SCHEMA.COLUMNS WHERE table_schema = SCHEMA_NAME() ORDER BY table_name, ordinal_position`,
		pks: `SELECT kcu.table_name, kcu.column_name FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = SCHEMA_NAME() ORDER BY kcu.table_name, kcu.ordinal_position`,
		fks: `SELECT OBJECT_NAME(fkc.parent_object_id), pc.name, OBJECT_NAME(fkc.referenced_object_id), rc.name
FROM sys.foreign_key_columns fkc
JOIN sys.tables t ON t.object_id = fkc.parent_object_id
JOIN sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
WHERE t.schema_id = SCHEMA_ID()`,
	},
	"mysql": {
		tables:  `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'`,
		columns: `SELECT table_name, column_name, column_type FROM information_schema.columns WHERE table_schema = DATABASE() ORDER BY table_name, ordinal_position`,
		pks:     `SELECT table_name, column_name FROM information_schema.key_column_usage WHERE table_schema = DATABASE() AND constraint_name = 'PRIMARY' ORDER BY table_name, ordinal_position`,
		fks:     `SELECT table_name, column_name, referenced_table_name, referenced_column_name FROM information_schema.key_column_usage WHERE table_schema = DATABASE() AND referenced_table_name IS NOT NULL`,
	},
	"postgres": {
		tables:  `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`,
		columns: `SELECT table_name, column_name, data_type FROM information_schema.columns WHERE table_schema = current_schema() ORDER BY table_name, ordinal_position`,
		pks: `SELECT kcu.table_name, kcu.column_name FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema AND kcu.table_name = tc.table_name
WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = current_schema() ORDER BY kcu.table_name, kcu.ordinal_position`,
		fks: `SELECT kcu.table_name, kcu.column_name, ccu.table_name, ccu.column_name FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = current_schema()`,
	},
}

// Introspect reads table structure only (fixed information_schema queries).
// The connection is closed before returning and nothing is persisted.
func Introspect(ctx context.Context, t dbTarget) (map[string]Table, string, error) {
	ctx, cancel := context.WithTimeout(ctx, introspectTimeout)
	defer cancel()
	db, err := t.open()
	if err != nil {
		return nil, "", userError("เชื่อมต่อฐานข้อมูลไม่สำเร็จ")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tables, err := readTables(ctx, db, t.engine, introspectSQL[t.engine])
	if err != nil {
		var ue userError
		if errors.As(err, &ue) {
			return nil, "", err
		}
		return nil, "", userError("เชื่อมต่อหรืออ่านโครงสร้างฐานข้อมูลไม่สำเร็จ (ตรวจ host, รหัสผ่าน, TLS และสิทธิ์อ่าน information_schema)")
	}
	schema := ""
	if t.engine == "sqlserver" {
		// readTables only looks at the login's default schema, so every table lives in it.
		if err := db.QueryRowContext(ctx, "SELECT SCHEMA_NAME()").Scan(&schema); err != nil {
			return nil, "", userError("อ่านชื่อ schema ของ SQL Server ไม่สำเร็จ")
		}
	}
	return tables, schema, nil
}

func readTables(ctx context.Context, db *sql.DB, engine string, q introspectQueries) (map[string]Table, error) {
	// Only fixed SELECTs on catalog views run here. Where the driver supports it
	// they also run in a read-only transaction (go-mssqldb does not).
	var tx interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	} = db
	if engine != "sqlserver" {
		rtx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, err
		}
		defer rtx.Rollback()
		tx = rtx
	}

	rows := 0
	scan := func(query string, dest int, fn func(v []string)) error {
		res, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer res.Close()
		for res.Next() {
			if rows++; rows > maxIntrospectRows {
				return userError("ฐานข้อมูลใหญ่เกินกว่าที่รองรับ")
			}
			v := make([]string, dest)
			ptr := make([]any, dest)
			for i := range v {
				ptr[i] = &v[i]
			}
			if err := res.Scan(ptr...); err != nil {
				return err
			}
			fn(v)
		}
		return res.Err()
	}

	columns := map[string][]Column{}
	if err := scan(q.tables, 1, func(v []string) { columns[v[0]] = nil }); err != nil {
		return nil, err
	}
	if err := scan(q.columns, 3, func(v []string) {
		if _, ok := columns[v[0]]; ok {
			columns[v[0]] = append(columns[v[0]], Column{Name: v[1], Type: v[2]})
		}
	}); err != nil {
		return nil, err
	}
	pks := map[string]string{}
	if err := scan(q.pks, 2, func(v []string) {
		if _, seen := pks[v[0]]; !seen {
			pks[v[0]] = v[1]
		}
	}); err != nil {
		return nil, err
	}
	fks := map[[2]string]ForeignKey{}
	if err := scan(q.fks, 4, func(v []string) {
		fks[[2]string{v[0], v[1]}] = ForeignKey{Table: v[2], Column: v[3]}
	}); err != nil {
		return nil, err
	}

	out := make(map[string]Table, len(columns))
	names := make([]string, 0, len(columns))
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cols := columns[name]
		if len(cols) == 0 {
			continue
		}
		for i := range cols {
			if fk, ok := fks[[2]string{name, cols[i].Name}]; ok {
				fk := fk
				cols[i].FK = &fk
			}
		}
		pk := pks[name]
		if pk == "" {
			pk = cols[0].Name
		}
		out[name] = Table{PK: pk, Columns: cols}
	}
	if len(out) == 0 {
		return nil, userError("ไม่พบตารางในฐานข้อมูลนี้ (หรือบัญชีไม่มีสิทธิ์มองเห็นตาราง)")
	}
	return out, nil
}
