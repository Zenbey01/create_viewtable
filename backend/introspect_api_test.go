package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func introspectHandler(t *testing.T) http.Handler {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "schemas.json"))
	if err != nil {
		t.Fatal(err)
	}
	return (&API{store: store, token: unitToken}).handler()
}

func postIntrospect(h http.Handler, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/introspect", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIntrospectEndpointGuards(t *testing.T) {
	h := introspectHandler(t)
	const pw = "SuperSecretPw1"
	conn := `"connectionString":"sqlserver://sa:` + pw + `@db.internal/app"`

	cases := []struct {
		name, allow, token, body string
		status                   int
		contains                 string
	}{
		{"no token", "db.internal", "", `{"name":"a","engine":"sqlserver",` + conn + `}`, 401, ""},
		{"wrong token", "db.internal", "nope", `{"name":"a","engine":"sqlserver",` + conn + `}`, 401, ""},
		{"disabled without allowlist", "", unitToken, `{"name":"a","engine":"sqlserver",` + conn + `}`, 400, "ALLOWED_DB_HOSTS"},
		{"host not allowed", "other.internal", unitToken, `{"name":"a","engine":"sqlserver",` + conn + `}`, 400, "ไม่อยู่ในรายการ"},
		{"missing name", "db.internal", unitToken, `{"name":" ","engine":"sqlserver",` + conn + `}`, 400, "ชื่อ"},
		{"unknown field", "db.internal", unitToken, `{"name":"a","engine":"sqlserver","sql":"DROP TABLE x",` + conn + `}`, 400, "อ่านข้อมูล"},
		{"bad json", "db.internal", unitToken, `{`, 400, "อ่านข้อมูล"},
		{"unsupported engine", "db.internal", unitToken, `{"name":"a","engine":"oracle",` + conn + `}`, 400, "รองรับเฉพาะ"},
	}
	for _, c := range cases {
		t.Setenv("ALLOWED_DB_HOSTS", c.allow)
		rec := postIntrospect(h, c.token, c.body)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.contains) {
			t.Errorf("%s: got %d %s", c.name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), pw) {
			t.Errorf("%s: response echoed the password", c.name)
		}
	}
}

func TestIntrospectEndpointOnlyAllowsPOST(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/introspect", nil)
	req.Header.Set("Authorization", "Bearer "+unitToken)
	rec := httptest.NewRecorder()
	introspectHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET got %d", rec.Code)
	}
}

func TestUnreachableDatabaseGivesGenericErrorWithoutCredentials(t *testing.T) {
	t.Setenv("ALLOWED_DB_HOSTS", "127.0.0.1")
	const pw = "SuperSecretPw1"
	for _, scheme := range []string{"sqlserver", "mysql", "postgres"} {
		rec := postIntrospect(introspectHandler(t), unitToken,
			`{"name":"a","engine":"`+scheme+`","connectionString":"`+scheme+`://u:`+pw+`@127.0.0.1:1/app"}`)
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), pw) ||
			!strings.Contains(rec.Body.String(), "ไม่สำเร็จ") {
			t.Errorf("%s: got %d %s", scheme, rec.Code, rec.Body.String())
		}
	}
}
