package status

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// testDB opens a real SQLite database with the NeoRouter schema in a temp dir.
func testDB(t *testing.T) (*dbx.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := dbx.Open(context.Background(), filepath.Join(dir, "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(context.Background(), db.W); err != nil {
		t.Fatalf("schema.Ensure: %v", err)
	}
	return db, dir
}

// TestPublicStatusRoute: the report and its history are served without
// authentication and without a cache.
func TestPublicStatusRoute(t *testing.T) {
	db, dir := testDB(t)
	service := New(db, dir)
	mux := http.NewServeMux()
	service.Routes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/public/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Product             string      `json:"product"`
		Status              string      `json:"status"`
		UpdatedAt           string      `json:"updatedAt"`
		RefreshAfterSeconds int         `json:"refreshAfterSeconds"`
		Note                string      `json:"note"`
		Components          []Component `json:"components"`
		History             []Sample    `json:"history"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body.String(), err)
	}
	if body.Product != "NeoRouter" {
		t.Fatalf("product = %q", body.Product)
	}
	if len(body.History) != 48 {
		t.Fatalf("history length = %d, want 48", len(body.History))
	}
	if body.Status != "operational" {
		t.Fatalf("status = %q, want operational", body.Status)
	}
	if body.Note != "All monitored services are operational." {
		t.Fatalf("note = %q", body.Note)
	}
	if body.RefreshAfterSeconds != 60 {
		t.Fatalf("refreshAfterSeconds = %d", body.RefreshAfterSeconds)
	}
	storage := false
	for _, c := range body.Components {
		if c.Name == "Storage" && c.Status == "operational" && c.Group == "Data" {
			storage = true
		}
	}
	if !storage {
		t.Fatalf("components = %+v", body.Components)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", body.UpdatedAt); err != nil {
		t.Fatalf("updatedAt = %q: %v", body.UpdatedAt, err)
	}
	// Key order is part of the contract: the report first, its history last.
	want := []string{`"product"`, `"status"`, `"updatedAt"`, `"refreshAfterSeconds"`, `"note"`, `"components"`, `"history"`}
	at := 0
	for _, key := range want {
		i := indexFrom(rec.Body.String(), key, at)
		if i < 0 {
			t.Fatalf("key %s out of order in %s", key, rec.Body.String())
		}
		at = i
	}
}

// TestPublicStatusDegradesWithoutCatalog pins the wording for a database whose
// catalog cannot be queried.
func TestPublicStatusDegradesWithoutCatalog(t *testing.T) {
	db, dir := testDB(t)
	if err := db.W.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := db.R.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	service := New(db, dir)
	report := service.Public(context.Background())

	if report.Status != "unavailable" {
		t.Fatalf("status = %q, want unavailable", report.Status)
	}
	if report.Note != "One or more monitored services need attention." {
		t.Fatalf("note = %q", report.Note)
	}
	want := []struct{ name, status, description string }{
		{"Web App & API", "operational", "The web server is responding to requests."},
		{"Inference Gateway", "degraded", "The gateway depends on unavailable storage."},
		{"Storage", "unavailable", "The database could not be reached."},
		{"Model Catalog", "degraded", "The model catalog could not be queried."},
	}
	for i, w := range want {
		got := report.Components[i]
		if got.Name != w.name || got.Status != w.status || got.Description != w.description {
			t.Fatalf("component %d = %+v, want %+v", i, got, w)
		}
	}
}

// TestPublicStatusProbesAreInjectable covers the consumer-side interface: a
// domain that owns the catalog table answers the model check itself.
func TestPublicStatusProbesAreInjectable(t *testing.T) {
	service := &Service{
		Storage: func(context.Context) error { return nil },
		Catalog: func(context.Context) error { return errNoCatalog },
		History: NewHistory(t.TempDir()),
	}
	report := service.Public(context.Background())
	if report.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", report.Status)
	}
	if report.Components[2].Status != "operational" || report.Components[3].Status != "degraded" {
		t.Fatalf("components = %+v", report.Components)
	}
}

type staticError string

func (e staticError) Error() string { return string(e) }

const errNoCatalog = staticError("no catalog")

// indexFrom is strings.Index starting at an offset.
func indexFrom(haystack, needle string, from int) int {
	if from > len(haystack) {
		return -1
	}
	if i := strings.Index(haystack[from:], needle); i >= 0 {
		return from + i
	}
	return -1
}
