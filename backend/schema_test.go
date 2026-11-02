package main

import "testing"

func TestParseDDL(t *testing.T) {
	ddl := `
CREATE TABLE public.users (
  id BIGINT PRIMARY KEY,
  name VARCHAR(120) NOT NULL,
  email VARCHAR(255) DEFAULT 'one,two',
  department_id BIGINT,
  CONSTRAINT fk_department FOREIGN KEY (department_id) REFERENCES public.departments(id)
);
CREATE TABLE departments (
  id BIGINT PRIMARY KEY,
  name TEXT
);
ALTER TABLE ONLY public.users ADD CONSTRAINT fk_users_department
  FOREIGN KEY (department_id) REFERENCES public.departments(id);
`
	tables, err := ParseDDL(ddl)
	if err != nil {
		t.Fatalf("ParseDDL returned an error: %v", err)
	}
	users, ok := tables["users"]
	if !ok {
		t.Fatal("expected qualified table name to be normalized")
	}
	if users.PK != "id" {
		t.Fatalf("primary key = %q, want id", users.PK)
	}
	if len(users.Columns) != 4 {
		t.Fatalf("column count = %d, want 4", len(users.Columns))
	}
	if users.Columns[2].Type != "VARCHAR(255)" {
		t.Fatalf("column type = %q, want VARCHAR(255)", users.Columns[2].Type)
	}
	fk := users.Columns[3].FK
	if fk == nil || fk.Table != "departments" || fk.Column != "id" {
		t.Fatalf("foreign key = %#v, want departments.id", fk)
	}
}

func TestParseDDLRejectsMissingOrIncompleteTables(t *testing.T) {
	for _, ddl := range []string{
		"SELECT * FROM users",
		"CREATE TABLE users (id BIGINT",
		"CREATE TABLE users (PRIMARY KEY (id))",
	} {
		if _, err := ParseDDL(ddl); err == nil {
			t.Errorf("ParseDDL(%q) succeeded, want an error", ddl)
		}
	}
}
