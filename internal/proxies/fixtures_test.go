package proxies

// Test support: the loopback origin the JS oracle builds in
// test/api/proxies.test.js, a temp database, a memory settings store, the admin
// gate stand-in and the request helpers. The proxies themselves live in
// proxyfixtures_test.go.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// memSettings is the settings domain stand-in the pool reads its policy from.
type memSettings struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemSettings() *memSettings { return &memSettings{values: map[string]string{}} }

func (m *memSettings) GetGlobal(_ context.Context, key string) (json.RawMessage, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.values[key]
	return json.RawMessage(value), ok, nil
}

func (m *memSettings) SetGlobalRaw(_ context.Context, key string, value json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = string(value)
	return nil
}

func (m *memSettings) setPolicy(t *testing.T, enabled, fallbackDirect bool) {
	t.Helper()
	raw, err := json.Marshal(PoolPolicy{Enabled: enabled, FallbackDirect: fallbackDirect})
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := m.SetGlobalRaw(context.Background(), policySettingKey, raw); err != nil {
		t.Fatalf("store policy: %v", err)
	}
}

// harness is one isolated pool: temp database, memory settings, test origin and
// the proxy fixtures the test needs.
type harness struct {
	t        *testing.T
	db       *dbx.DB
	settings *memSettings
	sealer   *Sealer
	svc      *Service
	routes   *Routes
	server   *httptest.Server

	origin     *httptest.Server
	dropped    atomic.Int32
	originHost string // 127.0.0.1:port
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	box, err := secretbox.New(SealedPrefix, "proxies-test-key-material")
	if err != nil {
		t.Fatalf("build box: %v", err)
	}
	sealer := NewSealer(box, []byte("0123456789abcdef0123456789abcdef"))

	h := &harness{t: t, db: db, settings: newMemSettings(), sealer: sealer}
	svc, err := New(Options{
		DB:       db,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sealer:   sealer,
		Settings: h.settings,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	h.svc = svc
	t.Cleanup(func() { _ = svc.Close() })

	h.origin = httptest.NewServer(http.HandlerFunc(h.serveOrigin))
	t.Cleanup(h.origin.Close)
	h.originHost = h.origin.Listener.Addr().String()

	h.routes = &Routes{Svc: svc, Log: svc.Log, Admin: testAdminGate}
	mux := http.NewServeMux()
	h.routes.Mount(mux)
	h.server = httptest.NewServer(mux)
	t.Cleanup(h.server.Close)
	return h
}

// serveOrigin is the origin server of the JS test: a stream, a dropped
// connection, a 429 and a plain 200 body.
func (h *harness) serveOrigin(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/drop":
		h.dropped.Add(1)
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		panic(http.ErrAbortHandler)
	case "/stream":
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: hello\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	default:
		status := http.StatusOK
		if r.URL.Path == "/limited" {
			status = http.StatusTooManyRequests
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "through proxy")
	}
}

// testAdminGate stands in for internal/auth's admin middleware, so a test can
// prove the routes sit behind it.
func testAdminGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer admin-token" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"code":"FORBIDDEN","message":"Administrator access is required."}}`)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// api calls the admin routes the way the JS test's supertest helper does.
func (h *harness) api(method, path, token string) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(method, h.server.URL+"/api/v1/admin/proxies"+path, nil)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// apiJSON sends a JSON body and returns the response with its decoded body.
func (h *harness) apiJSON(method, path, token string, body any) (*http.Response, []byte) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, h.server.URL+"/api/v1/admin/proxies"+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	return resp, raw
}

// reset matches the JS reset(): drop every proxy and enable rotation without a
// direct fallback.
func (h *harness) reset() {
	h.t.Helper()
	proxies, err := h.svc.List(context.Background())
	if err != nil {
		h.t.Fatalf("list: %v", err)
	}
	for _, proxy := range proxies {
		if err := h.svc.Remove(context.Background(), proxy.ID); err != nil {
			h.t.Fatalf("remove: %v", err)
		}
	}
	h.settings.setPolicy(h.t, true, false)
}

// apiRaw sends body verbatim, for request shapes a JSON struct cannot express.
func (h *harness) apiRaw(method, path, token, body string) (*http.Response, []byte) {
	h.t.Helper()
	req, err := http.NewRequest(method, h.server.URL+"/api/v1/admin/proxies"+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	return resp, raw
}

// allowPrivateTestURLs turns off the private-destination guard, so a test on
// loopback can exercise the probe path the guard otherwise blocks. The guard is
// a wiring seam (Options.IsPrivateDestination), not part of the pool.
func (h *harness) allowPrivateTestURLs() {
	h.svc.IsPrivateDestination = func(context.Context, string) (bool, error) { return false, nil }
}

// nowMillis is Date.now(), the unit every health timestamp uses.
func nowMillis() int64 { return time.Now().UnixMilli() }

func (h *harness) importProxies(text, protocol string) ImportResult {
	h.t.Helper()
	result, err := h.svc.ImportLines(context.Background(), text, protocol)
	if err != nil {
		h.t.Fatalf("import: %v", err)
	}
	return result
}

func (h *harness) list() []Proxy {
	h.t.Helper()
	proxies, err := h.svc.List(context.Background())
	if err != nil {
		h.t.Fatalf("list: %v", err)
	}
	return proxies
}

func (h *harness) request(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return req
}

func (h *harness) closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
