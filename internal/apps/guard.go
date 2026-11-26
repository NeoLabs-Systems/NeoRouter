package apps

import (
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Session is the signed-in person a request carries. internal/auth resolves it
// from the bearer token in server/middleware/auth.js requireSession.
type Session struct {
	UserID string
	Role   string // "admin" or "user"
}

// IsAdmin reports whether the session belongs to an administrator.
func (s Session) IsAdmin() bool { return s.Role == "admin" }

// Space is whose data a request works on, resolved by internal/teams from the
// X-NeoRouter-Space header in server/middleware/space.js resolveSpace.
type Space struct {
	OwnerID     string
	ActorID     string
	IsOwner     bool
	Permissions map[string]bool
}

// Allows reports whether the space grants permission.
func (s Space) Allows(permission string) bool { return s.Permissions[permission] }

// Caller is one authenticated request: the session and the space it works in.
type Caller struct {
	Session Session
	Space   Space
}

// Guard resolves who a request runs as. This domain does not import
// internal/auth or internal/teams; internal/app fills these in with them.
type Guard struct {
	// Authenticate returns the caller's session, or the error to answer with:
	// 401 AUTHENTICATION_REQUIRED "A valid session is required." when the bearer
	// token is missing or unknown. A nil session with a nil error is that error.
	Authenticate func(r *http.Request) (*Session, error)
	// Space returns whose data the request works on, or the error to answer
	// with: 404 SPACE_NOT_FOUND "This space does not exist." for a space the
	// caller does not belong to.
	Space func(r *http.Request, session *Session) (*Space, error)
}

// ErrAuthenticationRequired is what a missing session answers.
func ErrAuthenticationRequired() *httpserver.HttpError {
	return httpserver.New(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "A valid session is required.")
}

// errPermissionDenied is teams' requirePermission refusal.
func errPermissionDenied(permission string) *httpserver.HttpError {
	return httpserver.New(http.StatusForbidden, "PERMISSION_DENIED", "The team owner has not allowed this for you.",
		map[string]string{"permission": permission})
}

// errOwnSpaceOnly is teams' requireOwnSpace refusal.
func errOwnSpaceOnly() *httpserver.HttpError {
	return httpserver.New(http.StatusForbidden, "OWN_SPACE_ONLY", "Switch to your own space to do this.")
}

// errAdminRequired is auth's requireAdmin refusal.
func errAdminRequired() *httpserver.HttpError {
	return httpserver.New(http.StatusForbidden, "ADMIN_REQUIRED", "Administrator privileges are required.")
}

// guarded runs authenticate and resolveSpace before a handler, the way
// routes/index.js puts requireSession + resolveSpace in front of /apps.
func (s *Service) guarded(next func(http.ResponseWriter, *http.Request, Caller)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.guard.Authenticate == nil || s.guard.Space == nil {
			httpserver.WriteError(w, r, s.log, ErrAuthenticationRequired())
			return
		}
		session, err := s.guard.Authenticate(r)
		if err != nil || session == nil {
			if err == nil {
				err = ErrAuthenticationRequired()
			}
			httpserver.WriteError(w, r, s.log, err)
			return
		}
		space, err := s.guard.Space(r, session)
		if err != nil || space == nil {
			if err == nil {
				err = ErrAuthenticationRequired()
			}
			httpserver.WriteError(w, r, s.log, err)
			return
		}
		next(w, r, Caller{Session: *session, Space: *space})
	}
}
