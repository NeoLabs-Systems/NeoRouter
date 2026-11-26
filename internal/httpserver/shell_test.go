package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

func shellFor(next http.HandlerFunc) http.Handler {
	return Shell(next, ShellOptions{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Getenv:          func(string) string { return "" },
		AnalyticsOrigin: "https://stats.example.test",
	})
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestShellHeaders(t *testing.T) {
	h := shellFor(func(w http.ResponseWriter, r *http.Request) {})

	page := serve(h, httptest.NewRequest("GET", "/app/", nil)).Header()
	csp := page.Get("Content-Security-Policy")
	for _, want := range []string{
		"frame-ancestors 'self'", "https://www.gstatic.com", "'wasm-unsafe-eval'",
		"font-src 'self' data: https://fonts.gstatic.com", "https://stats.example.test",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("policy misses %q: %s", want, csp)
		}
	}
	if page.Get("X-Frame-Options") != "SAMEORIGIN" || page.Get("Strict-Transport-Security") != "" || page.Get("Cache-Control") != "" {
		t.Fatalf("page headers: %v", page)
	}
	if api := serve(h, httptest.NewRequest("GET", "/api/v1/info", nil)).Header(); !strings.HasPrefix(api.Get("Cache-Control"), "no-store") {
		t.Fatalf("API answers are not cached: %v", api)
	}
	if gateway := serve(h, httptest.NewRequest("GET", "/v1/media/x", nil)).Header(); gateway.Get("Cross-Origin-Resource-Policy") != "cross-origin" {
		t.Fatalf("gateway resource policy: %v", gateway)
	}
}

func TestShellRecoversAndCapsBodies(t *testing.T) {
	panicking := shellFor(func(http.ResponseWriter, *http.Request) { panic("boom") })
	if w := serve(panicking, httptest.NewRequest("GET", "/api/v1/x", nil)); w.Code != 500 || strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("panic answer: %d %s", w.Code, w.Body)
	}

	read := shellFor(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		}
	})
	big := strings.Repeat("x", int(DefaultJSONBodyLimit)+1)
	for path, want := range map[string]int{
		"/api/v1/processing/preview": 413,
		"/api/v1/playground/media":   200,
		"/v1/chat/completions":       200,
	} {
		if w := serve(read, httptest.NewRequest("POST", path, strings.NewReader(big))); w.Code != want {
			t.Fatalf("%s: %d, want %d", path, w.Code, want)
		}
	}
}

func TestShellJoinsForwardedFor(t *testing.T) {
	var seen string
	h := shellFor(func(_ http.ResponseWriter, r *http.Request) { seen = httpx.ClientIP(r, true) })
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("X-Forwarded-For", "203.0.113.9")
	r.Header.Add("X-Forwarded-For", "198.51.100.7")
	serve(h, r)
	if seen != "198.51.100.7" {
		t.Fatalf("client address %q", seen)
	}
}
