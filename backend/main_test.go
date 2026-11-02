package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

const unitToken = "builder-unit-credential"

func TestSchemaAPIAndPersistentStore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "schemas.json")
	store, err := OpenStore(storePath)
	if err != nil {
		t.Fatalf("OpenStore returned an error: %v", err)
	}
	handler := (&API{store: store, token: unitToken}).handler()

	payload := createSchemaRequest{
		Name:   "analytics",
		Engine: "postgres",
		DDL:    "CREATE TABLE events (id BIGINT PRIMARY KEY, account_id BIGINT REFERENCES accounts(id));",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	create := httptest.NewRequest(http.MethodPost, "/api/schemas", bytes.NewReader(body))
	create.Header.Set("Authorization", "Bearer "+unitToken)
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want %d: %s", created.Code, http.StatusCreated, created.Body)
	}
	var schema Schema
	if err := json.Unmarshal(created.Body.Bytes(), &schema); err != nil {
		t.Fatalf("decode created schema: %v", err)
	}
	if schema.ID == "" || schema.Tables["events"].Columns[1].FK == nil {
		t.Fatalf("created schema is missing generated ID or parsed foreign key: %#v", schema)
	}

	reloaded, err := OpenStore(storePath)
	if err != nil {
		t.Fatalf("reload persisted store: %v", err)
	}
	if got := reloaded.List(); len(got) != 1 || got[0].ID != schema.ID {
		t.Fatalf("reloaded schemas = %#v, want created schema", got)
	}

	remove := httptest.NewRequest(http.MethodDelete, "/api/schemas/"+schema.ID, nil)
	remove.Header.Set("Authorization", "Bearer "+unitToken)
	removed := httptest.NewRecorder()
	handler.ServeHTTP(removed, remove)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want %d", removed.Code, http.StatusNoContent)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("schemas after deletion = %#v, want empty", got)
	}
}

func TestSchemaAPIRejectsInvalidSchema(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "schemas.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := (&API{store: store, token: unitToken}).handler()
	for _, payload := range []string{
		`{"name":"","engine":"mysql","ddl":"CREATE TABLE t (id INT)"}`,
		`{"name":"bad-engine","engine":"oracle","ddl":"CREATE TABLE t (id INT)"}`,
		`{"name":"missing-ddl","engine":"mysql","ddl":"SELECT 1"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/schemas", bytes.NewBufferString(payload))
		request.Header.Set("Authorization", "Bearer "+unitToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("POST %s status = %d, want %d", payload, response.Code, http.StatusBadRequest)
		}
	}
}

func TestSchemaAPIRequiresToken(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "schemas.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := (&API{store: store, token: unitToken}).handler()
	for _, authorization := range []string{"", "Bearer invalid"} {
		request := httptest.NewRequest(http.MethodGet, "/api/schemas", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("GET with Authorization %q status = %d, want %d", authorization, response.Code, http.StatusUnauthorized)
		}
	}
}
