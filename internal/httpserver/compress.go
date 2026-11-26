package httpserver

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

// GzipMinBytes is the compression threshold: smaller bodies are sent as they
// are.
const GzipMinBytes = 1024

// Gzip compresses responses when the client accepts gzip, unless the body is
// already encoded, is a streaming text/event-stream, is not a compressible
// type, or stays below GzipMinBytes. Like compression(), the decision is
// deferred until the threshold is reached, so streamed JSON and static files
// keep the same compression behaviour.
//
// The gateway is mounted outside this middleware because it streams responses.
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || isUpgrade(r) || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w, minBytes: GzipMinBytes}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

// isUpgrade reports a protocol upgrade (WebSocket live voice, pool relay),
// which must reach the handler with the untouched ResponseWriter.
func isUpgrade(r *http.Request) bool {
	return r.Header.Get("Upgrade") != "" && strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// acceptsGzip reports whether the Accept-Encoding header offers gzip at a
// non-zero quality.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		coding := strings.ToLower(strings.TrimSpace(fields[0]))
		if coding != "gzip" && coding != "x-gzip" {
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
		return quality > 0
	}
	return false
}

// compressible reports whether a Content-Type benefits from compression.
func compressible(contentType string) bool {
	ct := strings.ToLower(contentType)
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if ct == "" || ct == "text/event-stream" {
		return false
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/javascript", "application/x-javascript", "application/xml",
		"application/xhtml+xml", "application/manifest+json", "application/wasm", "image/svg+xml",
		"application/rss+xml", "application/atom+xml":
		return true
	}
	return strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml")
}

type gzipResponseWriter struct {
	http.ResponseWriter
	minBytes int

	gz         *gzip.Writer
	pending    []byte
	status     int
	decided    bool
	compress   bool
	headerSent bool
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.status != 0 || g.headerSent {
		return
	}
	g.status = code
	if code >= 100 && code < 200 {
		g.ResponseWriter.WriteHeader(code)
		return
	}
	h := g.ResponseWriter.Header()
	if code == http.StatusNoContent || code == http.StatusNotModified ||
		h.Get("Content-Encoding") != "" || !compressible(h.Get("Content-Type")) {
		g.plain()
		return
	}
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n < g.minBytes {
			g.plain()
			return
		}
		// The size is known and worth compressing: decide before any byte is sent.
		g.startCompress()
	}
}

func (g *gzipResponseWriter) Write(p []byte) (int, error) {
	if g.status == 0 {
		g.WriteHeader(http.StatusOK)
	}
	if g.headerSent {
		if g.compress {
			return g.gz.Write(p)
		}
		return g.ResponseWriter.Write(p)
	}
	if len(g.pending)+len(p) < g.minBytes {
		g.pending = append(g.pending, p...)
		return len(p), nil
	}
	g.pending = append(g.pending, p...)
	g.startCompress()
	if _, err := g.gz.Write(g.pending); err != nil {
		return 0, err
	}
	g.pending = nil
	return len(p), nil
}

// Flush implements http.Flusher. A handler that flushes before the threshold
// gets an uncompressed response, which keeps streaming endpoints readable.
func (g *gzipResponseWriter) Flush() {
	if !g.headerSent && g.status != 0 {
		g.plain()
	}
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (g *gzipResponseWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipResponseWriter) plain() {
	g.decided = true
	g.headerSent = true
	g.ResponseWriter.WriteHeader(g.status)
	if len(g.pending) > 0 {
		_, _ = g.ResponseWriter.Write(g.pending)
		g.pending = nil
	}
}

func (g *gzipResponseWriter) startCompress() {
	g.decided = true
	g.compress = true
	g.headerSent = true
	h := g.ResponseWriter.Header()
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	g.gz = gzip.NewWriter(g.ResponseWriter)
	g.ResponseWriter.WriteHeader(g.status)
}

func (g *gzipResponseWriter) close() {
	if !g.headerSent {
		if g.status == 0 {
			return // the handler sent nothing; net/http answers with an empty 200
		}
		g.plain()
		return
	}
	if g.compress && g.gz != nil {
		_ = g.gz.Close()
	}
}
