package auth

import (
	"context"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Auth is the authenticated caller of a request.
type Auth struct {
	Type      string     `json:"type"`
	SessionID string     `json:"sessionId"`
	UserID    string     `json:"userId"`
	User      PublicUser `json:"user"`
}

type authCtxKey struct{}

// ContextWithAuth attaches auth to ctx.
func ContextWithAuth(ctx context.Context, value *Auth) context.Context {
	return context.WithValue(ctx, authCtxKey{}, value)
}

// FromContext returns the authenticated caller, reporting false when the
// request did not pass the session middleware.
func FromContext(ctx context.Context) (*Auth, bool) {
	value, ok := ctx.Value(authCtxKey{}).(*Auth)
	return value, ok
}

// Actor reports the account a request is authenticated as. The wiring hands it
// to teams.SpaceMiddleware, which needs the caller's id and nothing else.
func Actor(r *http.Request) (string, bool) {
	value, ok := FromContext(r.Context())
	if !ok || value == nil {
		return "", false
	}
	return value.UserID, true
}

// Middleware is requireSession: it resolves the bearer session and refuses the
// request without one.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller := s.AuthenticateToken(r.Context(), bearerToken(r))
		if caller == nil {
			httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
				"AUTHENTICATION_REQUIRED", "A valid session is required."))
			return
		}
		next.ServeHTTP(w, r.WithContext(ContextWithAuth(r.Context(), caller)))
	})
}

// AdminOnly is requireAdmin: the caller must be signed in as an administrator.
func (s *Service) AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := FromContext(r.Context())
		if !ok || caller == nil || caller.User.Role != "admin" {
			httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusForbidden,
				"ADMIN_REQUIRED", "Administrator privileges are required."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the address of the client, honouring the trusted-proxy setting.
func (s *Service) clientIP(r *http.Request) string {
	return httpx.ClientIP(r, s.cfg().TrustProxy)
}

// requestContext reads the client metadata stored with a session.
func (s *Service) requestContext(r *http.Request) RequestContext {
	return RequestContext{IPAddress: s.clientIP(r), UserAgent: r.Header.Get("User-Agent")}
}
