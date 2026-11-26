package httpserver

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/secheaders"
)

// ShellOptions configures the middleware every request passes through.
type ShellOptions struct {
	Log    *slog.Logger
	Getenv func(string) string
	// MaxBodyBytes is the configured gateway body limit, which the playground
	// (the gateway used from the dashboard) shares.
	MaxBodyBytes func() int64
	// AnalyticsOrigin is where the web pages send analytics; empty when off.
	AnalyticsOrigin string
}

// SecurityHeaders is the browser policy of the dashboard, the landing site and
// the server-rendered pages. The sources beyond 'self' are what the Flutter web
// client loads: CanvasKit from gstatic and its fonts from Google Fonts. Only
// this server may frame a page, which protects the sign-in and consent screens.
// There is no HSTS: self-hosted installs are often plain HTTP, and the proxy
// that terminates TLS owns that header.
func SecurityHeaders(analyticsOrigin string) secheaders.Config {
	connect := []string{"'self'", "ws:", "wss:", "https://www.gstatic.com", "https://fonts.googleapis.com", "https://fonts.gstatic.com"}
	if analyticsOrigin != "" {
		connect = append(connect, analyticsOrigin)
	}
	return secheaders.Config{
		PermissionsPolicy: "camera=(self), geolocation=(self), microphone=(self), payment=(), usb=()",
		CSP: secheaders.Policy{
			{Name: "default-src", Values: []string{"'self'"}},
			{Name: "base-uri", Values: []string{"'self'"}},
			{Name: "font-src", Values: []string{"'self'", "data:", "https://fonts.gstatic.com"}},
			{Name: "form-action", Values: []string{"'self'"}},
			{Name: "frame-ancestors", Values: []string{"'self'"}},
			{Name: "img-src", Values: []string{"'self'", "data:", "blob:"}},
			{Name: "object-src", Values: []string{"'none'"}},
			{Name: "script-src", Values: []string{"'self'", "'unsafe-inline'", "'unsafe-eval'", "'wasm-unsafe-eval'", "blob:", "https://www.gstatic.com"}},
			{Name: "script-src-attr", Values: []string{"'unsafe-inline'"}},
			{Name: "style-src", Values: []string{"'self'", "'unsafe-inline'", "https://fonts.googleapis.com"}},
			{Name: "media-src", Values: []string{"'self'", "data:", "blob:"}},
			{Name: "connect-src", Values: connect},
			{Name: "worker-src", Values: []string{"'self'", "blob:"}},
		},
	}
}

// Shell wraps the whole server: panic recovery, security headers, request ids,
// uncacheable API answers, CORS and the body limit.
func Shell(next http.Handler, opts ShellOptions) http.Handler {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return httpx.Chain(next,
		httpx.Recover(log),
		secheaders.Middleware(SecurityHeaders(opts.AnalyticsOrigin)),
		crossOriginGateway,
		joinForwardedFor,
		RequestID,
		httpx.NoStoreAPI(),
		func(h http.Handler) http.Handler { return CORS(opts.Getenv, h) },
		limitBodies(opts.MaxBodyBytes),
	)
}

// crossOriginGateway keeps what the gateway serves (generated media among it)
// usable by pages of other origins; the rest stays same-site.
func crossOriginGateway(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
		}
		next.ServeHTTP(w, r)
	})
}

// joinForwardedFor folds repeated X-Forwarded-For lines into one, so that
// httpx.ClientIP sees the entry the nearest proxy appended even when it added a
// line of its own.
func joinForwardedFor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if lines := r.Header.Values("X-Forwarded-For"); len(lines) > 1 {
			r.Header.Set("X-Forwarded-For", strings.Join(lines, ", "))
		}
		next.ServeHTTP(w, r)
	})
}

// limitBodies caps what the management API reads, whether or not a handler
// does so itself. The gateway (/v1) applies its own configured limits.
func limitBodies(gatewayLimit func() int64) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasPrefix(r.URL.Path, "/api/v1/playground/"):
				limit := PlaygroundMediaBodyLimit
				if gatewayLimit != nil {
					limit = max(limit, gatewayLimit())
				}
				httpx.MaxBody(limit)(next).ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, "/api/"):
				httpx.MaxBody(DefaultJSONBodyLimit)(next).ServeHTTP(w, r)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}
