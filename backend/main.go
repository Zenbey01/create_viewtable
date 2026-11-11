package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxRequestBody = 5 << 20

type API struct {
	store *Store
	token string
}

type createSchemaRequest struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
	DDL    string `json:"ddl"`
}

func main() {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	store, err := OpenStore(filepath.Join(dataDir, "schemas.json"))
	if err != nil {
		log.Fatalf("load schema store: %v", err)
	}
	address := os.Getenv("API_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	token := os.Getenv("API_TOKEN")
	if token == "" {
		log.Fatal("API_TOKEN must be set")
	}
	server := &http.Server{
		Addr:              address,
		Handler:           (&API{store: store, token: token}).handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Go API listening on http://%s", address)
	log.Fatal(server.ListenAndServe())
}

func (a *API) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("/api/schemas", a.requireToken(http.HandlerFunc(a.schemas)))
	mux.Handle("/api/schemas/", a.requireToken(http.HandlerFunc(a.schemaByID)))
	mux.Handle("/api/introspect", a.requireToken(http.HandlerFunc(a.introspect)))
	return mux
}

func (a *API) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), prefix)
		if !strings.HasPrefix(r.Header.Get("Authorization"), prefix) ||
			len(provided) != len(a.token) ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(a.token)) != 1 {
			writeAPIError(w, http.StatusUnauthorized, "ไม่ได้รับอนุญาต")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) schemas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.store.List())
	case http.MethodPost:
		a.createSchema(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "รองรับเฉพาะ GET และ POST")
	}
}

func (a *API) createSchema(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request createSchemaRequest
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "อ่านข้อมูลคำขอไม่สำเร็จ")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "คำขอต้องมี JSON object เพียงชุดเดียว")
		return
	}

	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "กรุณาระบุชื่อฐานข้อมูล")
		return
	}
	switch request.Engine {
	case "mysql", "postgres", "sqlserver":
	default:
		writeAPIError(w, http.StatusBadRequest, "ไม่รองรับระบบฐานข้อมูลนี้")
		return
	}
	tables, err := ParseDDL(request.DDL)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := newID()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "สร้างรหัสฐานข้อมูลไม่สำเร็จ")
		return
	}
	schema := Schema{
		ID:       id,
		Name:     request.Name,
		Engine:   request.Engine,
		ViewName: "v_new_view",
		Tables:   tables,
	}
	if err := a.store.Add(schema); err != nil {
		log.Printf("persist schema: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "บันทึก schema ไม่สำเร็จ")
		return
	}
	writeJSON(w, http.StatusCreated, schema)
}

type introspectRequest struct {
	Name             string `json:"name"`
	Engine           string `json:"engine"`
	ConnectionString string `json:"connectionString"`
	Save             bool   `json:"save"` // keep the structure (never the credentials) like a pasted DDL
}

// introspect connects with a one-off connection string, reads table structure
// and returns it. The connection string is never stored or logged. The schema
// is persisted only when the request asks for it (save: true).
func (a *API) introspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "รองรับเฉพาะ POST")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request introspectRequest
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "อ่านข้อมูลคำขอไม่สำเร็จ")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "กรุณาระบุชื่อฐานข้อมูล")
		return
	}
	target, err := parseTarget(request.Engine, request.ConnectionString, allowedHosts(), os.Getenv("DB_ALLOW_INSECURE") == "1")
	if err == nil {
		var tables map[string]Table
		var schemaName string
		if tables, schemaName, err = Introspect(r.Context(), target); err == nil {
			id, idErr := newID()
			if idErr != nil {
				writeAPIError(w, http.StatusInternalServerError, "สร้างรหัสฐานข้อมูลไม่สำเร็จ")
				return
			}
			schema := Schema{ID: id, Name: request.Name, Engine: request.Engine, ViewName: "v_new_view",
				Database: target.database, Schema: schemaName, Tables: tables}
			if request.Save {
				if err := a.store.Add(schema); err != nil {
					log.Printf("persist schema: %v", err)
					writeAPIError(w, http.StatusInternalServerError, "บันทึก schema ไม่สำเร็จ")
					return
				}
			}
			writeJSON(w, http.StatusOK, schema)
			return
		}
	}
	var ue userError
	if errors.As(err, &ue) {
		writeAPIError(w, http.StatusBadRequest, ue.Error())
		return
	}
	writeAPIError(w, http.StatusBadGateway, "เชื่อมต่อฐานข้อมูลไม่สำเร็จ")
}

func (a *API) schemaByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/schemas/")
	if id == "" || strings.Contains(id, "/") {
		writeAPIError(w, http.StatusNotFound, "ไม่พบ schema")
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		writeAPIError(w, http.StatusMethodNotAllowed, "รองรับเฉพาะ DELETE")
		return
	}
	deleted, err := a.store.Delete(id)
	if err != nil {
		log.Printf("delete schema: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "ลบ schema ไม่สำเร็จ")
		return
	}
	if !deleted {
		writeAPIError(w, http.StatusNotFound, "ไม่พบ schema")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
