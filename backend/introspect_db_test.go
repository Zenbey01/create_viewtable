package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

// fakeCatalog is a tiny database/sql driver that answers each fixed catalog
// query with canned rows, so readTables' assembly logic is testable without a
// real server.
type fakeCatalog map[string][][]driver.Value

func (f fakeCatalog) Open(string) (driver.Conn, error) { return fakeConn{f}, nil }

type fakeConn struct{ data fakeCatalog }

func (c fakeConn) Prepare(q string) (driver.Stmt, error) { return fakeStmt{c.data, q}, nil }
func (fakeConn) Close() error                            { return nil }
func (fakeConn) Begin() (driver.Tx, error)               { return fakeTx{}, nil }
func (fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return fakeTx{}, nil
}

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeStmt struct {
	data fakeCatalog
	q    string
}

func (fakeStmt) Close() error  { return nil }
func (fakeStmt) NumInput() int { return -1 }
func (fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("writes are not allowed")
}
func (s fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	return &fakeRows{rows: s.data[s.q]}, nil
}

type fakeRows struct {
	rows [][]driver.Value
	i    int
}

func (r *fakeRows) Columns() []string {
	if len(r.rows) == 0 {
		return nil
	}
	return make([]string, len(r.rows[0]))
}
func (*fakeRows) Close() error { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.i])
	r.i++
	return nil
}

var fakeQueries = introspectQueries{tables: "T", columns: "C", pks: "P", fks: "F"}

func openFake(t *testing.T, name string, data fakeCatalog) *sql.DB {
	t.Helper()
	sql.Register(name, data)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func row(v ...string) []driver.Value {
	out := make([]driver.Value, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

func TestReadTablesAssemblesColumnsKeysAndForeignKeys(t *testing.T) {
	data := fakeCatalog{
		"T": {row("orders"), row("customers"), row("no_columns")},
		"C": {
			row("customers", "id", "int"), row("customers", "name", "nvarchar(50)"),
			row("orders", "order_no", "int"), row("orders", "customer_id", "int"),
			row("some_view", "x", "int"), // not a base table: ignored
		},
		"P": {row("customers", "id"), row("customers", "ignored_second_key")},
		"F": {row("orders", "customer_id", "customers", "id")},
	}
	// "mysql" exercises the read-only transaction path, "sqlserver" the direct one.
	for _, engine := range []string{"mysql", "sqlserver"} {
		db := openFake(t, "fake-ok-"+engine, data)
		tables, err := readTables(context.Background(), db, engine, fakeQueries)
		if err != nil {
			t.Fatalf("%s: %v", engine, err)
		}
		if len(tables) != 2 {
			t.Fatalf("%s: want 2 tables (no empty/view tables), got %v", engine, tables)
		}
		if tables["customers"].PK != "id" {
			t.Errorf("%s: customers pk = %q", engine, tables["customers"].PK)
		}
		if tables["orders"].PK != "order_no" { // no PK row: falls back to first column
			t.Errorf("%s: orders pk = %q", engine, tables["orders"].PK)
		}
		fk := tables["orders"].Columns[1].FK
		if fk == nil || fk.Table != "customers" || fk.Column != "id" {
			t.Errorf("%s: orders.customer_id fk = %+v", engine, fk)
		}
		if tables["orders"].Columns[0].FK != nil {
			t.Errorf("%s: order_no must not have an fk", engine)
		}
	}
}

func TestReadTablesRejectsEmptyAndOversizedCatalogs(t *testing.T) {
	_, err := readTables(context.Background(), openFake(t, "fake-empty", fakeCatalog{}), "mysql", fakeQueries)
	var ue userError
	if !errors.As(err, &ue) || !strings.Contains(ue.Error(), "ไม่พบตาราง") {
		t.Fatalf("empty catalog: got %v", err)
	}

	big := make([][]driver.Value, maxIntrospectRows+1)
	for i := range big {
		big[i] = row("t")
	}
	_, err = readTables(context.Background(), openFake(t, "fake-big", fakeCatalog{"T": big}), "mysql", fakeQueries)
	if !errors.As(err, &ue) || !strings.Contains(ue.Error(), "ใหญ่เกิน") {
		t.Fatalf("oversized catalog: got %v", err)
	}
}

func TestIntrospectQueriesAreReadOnlySelects(t *testing.T) {
	for engine, q := range introspectSQL {
		for name, query := range map[string]string{"tables": q.tables, "columns": q.columns, "pks": q.pks, "fks": q.fks} {
			up := strings.ToUpper(strings.TrimSpace(query))
			if !strings.HasPrefix(up, "SELECT") {
				t.Errorf("%s/%s does not start with SELECT", engine, name)
			}
			for _, bad := range []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "ALTER ", "EXEC", "CREATE "} {
				if strings.Contains(up, bad) {
					t.Errorf("%s/%s contains %q", engine, name, bad)
				}
			}
		}
	}
}
