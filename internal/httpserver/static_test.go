package httpserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testTree builds a Flutter web client and a landing site in a temp directory.
func testTree(t *testing.T) (web, landing string) {
	t.Helper()
	appDir := t.TempDir()
	web = filepath.Join(appDir, "flutter_app", "build", "web")
	landing = filepath.Join(appDir, "landing")
	for _, dir := range []string{web, landing, filepath.Join(web, "assets")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(web, "index.html"):                  "<!doctype html><title>NeoRouter</title>",
		filepath.Join(web, "main.dart.js"):                "console.log('app');",
		filepath.Join(web, "assets", "FontManifest.json"): `[]`,
		filepath.Join(landing, "index.html"):              "<!doctype html><title>NeoRouter landing</title>",
		filepath.Join(landing, "status.html"):             "<!doctype html><title>Status</title>",
		filepath.Join(landing, "404.html"):                "<!doctype html><p>None go here</p>",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return web, landing
}

func staticHandler(web, landing, devURL string) http.Handler {
	return Static(StaticOptions{
		WebDir:     func() string { return web },
		LandingDir: func() string { return landing },
		WebDevURL:  func() string { return devURL },
	})
}

func TestStaticServesAppClient(t *testing.T) {
	web, landing := testTree(t)
	h := staticHandler(web, landing, "")

	cases := []struct {
		name     string
		target   string
		method   string
		status   int
		contains string
		cache    string
	}{
		{name: "index at /app/", target: "/app/", method: http.MethodGet, status: http.StatusOK, contains: "NeoRouter", cache: "private, no-cache"},
		{name: "index at /app", target: "/app", method: http.MethodGet, status: http.StatusOK, contains: "NeoRouter", cache: "private, no-cache"},
		{name: "asset file", target: "/app/main.dart.js", method: http.MethodGet, status: http.StatusOK, contains: "console.log", cache: "private, no-cache"},
		{name: "nested asset", target: "/app/assets/FontManifest.json", method: http.MethodGet, status: http.StatusOK, contains: "[]", cache: "private, no-cache"},
		{name: "extension-less path falls back to the app shell", target: "/app/manifest", method: http.MethodGet, status: http.StatusOK, contains: "NeoRouter", cache: "private, no-cache"},
		{name: "spa fallback for a route", target: "/app/settings/providers", method: http.MethodGet, status: http.StatusOK, contains: "NeoRouter"},
		{name: "head is served", target: "/app/main.dart.js", method: http.MethodHead, status: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, h, tc.method, tc.target, nil, map[string]string{"Accept": "text/html"})
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.contains != "" && !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("body = %q", rec.Body.String())
			}
			if tc.cache != "" && rec.Header().Get("Cache-Control") != tc.cache {
				t.Fatalf("cache-control = %q, want %q", rec.Header().Get("Cache-Control"), tc.cache)
			}
		})
	}
}

func TestStaticNeverFallsBackForMissingAssets(t *testing.T) {
	web, landing := testTree(t)
	h := staticHandler(web, landing, "")

	// A missing asset is a 404 for API-style clients, never index.html.
	rec := doRequest(t, h, http.MethodGet, "/app/main.dart.js.map", nil, map[string]string{"Accept": "application/json"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	// A browser gets the landing 404 page instead of JSON.
	rec = doRequest(t, h, http.MethodGet, "/app/main.dart.js.map", nil, map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "None go here") {
		t.Fatalf("body = %q", rec.Body.String())
	}
	// .html is the one extension that still falls back to the app shell.
	rec = doRequest(t, h, http.MethodGet, "/app/whatever.html", nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "NeoRouter") {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestStaticWebClientMissing(t *testing.T) {
	web, landing := testTree(t)
	empty := t.TempDir()

	h := staticHandler(empty, landing, "")
	rec := doRequest(t, h, http.MethodGet, "/app/", nil, map[string]string{"Accept": "text/html"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"WEB_UI_MISSING"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	// The 503 also answers non-GET requests from the app shell.
	if rec := doRequest(t, h, http.MethodPost, "/app/anything", nil, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST status = %d, want 503", rec.Code)
	}

	dev := staticHandler(empty, landing, "http://localhost:3000")
	rec = doRequest(t, dev, http.MethodGet, "/app/", nil, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "http://localhost:3000" {
		t.Fatalf("location = %q", loc)
	}

	// A bundled client is served even when a dev URL is configured.
	rec = doRequest(t, staticHandler(web, landing, "http://localhost:3000"), http.MethodGet, "/app/", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestStaticLandingAndNotFound(t *testing.T) {
	web, landing := testTree(t)
	h := staticHandler(web, landing, "")

	cases := []struct {
		name        string
		target      string
		accept      string
		method      string
		status      int
		contentType string
		contains    string
	}{
		{name: "landing index", target: "/", method: http.MethodGet, status: http.StatusOK, contains: "landing"},
		{name: "landing html extension", target: "/status", method: http.MethodGet, status: http.StatusOK, contains: "Status"},
		{name: "landing file by name", target: "/status.html", method: http.MethodGet, status: http.StatusOK, contains: "Status"},
		{name: "browser 404 page", target: "/no/such/page", accept: "text/html,application/xhtml+xml,*/*;q=0.8", method: http.MethodGet, status: http.StatusNotFound, contentType: "text/html", contains: "None go here"},
		{name: "api client 404 is json", target: "/no/such/page", accept: "application/json", method: http.MethodGet, status: http.StatusNotFound, contentType: "application/json", contains: `"code":"NOT_FOUND"`},
		{name: "wildcard accept keeps json", target: "/no/such/page", accept: "*/*", method: http.MethodGet, status: http.StatusNotFound, contentType: "application/json", contains: `"code":"NOT_FOUND"`},
		{name: "api path with html accept stays json", target: "/api/v1/nope", accept: "text/html", method: http.MethodGet, status: http.StatusNotFound, contentType: "application/json", contains: `"code":"NOT_FOUND"`},
		{name: "gateway path with html accept stays json", target: "/v1/nope", accept: "text/html", method: http.MethodGet, status: http.StatusNotFound, contentType: "application/json", contains: `"code":"NOT_FOUND"`},
		{name: "post is json not found", target: "/no/such/page", accept: "text/html", method: http.MethodPost, status: http.StatusNotFound, contentType: "application/json", contains: `"code":"NOT_FOUND"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{}
			if tc.accept != "" {
				headers["Accept"] = tc.accept
			}
			rec := doRequest(t, h, tc.method, tc.target, nil, headers)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.contentType != "" {
				if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.contentType) {
					t.Fatalf("content type = %q, want prefix %q", ct, tc.contentType)
				}
			}
			if !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tc.contains)
			}
		})
	}
}

func TestStaticRejectsPathTraversal(t *testing.T) {
	web, landing := testTree(t)
	secret := filepath.Join(filepath.Dir(landing), "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := staticHandler(web, landing, "")
	for _, target := range []string{"/app/../secret.txt", "/../secret.txt", "/static/../../secret.txt"} {
		rec := doRequest(t, h, http.MethodGet, target, nil, map[string]string{"Accept": "application/json"})
		if strings.Contains(rec.Body.String(), "private") {
			t.Fatalf("%s leaked a file outside the root", target)
		}
	}
}
