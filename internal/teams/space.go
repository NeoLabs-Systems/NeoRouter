package teams

import (
	"context"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// SpaceHeader is the request header naming whose data a request works on.
const SpaceHeader = "X-NeoRouter-Space"

// Space says whose data a request works on and what the caller may do there.
// The owner of the space is the account whose team members, keys and providers
// are in play; a request without the header works on the caller's own space.
type Space struct {
	OwnerID     string
	ActorID     string
	IsOwner     bool
	Permissions map[string]bool
}

// Can reports whether the caller may do permission in this space.
func (s *Space) Can(permission string) bool {
	return s != nil && s.Permissions[permission]
}

func ownSpace(actorID string) *Space {
	all := make(map[string]bool, len(PERMISSIONS))
	for _, permission := range PERMISSIONS {
		all[permission] = true
	}
	return &Space{OwnerID: actorID, ActorID: actorID, IsOwner: true, Permissions: all}
}

type spaceCtxKey struct{}

// ContextWithSpace attaches space to ctx.
func ContextWithSpace(ctx context.Context, space *Space) context.Context {
	return context.WithValue(ctx, spaceCtxKey{}, space)
}

// SpaceFromContext returns the space SpaceMiddleware resolved, reporting false
// when the request was not scoped (a route mounted outside the API).
func SpaceFromContext(ctx context.Context) (*Space, bool) {
	space, ok := ctx.Value(spaceCtxKey{}).(*Space)
	return space, ok
}

// SpaceMiddleware resolves the space: without the header, or with the caller's
// own id, the request works on the caller's own space with every right;
// otherwise the caller must be a member of that owner's team and gets only what
// the owner granted. A space the caller does not belong to looks like it does
// not exist.
//
// actor reports the authenticated account, so the wiring passes auth.Actor.
func (s *Service) SpaceMiddleware(actor ActorFunc, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actorID, ok := "", false
		if actor != nil {
			actorID, ok = actor(r)
		}
		if !ok {
			httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusUnauthorized,
				"AUTHENTICATION_REQUIRED", "A valid session is required."))
			return
		}
		requested := r.Header.Get(SpaceHeader)
		if requested == "" || requested == actorID {
			next.ServeHTTP(w, r.WithContext(ContextWithSpace(r.Context(), ownSpace(actorID))))
			return
		}
		current, err := s.membership(r.Context(), s.DB.R, actorID)
		if err != nil {
			httpserver.WriteError(w, r, s.log(), err)
			return
		}
		if current == nil || current.OwnerUserID != requested {
			httpserver.WriteError(w, r, s.log(), httpserver.New(http.StatusNotFound,
				"SPACE_NOT_FOUND", "This space does not exist."))
			return
		}
		granted := make(map[string]bool, len(current.Permissions))
		for _, permission := range current.Permissions {
			granted[permission] = true
		}
		space := &Space{OwnerID: requested, ActorID: actorID, IsOwner: false, Permissions: granted}
		next.ServeHTTP(w, r.WithContext(ContextWithSpace(r.Context(), space)))
	})
}

// RequirePermission refuses a request the owner has not allowed, naming the
// permission it wanted.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			space, _ := SpaceFromContext(r.Context())
			if space.Can(permission) {
				next.ServeHTTP(w, r)
				return
			}
			httpserver.WriteError(w, r, nil, httpserver.New(http.StatusForbidden,
				"PERMISSION_DENIED", "The team owner has not allowed this for you.",
				map[string]string{"permission": permission}))
		})
	}
}

// RequireOwnSpace guards team management, admin and account endpoints: they
// only ever act on the caller's own space.
func RequireOwnSpace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		space, _ := SpaceFromContext(r.Context())
		if space != nil && space.IsOwner {
			next.ServeHTTP(w, r)
			return
		}
		httpserver.WriteError(w, r, nil, httpserver.New(http.StatusForbidden,
			"OWN_SPACE_ONLY", "Switch to your own space to do this."))
	})
}
