package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// testConfig builds a configuration that points every path at a temp home.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	home := t.TempDir()
	appDir := t.TempDir()
	env := map[string]string{
		"NEOROUTER_HOME":    home,
		"NEOROUTER_APP_DIR": appDir,
		"NODE_ENV":          "test",
	}
	getenv := func(k string) string { return env[k] }
	cfg, err := config.Load(getenv, home, appDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func openApp(t *testing.T) *App {
	t.Helper()
	cfg := testConfig(t)
	a, err := Open(context.Background(), Options{StaticConfig: &cfg, Version: "9.9.9"})
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func get(t *testing.T, a *App, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.Handler.ServeHTTP(rec, req)
	return rec
}

func TestHealthAndReady(t *testing.T) {
	a := openApp(t)

	rec := get(t, a, "/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/health status = %d", rec.Code)
	}
	if want := `{"status":"ok","process":"http","version":"9.9.9"}`; strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("/health body = %s, want %s", strings.TrimSpace(rec.Body.String()), want)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("/health content type = %q", ct)
	}
	if id := rec.Header().Get("X-Request-Id"); id == "" {
		t.Fatal("/health has no X-Request-Id")
	}

	rec = get(t, a, "/ready", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/ready status = %d", rec.Code)
	}
	if want := `{"status":"ready","database":true}`; strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("/ready body = %s, want %s", strings.TrimSpace(rec.Body.String()), want)
	}
}

// TestHealthRejectsOtherMethodsWithNotFound: a non-GET request to /health or
// /ready falls through to the JSON 404, not 405.
func TestHealthRejectsOtherMethodsWithNotFound(t *testing.T) {
	a := openApp(t)
	for _, target := range []string{"/health", "/ready"} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		rec := httptest.NewRecorder()
		a.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("POST %s status = %d, want 404", target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"NOT_FOUND"`) {
			t.Fatalf("POST %s body = %s", target, rec.Body.String())
		}
	}
}

func TestOpenCreatesPrivateLayoutAndSchema(t *testing.T) {
	cfg := testConfig(t)
	a, err := Open(context.Background(), Options{StaticConfig: &cfg})
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	defer a.Close()

	for _, dir := range []string{cfg.Paths.Home, cfg.Paths.Data, cfg.Paths.Logs, cfg.Paths.Backups} {
		st, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("missing %s: %v", dir, err)
		}
		if !st.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}
	if _, err := os.Stat(cfg.Paths.Database); err != nil {
		t.Fatalf("missing database: %v", err)
	}
	var uv int
	if err := a.DB.R.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&uv); err != nil {
		t.Fatal(err)
	}
	if want := len(schema.Steps()); uv != want {
		t.Fatalf("user_version = %d, want %d", uv, want)
	}
	if got := a.Config().Port; got != 4600 {
		t.Fatalf("port = %d, want 4600", got)
	}
	if a.Version != config.Version {
		t.Fatalf("version = %q, want %q", a.Version, config.Version)
	}
}

func TestUnknownPathKeepsJSONErrorAndCORS(t *testing.T) {
	a := openApp(t)
	rec := get(t, a, "/no/such/thing", map[string]string{"Accept": "application/json", "Origin": "http://localhost:5173", "X-Request-Id": "trace-1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", rec.Body.String(), err)
	}
	if body.Error.Code != "NOT_FOUND" || body.Error.Message != "The requested resource was not found." {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if body.Error.RequestID != "" {
		t.Fatalf("notFound must not carry a requestId, got %q", body.Error.RequestID)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "trace-1" {
		t.Fatalf("request id = %q", got)
	}
}

func TestReloadConfigAndClose(t *testing.T) {
	cfg := testConfig(t)
	calls := 0
	a, err := Open(context.Background(), Options{
		ConfigLoader: func() (config.Config, error) {
			calls++
			return cfg, nil
		},
		Version: "1.2.3",
	})
	if err != nil {
		t.Fatalf("app.Open: %v", err)
	}
	if calls != 1 {
		t.Fatalf("loader calls = %d, want 1", calls)
	}
	if err := a.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig: %v", err)
	}
	if calls != 2 {
		t.Fatalf("loader calls = %d, want 2", calls)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
