package httpserver

import (
	"net/http"
	"regexp"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// localOrigin matches the development origins server/middleware/cors.js trusts.
var localOrigin = regexp.MustCompile(`^https?://(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$`)

const (
	corsExposeHeaders = "X-Request-Id,X-NeoRouter-Model,X-NeoRouter-Group,X-NeoRouter-Trail,X-NeoRouter-Task," +
		"X-NeoRouter-Concurrency-Limit,X-NeoRouter-Concurrency-Available,x-ratelimit-limit-requests," +
		"x-ratelimit-remaining-requests,x-ratelimit-reset-requests,Retry-After"
	corsAllowHeaders = "Authorization,Content-Type,X-Api-Key,Anthropic-Version,Anthropic-Beta,OpenAI-Organization," +
		"OpenAI-Beta,X-Request-Id,X-NeoRouter-Space,X-NeoRouter-Workload"
	corsAllowMethods = "GET,POST,PUT,PATCH,DELETE,OPTIONS"
)

// OriginAllowed reports whether an Origin header value may call this server:
// an exact entry of NEOROUTER_ALLOWED_ORIGINS, the wildcard entry, or a
// localhost/127.0.0.1/[::1] origin on any port. The list is read through
// getenv on every request, as the JS middleware does.
func OriginAllowed(origin string, getenv func(string) string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range config.AllowedOrigins(getenv) {
		if allowed == origin || allowed == "*" {
			return true
		}
	}
	return localOrigin.MatchString(origin)
}

// CORS ports server/middleware/cors.js. A request without an Origin, or from
// an origin that is not allowed, gets no CORS headers but still reaches its
// handler; every OPTIONS request answers 204.
func CORS(getenv func(string) string, next http.Handler) http.Handler {
	getenv = getenvOr(getenv)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && OriginAllowed(origin, getenv) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
			h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
			h.Set("Access-Control-Allow-Methods", corsAllowMethods)
			h.Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
