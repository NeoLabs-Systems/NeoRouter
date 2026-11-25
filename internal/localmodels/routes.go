package localmodels

// routes.go is server/routes/local_models.js: admin → local models — what this
// server can run, installing, starting, stopping, repairing and removing
// models, who may use them, and the privacy filter (a local model too, run
// inside NeoRouter).
//
// The router is mounted behind the admin router's authentication and role
// checks in the JS server, so every handler here runs behind Routes.Admin,
// which internal/app supplies from internal/auth.

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// maxBodyBytes is app.js express.json({limit:'2mb'}).
const maxBodyBytes int64 = httpserver.DefaultJSONBodyLimit

// Routes mounts /api/v1/admin/local-models.
type Routes struct {
	Svc *Service
	Log *slog.Logger
	// Admin is the admin gate (authentication plus the admin role) every route
	// sits behind. It is required: without it Mount refuses every request.
	Admin func(http.Handler) http.Handler
	// UserID reports who the request is authenticated as (auth.Actor); the
	// installing admin gets a local models connection.
	UserID func(r *http.Request) (string, bool)
}

// Mount registers the routes on mux.
func (rt *Routes) Mount(mux *http.ServeMux) {
	admin := rt.Admin
	if admin == nil {
		admin = func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpserver.WriteError(w, r, rt.log(), errors.New("localmodels: admin middleware is not wired"))
			})
		}
	}
	// The collection: the page, the two switches and the starter set. They are
	// registered before the {id} patterns, which Go's mux prefers anyway.
	for _, pattern := range []string{
		"GET /api/v1/admin/local-models",
		"GET /api/v1/admin/local-models/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handleOverview)))
	}
	for _, pattern := range []string{
		"PUT /api/v1/admin/local-models/access",
		"PUT /api/v1/admin/local-models/access/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handleAccess)))
	}
	for _, pattern := range []string{
		"PUT /api/v1/admin/local-models/privacy-filter",
		"PUT /api/v1/admin/local-models/privacy-filter/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handlePrivacyFilter)))
	}
	for _, pattern := range []string{
		"POST /api/v1/admin/local-models/starter",
		"POST /api/v1/admin/local-models/starter/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handleStarter)))
	}
	mux.Handle("POST /api/v1/admin/local-models/{id}/install", admin(http.HandlerFunc(rt.handleInstall)))
	mux.Handle("POST /api/v1/admin/local-models/{id}/start", admin(http.HandlerFunc(rt.handleStart)))
	mux.Handle("POST /api/v1/admin/local-models/{id}/stop", admin(http.HandlerFunc(rt.handleStop)))
	mux.Handle("POST /api/v1/admin/local-models/{id}/retry", admin(http.HandlerFunc(rt.handleRetry)))
	mux.Handle("POST /api/v1/admin/local-models/{id}/repair", admin(http.HandlerFunc(rt.handleRepair)))
	mux.Handle("DELETE /api/v1/admin/local-models/{id}", admin(http.HandlerFunc(rt.handleRemove)))
}

func (rt *Routes) handleOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := rt.Svc.Overview(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, overview)
}

func (rt *Routes) handleAccess(w http.ResponseWriter, r *http.Request) {
	object, v, ok := objectBody(w, r, maxBodyBytes)
	if !ok {
		return
	}
	var level string
	v.requiredEnum(object, "level", AccessLevels, &level)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	overview, err := rt.Svc.SetAccess(r.Context(), level)
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, overview)
}

func (rt *Routes) handlePrivacyFilter(w http.ResponseWriter, r *http.Request) {
	object, v, ok := objectBody(w, r, maxBodyBytes)
	if !ok {
		return
	}
	var enabled bool
	v.requiredBool(object, "enabled", &enabled)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	overview, err := rt.Svc.SetPrivacyFilter(r.Context(), enabled)
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, overview)
}

func (rt *Routes) handleStarter(w http.ResponseWriter, r *http.Request) {
	userID, _ := rt.userID(r)
	overview, err := rt.Svc.InstallStarter(r.Context(), userID)
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, overview)
}

func (rt *Routes) handleInstall(w http.ResponseWriter, r *http.Request) {
	userID, _ := rt.userID(r)
	overview, err := rt.Svc.Install(r.Context(), userID, []string{r.PathValue("id")})
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, overview)
}

func (rt *Routes) handleStart(w http.ResponseWriter, r *http.Request) {
	rt.setEnabled(w, r, true)
}

func (rt *Routes) handleStop(w http.ResponseWriter, r *http.Request) {
	rt.setEnabled(w, r, false)
}

func (rt *Routes) setEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	overview, err := rt.Svc.SetEnabled(r.Context(), r.PathValue("id"), enabled)
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, overview)
}

func (rt *Routes) handleRetry(w http.ResponseWriter, r *http.Request) {
	overview, err := rt.Svc.Retry(r.Context(), r.PathValue("id"))
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, overview)
}

func (rt *Routes) handleRepair(w http.ResponseWriter, r *http.Request) {
	overview, err := rt.Svc.Repair(r.Context(), r.PathValue("id"))
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, overview)
}

func (rt *Routes) handleRemove(w http.ResponseWriter, r *http.Request) {
	overview, err := rt.Svc.Remove(r.Context(), r.PathValue("id"))
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, overview)
}

// userID is the authenticated caller, or "" before the wiring supplies one.
func (rt *Routes) userID(r *http.Request) (string, bool) {
	if rt.UserID == nil {
		return "", false
	}
	return rt.UserID(r)
}

func (rt *Routes) log() *slog.Logger {
	if rt.Log != nil {
		return rt.Log
	}
	return slog.Default()
}
