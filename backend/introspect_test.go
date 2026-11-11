package main

import (
	"strings"
	"testing"
)

func TestParseTargetDecodesCredentials(t *testing.T) {
	got, err := parseTarget("mysql", "mysql://u%40ser:p%3Aw%2Fd%23%3F@db.internal:3306/my%20db", map[string]bool{"db.internal": true}, false)
	if err != nil || got.user != "u@ser" || got.password != "p:w/d#?" || got.database != "my db" {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestParseTarget(t *testing.T) {
	allow := map[string]bool{"db.internal": true, "pg.internal:5433": true}
	cases := []struct {
		name, engine, raw string
		allow             map[string]bool
		insecure          bool
		wantErr           string
	}{
		{"mysql ok", "mysql", "mysql://u:p@db.internal/app", allow, false, ""},
		{"postgres port allow", "postgres", "postgres://u:p@pg.internal:5433/app?sslmode=require", allow, false, ""},
		{"disabled without allowlist", "mysql", "mysql://u:p@db.internal/app", nil, false, "ALLOWED_DB_HOSTS"},
		{"host not allowed", "mysql", "mysql://u:p@evil.example/app", allow, false, "ไม่อยู่ในรายการ"},
		{"wrong port not allowed", "postgres", "postgres://u:p@pg.internal:5432/app", allow, false, "ไม่อยู่ในรายการ"},
		{"scheme mismatch", "postgres", "mysql://u:p@db.internal/app", allow, false, "ไม่ตรง"},
		{"sqlserver ok", "sqlserver", "sqlserver://sa:p@db.internal:1433/app", map[string]bool{"db.internal:1433": true}, false, ""},
		{"sqlserver bad param", "sqlserver", "sqlserver://sa:p@db.internal/app?server=evil", allow, false, "ไม่รองรับ"},
		{"sqlserver scheme mismatch", "sqlserver", "mysql://u:p@db.internal/app", allow, false, "ไม่ตรง"},
		{"unknown engine", "oracle", "oracle://u:p@db.internal/app", allow, false, "รองรับเฉพาะ"},
		{"host override param", "postgres", "postgres://u:p@pg.internal:5433/app?host=evil", allow, false, "ไม่รองรับ"},
		{"tls off blocked", "mysql", "mysql://u:p@db.internal/app?tls=false", allow, false, "TLS"},
		{"tls off allowed by env", "mysql", "mysql://u:p@db.internal/app?tls=false", allow, true, ""},
		{"missing db", "mysql", "mysql://u:p@db.internal", allow, false, "ชื่อฐานข้อมูล"},
	}
	for _, c := range cases {
		_, err := parseTarget(c.engine, c.raw, c.allow, c.insecure)
		if c.wantErr == "" && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: error %v, want containing %q", c.name, err, c.wantErr)
		}
	}
}
