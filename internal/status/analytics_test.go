package status

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAnalyticsDeliveredOnlyWhenEnabled is the first case of
// test/unit/analytics.test.js: the pages gain the script while analytics are on,
// and app assets keep their own answer.
func TestAnalyticsDeliveredOnlyWhenEnabled(t *testing.T) {
	env := map[string]string{}
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	landing := filepath.Join(dir, "landing")
	handler := &Analytics{
		Getenv:     func(k string) string { return env[k] },
		WebDir:     func() string { return web },
		LandingDir: func() string { return landing },
	}
	for _, d := range []string{web, landing} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte(`<head><base href="/"></head>app`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(landing, "index.html"), []byte(`<head></head>landing`), 0o600); err != nil {
		t.Fatal(err)
	}
	chain := handler.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	fetch := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	page := fetch("/app/")
	if page.Code != http.StatusOK {
		t.Fatalf("/app/ status = %d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, "_neolabs/analytics.js") || !strings.Contains(body, `base href="/app/"`) {
		t.Fatalf("/app/ body = %s", body)
	}
	if !strings.Contains(body, `/js/cookie-notice.js`) {
		t.Fatalf("/app/ body = %s", body)
	}
	if got := page.Header().Get("Cache-Control"); got != "private, no-cache" {
		t.Fatalf("/app/ Cache-Control = %q", got)
	}
	if got := page.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("/app/ Content-Type = %q", got)
	}
	if landing := fetch("/"); !strings.Contains(landing.Body.String(), "_neolabs/analytics.js") {
		t.Fatalf("/ body = %s", landing.Body.String())
	}
	if got := fetch("/app/missing.js").Code; got != http.StatusNotFound {
		t.Fatalf("/app/missing.js status = %d, want 404", got)
	}
	script := fetch(ClientPath)
	if script.Code != http.StatusOK {
		t.Fatalf("%s status = %d", ClientPath, script.Code)
	}
	if !strings.Contains(script.Body.String(), "data.neo.eu.com/i") || !strings.Contains(script.Body.String(), CountlyAppKey) {
		t.Fatalf("client script = %s", script.Body.String())
	}
	if got := script.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("script Cache-Control = %q", got)
	}
	if got := script.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("script Content-Type = %q", got)
	}

	env["ANALYTICS_ENABLED"] = "false"
	if body := fetch("/app/").Body.String(); strings.Contains(body, "_neolabs/analytics.js") {
		t.Fatalf("opt-out still injected analytics: %s", body)
	}
	if got := fetch(ClientPath).Code; got != http.StatusNotFound {
		t.Fatalf("opt-out script status = %d, want 404", got)
	}
}

// TestAnalyticsConfigOptOut covers the accepted opt-out spellings and the
// default-on behavior of analytics-config.js.
func TestAnalyticsConfigOptOut(t *testing.T) {
	for _, value := range []string{"false", "0", "off", "no", " FALSE ", "Off"} {
		getenv := func(k string) string {
			if k == "ANALYTICS_ENABLED" {
				return value
			}
			return ""
		}
		if cfg := AnalyticsConfig(getenv); cfg != nil {
			t.Fatalf("ANALYTICS_ENABLED=%q still enabled", value)
		}
	}
	for _, value := range []string{"", "true", "1", "yes", "on", "anything"} {
		getenv := func(k string) string {
			if k == "ANALYTICS_ENABLED" {
				return value
			}
			return ""
		}
		cfg := AnalyticsConfig(getenv)
		if cfg == nil {
			t.Fatalf("ANALYTICS_ENABLED=%q disabled", value)
		}
		if cfg.Endpoint != CountlyEndpoint || cfg.AppKey != CountlyAppKey {
			t.Fatalf("config = %+v", cfg)
		}
	}
}

// TestAnalyticsClientInjection pins the placeholder replacement the browser
// needs to know where to report.
func TestAnalyticsClientInjection(t *testing.T) {
	script := ClientScript(DefaultConfig())
	if strings.Contains(script, configPlaceholder) {
		t.Fatal("the config placeholder survived")
	}
	want := `{"endpoint":"https://data.neo.eu.com/i","appKey":"` + CountlyAppKey + `"}`
	if !strings.Contains(script, "var config = "+want+";") {
		t.Fatalf("config not injected: %s", firstLines(script, 8))
	}
	if !strings.Contains(script, "device_id: 'aggregate-browser'") {
		t.Fatal("the browser stream label is missing")
	}
	if !strings.Contains(script, "navigator.doNotTrack === '1'") {
		t.Fatal("the Do Not Track check is missing")
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// TestAnalyticsMissingPageFallsThrough keeps a page the install does not have
// with the static handler's answer.
func TestAnalyticsMissingPageFallsThrough(t *testing.T) {
	chain := (&Analytics{
		Getenv:     func(string) string { return "" },
		WebDir:     func() string { return filepath.Join(t.TempDir(), "absent") },
		LandingDir: func() string { return filepath.Join(t.TempDir(), "absent") },
	}).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want the static handler's 404", rec.Code)
	}
}

// TestAnalyticsPostIsNotRewritten keeps a POST to a page path out of the
// rewriter, exactly like the GET-only middleware in analytics.js.
func TestAnalyticsPostIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<head></head>"), 0o600); err != nil {
		t.Fatal(err)
	}
	chain := (&Analytics{
		Getenv:     func(string) string { return "" },
		WebDir:     func() string { return dir },
		LandingDir: func() string { return dir },
	}).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/app/", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want the next handler's 204", rec.Code)
	}
}
