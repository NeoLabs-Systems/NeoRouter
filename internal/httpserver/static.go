package httpserver

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// StaticOptions points the static handler at the built Flutter client and the
// landing site. The functions are read per request so a reload picks up new
// paths.
type StaticOptions struct {
	WebDir     func() string // flutter_app/build/web, served at /app
	LandingDir func() string // landing, served at /
	WebDevURL  func() string // NEOROUTER_WEB_DEV_URL, used when no client is built
}

var apiOrGatewayPath = regexp.MustCompile(`^/(api|v1)(/|$)`)

// Static serves the Flutter web client under /app, the landing site under /,
// and the browser 404 page (landing/404.html) while API clients keep the JSON
// error.
func Static(opts StaticOptions) http.Handler {
	webDir := opts.WebDir
	if webDir == nil {
		webDir = func() string { return "" }
	}
	landingDir := opts.LandingDir
	if landingDir == nil {
		landingDir = func() string { return "" }
	}
	devURL := opts.WebDevURL
	if devURL == nil {
		devURL = func() string { return "" }
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p == "/app" || strings.HasPrefix(p, "/app/") {
			if serveAppClient(w, r, webDir(), devURL()) {
				return
			}
			notFound(w, r, landingDir())
			return
		}
		if serveLanding(w, r, landingDir()) {
			return
		}
		notFound(w, r, landingDir())
	})
}

// serveAppClient serves a file of the Flutter web client, or the SPA entry
// point. It reports false when the request belongs to the not-found handling.
func serveAppClient(w http.ResponseWriter, r *http.Request, dir, devURL string) bool {
	rel := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/app")), "/")
	if rel != "" {
		if serveStaticFile(w, r, dir, rel) {
			return true
		}
		if path.Ext(rel) == "" && serveStaticFile(w, r, dir, rel+".html") {
			return true
		}
	}
	if !hasFile(dir, "index.html") {
		if devURL != "" {
			http.Redirect(w, r, devURL, http.StatusFound)
			return true
		}
		WriteError(w, r, nil, New(http.StatusServiceUnavailable, "WEB_UI_MISSING",
			"NeoRouter web UI assets are missing. Run `neorouter rebuild-web`."))
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	// Only pages fall back to the app; a missing script or asset is a 404,
	// never index.html run as JavaScript.
	if ext := path.Ext(rel); ext != "" && !strings.HasSuffix(rel, ".html") {
		return false
	}
	return serveStaticFile(w, r, dir, "index.html")
}

// serveLanding serves a file of the landing site, trying ".html" for
// extension-less paths.
func serveLanding(w http.ResponseWriter, r *http.Request, dir string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		rel = "index.html"
	}
	if serveStaticFile(w, r, dir, rel) {
		return true
	}
	return path.Ext(rel) == "" && serveStaticFile(w, r, dir, rel+".html")
}

// notFound answers a request no file and no route matched: browsers get the
// landing 404 page, everything else the JSON error.
func notFound(w http.ResponseWriter, r *http.Request, landingDir string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		NotFound(w)
		return
	}
	if apiOrGatewayPath.MatchString(r.URL.Path) || !prefersHTML(r) {
		NotFound(w)
		return
	}
	f, st, ok := openStatic(landingDir, "404.html")
	if !ok {
		NotFound(w)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	h.Set("Last-Modified", st.ModTime().UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, f)
	}
}

// serveStaticFile serves one file of dir. Every static file is revalidated
// (Cache-Control: private, no-cache) because the client keeps the same file
// names across releases.
func serveStaticFile(w http.ResponseWriter, r *http.Request, dir, rel string) bool {
	f, st, ok := openStatic(dir, rel)
	if !ok {
		return false
	}
	defer f.Close()
	h := w.Header()
	h.Set("Cache-Control", "private, no-cache")
	h.Set("ETag", fmt.Sprintf(`W/"%x-%x"`, st.Size(), st.ModTime().UnixMilli()))
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
	return true
}

// openStatic opens rel inside dir without allowing a path to escape the root.
func openStatic(dir, rel string) (*os.File, os.FileInfo, bool) {
	if dir == "" || rel == "" || rel == "." {
		return nil, nil, false
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, false
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return nil, nil, false
	}
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		f.Close()
		return nil, nil, false
	}
	return f, st, true
}

func hasFile(dir, rel string) bool {
	f, _, ok := openStatic(dir, rel)
	if ok {
		f.Close()
	}
	return ok
}

// prefersHTML reports whether the Accept header prefers HTML over JSON: JSON
// wins a tie.
func prefersHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	html := acceptQuality(accept, "text/html")
	json := acceptQuality(accept, "application/json")
	return html > 0 && html > json
}

func acceptQuality(header, mediaType string) float64 {
	if strings.TrimSpace(header) == "" {
		return 1 // no Accept header means */*
	}
	major, _, _ := strings.Cut(mediaType, "/")
	bestSpec := -1
	best := 0.0
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		media := strings.ToLower(strings.TrimSpace(fields[0]))
		if media == "" {
			continue
		}
		spec := -1
		switch media {
		case mediaType:
			spec = 2
		case major + "/*":
			spec = 1
		case "*/*":
			spec = 0
		}
		if spec < 0 || spec <= bestSpec {
			continue
		}
		quality := 1.0
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if !strings.HasPrefix(strings.ToLower(param), "q=") {
				continue
			}
			if q, err := strconv.ParseFloat(strings.TrimSpace(param[2:]), 64); err == nil {
				quality = q
			}
		}
		bestSpec = spec
		best = quality
	}
	return best
}
