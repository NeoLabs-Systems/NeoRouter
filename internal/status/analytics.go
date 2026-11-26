package status

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

//go:embed analytics_client.js
var clientSource string

const (
	// ClientPath is where the browser analytics script is served.
	ClientPath = "/_neolabs/analytics.js"
	// CountlyOrigin is where the browser sends analytics, which the Content
	// Security Policy has to allow.
	CountlyOrigin = "https://data.neo.eu.com"
	// CountlyEndpoint is the NeoLabs analytics write endpoint.
	CountlyEndpoint = CountlyOrigin + "/i"
	// CountlyAppKey is the public write key of the NeoLabs Countly app.
	CountlyAppKey = "fd1d80746c13491c9744ecc96bf096da4f10cffd"

	// configPlaceholder is the token analytics_client.js leaves for the
	// endpoint and key it must not hard-code.
	configPlaceholder = "/* COUNTLY_CONFIG */ null"
)

// Config is the analytics destination handed to the browser.
type Config struct {
	Endpoint string `json:"endpoint"`
	AppKey   string `json:"appKey"`
}

// DefaultConfig is the NeoLabs Countly app.
func DefaultConfig() *Config {
	return &Config{Endpoint: CountlyEndpoint, AppKey: CountlyAppKey}
}

// AnalyticsConfig reads the analytics configuration: analytics are on unless
// ANALYTICS_ENABLED is one of false/0/off/no; nil means opted out.
func AnalyticsConfig(getenv func(string) string) *Config {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch strings.ToLower(strings.TrimSpace(getenv("ANALYTICS_ENABLED"))) {
	case "false", "0", "off", "no":
		return nil
	}
	return DefaultConfig()
}

// ClientScript is analytics_client.js with the endpoint and key injected.
func ClientScript(cfg *Config) string {
	if cfg == nil {
		return clientSource
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return clientSource
	}
	return strings.Replace(clientSource, configPlaceholder, string(body), 1)
}

// Analytics serves the browser analytics script and injects it into the pages
// the server renders itself.
//
// It is middleware because the pages it rewrites are the static handler's: wrap
// the static client handler with it and the pages of the web app and the landing
// site gain the script exactly when analytics are enabled.
type Analytics struct {
	// Getenv reads ANALYTICS_ENABLED; nil means the process environment.
	Getenv func(string) string
	// WebDir is the built Flutter client (served at /app).
	WebDir func() string
	// LandingDir is the landing site (served at /).
	LandingDir func() string
	// Log records an unexpected page read failure; nil ignores it.
	Log func(err error)
}

var (
	// appPagePath matches the web app's pages, never its assets.
	appPagePath = regexp.MustCompile(`^/app(?:/.*)?$`)
	// assetExtension matches a path whose last segment carries a file suffix.
	assetExtension = regexp.MustCompile(`(?i)\.[a-z0-9]+$`)
	// indexHTMLSuffix strips the SPA entry point before the asset test.
	indexHTMLSuffix = regexp.MustCompile(`(?i)/index\.html$`)
)

// Middleware serves the script and rewrites the two HTML pages; everything else
// goes to next untouched.
func (a *Analytics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			if r.URL.Path == ClientPath && a.serveClient(w, r) {
				return
			}
			if a.servePage(w, r) {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// serveClient answers the analytics script. It reports false when analytics are
// disabled, so the request continues to the static handler and ends as a 404
// like any other missing file.
func (a *Analytics) serveClient(w http.ResponseWriter, r *http.Request) bool {
	cfg := AnalyticsConfig(a.Getenv)
	if cfg == nil {
		return false
	}
	script := ClientScript(cfg)
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Type", "application/javascript; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(script)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(script))
	}
	return true
}

// servePage rewrites a page this server owns. It reports false when the request
// is not a page the middleware handles, so the static handler serves it.
func (a *Analytics) servePage(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	appPage := appPagePath.MatchString(path) && !assetExtension.MatchString(indexHTMLSuffix.ReplaceAllString(path, ""))
	landingPage := path == "/" || path == "/index.html"
	cfg := AnalyticsConfig(a.Getenv)
	if !appPage && (!landingPage || cfg == nil) {
		return false
	}
	dir := a.landingDir()
	if appPage {
		dir = a.webDir()
	}
	page, err := readPage(dir)
	if err != nil {
		// A missing page is the static handler's 404; anything else is worth a
		// log line but still not this middleware's answer.
		if a.Log != nil && !os.IsNotExist(err) {
			a.Log(err)
		}
		return false
	}
	if appPage {
		page = strings.Replace(page, `<base href="/">`, `<base href="/app/">`, 1)
		if !strings.Contains(page, "/js/cookie-notice.js") {
			page = insertBeforeHeadEnd(page, `<script defer src="/js/cookie-notice.js"></script>`)
		}
	}
	if cfg != nil {
		page = insertBeforeHeadEnd(page, `<script defer src="`+ClientPath+`"></script>`)
	}
	// Like every web file: revalidated on each load, so a new release or a
	// changed setting is picked up.
	h := w.Header()
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(page)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(page))
	}
	return true
}

func (a *Analytics) webDir() string {
	if a.WebDir == nil {
		return ""
	}
	return a.WebDir()
}

func (a *Analytics) landingDir() string {
	if a.LandingDir == nil {
		return ""
	}
	return a.LandingDir()
}

// readPage reads the index.html of dir.
func readPage(dir string) (string, error) {
	if dir == "" {
		return "", os.ErrNotExist
	}
	raw, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// insertBeforeHeadEnd puts tag and a newline before the first </head>, matched
// case-insensitively.
func insertBeforeHeadEnd(page, tag string) string {
	at := strings.Index(strings.ToLower(page), "</head>")
	if at < 0 {
		return page
	}
	return page[:at] + tag + "\n" + page[at:]
}
