package apps

import (
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Routes registers the settings → "connect your apps" endpoints of
// server/routes/apps.js under /api/v1/apps. They are mounted behind
// requireSession + resolveSpace, like routes/index.js mounts /apps.
func (s *Service) Routes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/apps", s.guarded(s.list))
	mux.Handle("GET /api/v1/apps/{$}", s.guarded(s.list))
	mux.Handle("GET /api/v1/apps/keys/{id}/activity", s.guarded(s.keyActivity))
	mux.Handle("PUT /api/v1/apps/network", s.guarded(s.setNetwork))
}

// catalogResponse is the settings page load: what can be connected, where, and
// who may reach NeoRouter.
type catalogResponse struct {
	Apps    []App         `json:"apps"`
	Places  []Place       `json:"places"`
	Network NetworkStatus `json:"network"`
}

// networkResponse is a network change with the addresses it produces.
type networkResponse struct {
	ScopeResult
	Places []Place `json:"places"`
}

// browserOrigin is the address the browser used, to offer it to apps on that
// same device: `${req.protocol}://${req.get('host')}`.
func (s *Service) browserOrigin(r *http.Request) string {
	return requestProtocol(r, s.cfg().TrustProxy) + "://" + r.Host
}

func (s *Service) list(w http.ResponseWriter, r *http.Request, _ Caller) {
	httpx.WriteJSON(w, http.StatusOK, catalogResponse{
		Apps:    All(),
		Places:  s.Places(s.browserOrigin(r)),
		Network: s.NetworkStatus(),
	})
}

func (s *Service) keyActivity(w http.ResponseWriter, r *http.Request, caller Caller) {
	if !caller.Space.Allows("keys.view") {
		httpserver.WriteError(w, r, s.log, errPermissionDenied("keys.view"))
		return
	}
	activity, err := s.Activity(r.Context(), caller.Space.OwnerID, r.PathValue("id"))
	if err != nil {
		httpserver.WriteError(w, r, s.log, fmt.Errorf("apps: key activity: %w", err))
		return
	}
	if activity == nil {
		httpserver.WriteError(w, r, s.log, httpserver.New(http.StatusNotFound, "KEY_NOT_FOUND", "This key does not exist."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, activity)
}

func (s *Service) setNetwork(w http.ResponseWriter, r *http.Request, caller Caller) {
	if !caller.Space.IsOwner {
		httpserver.WriteError(w, r, s.log, errOwnSpaceOnly())
		return
	}
	if !caller.Session.IsAdmin() {
		httpserver.WriteError(w, r, s.log, errAdminRequired())
		return
	}
	var body scopeBody
	if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
		return
	}
	scope, err := parseScope(body.Scope)
	if err != nil {
		httpserver.WriteError(w, r, s.log, err)
		return
	}
	result, err := s.SetScope(scope)
	if err != nil {
		httpserver.WriteError(w, r, s.log, fmt.Errorf("apps: set network scope: %w", err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, networkResponse{ScopeResult: result, Places: s.Places(s.browserOrigin(r))})
}
