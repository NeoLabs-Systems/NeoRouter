package proxies

// Admin API, ported from server/routes/proxies.js. The router is mounted behind
// the admin router's authentication and role checks in the JS server, so every
// handler here runs behind Routes.Admin, which internal/app supplies from
// internal/auth.

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// maxBodyBytes is app.js express.json({limit:'2mb'}).
const maxBodyBytes int64 = httpserver.DefaultJSONBodyLimit

// Routes mounts /api/v1/admin/proxies.
type Routes struct {
	Svc *Service
	Log *slog.Logger
	// Admin is the admin gate (authentication plus the admin role) every route
	// sits behind. It is required: without it Mount refuses every request.
	Admin func(http.Handler) http.Handler
}

// Mount registers the routes on mux.
func (rt *Routes) Mount(mux *http.ServeMux) {
	admin := rt.Admin
	if admin == nil {
		admin = func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpserver.WriteError(w, r, rt.log(), errors.New("proxies: admin middleware is not wired"))
			})
		}
	}
	for _, pattern := range []string{
		"GET /api/v1/admin/proxies",
		"GET /api/v1/admin/proxies/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handleList)))
	}
	for _, pattern := range []string{
		"POST /api/v1/admin/proxies/check",
		"POST /api/v1/admin/proxies/check/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handleCheck)))
	}
	for _, pattern := range []string{
		"PUT /api/v1/admin/proxies/policy",
		"PUT /api/v1/admin/proxies/policy/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handlePolicy)))
	}
	for _, pattern := range []string{
		"POST /api/v1/admin/proxies/import",
		"POST /api/v1/admin/proxies/import/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handleImport)))
	}
	for _, pattern := range []string{
		"POST /api/v1/admin/proxies/bulk",
		"POST /api/v1/admin/proxies/bulk/{$}",
	} {
		mux.Handle(pattern, admin(http.HandlerFunc(rt.handleBulk)))
	}
	mux.Handle("POST /api/v1/admin/proxies/{id}/test", admin(http.HandlerFunc(rt.handleTest)))
	mux.Handle("PATCH /api/v1/admin/proxies/{id}", admin(http.HandlerFunc(rt.handlePatch)))
	mux.Handle("DELETE /api/v1/admin/proxies/{id}", admin(http.HandlerFunc(rt.handleDelete)))
}

type listResponse struct {
	Proxies   []Proxy      `json:"proxies"`
	Policy    PoolPolicy   `json:"policy"`
	LastCheck *CheckResult `json:"lastCheck"`
}

type policyResponse struct {
	Policy PoolPolicy `json:"policy"`
}

type bulkResponse struct {
	Changed int `json:"changed"`
}

func (rt *Routes) handleList(w http.ResponseWriter, r *http.Request) {
	proxies, err := rt.Svc.List(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	policy, err := rt.Svc.PoolPolicy(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse{Proxies: proxies, Policy: policy, LastCheck: rt.Svc.LastCheck()})
}

func (rt *Routes) handleCheck(w http.ResponseWriter, r *http.Request) {
	result, err := rt.Svc.CheckAll(r.Context(), "")
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (rt *Routes) handlePolicy(w http.ResponseWriter, r *http.Request) {
	object, v, ok := objectBody(w, r, maxBodyBytes)
	if !ok {
		return
	}
	var policy PoolPolicy
	v.requiredBool(object, "enabled", &policy.Enabled)
	v.requiredBool(object, "fallbackDirect", &policy.FallbackDirect)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	if err := rt.Svc.SetPoolPolicy(r.Context(), policy); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	stored, err := rt.Svc.PoolPolicy(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, policyResponse{Policy: stored})
}

func (rt *Routes) handleImport(w http.ResponseWriter, r *http.Request) {
	object, v, ok := objectBody(w, r, maxBodyBytes)
	if !ok {
		return
	}
	var (
		text     string
		protocol string
	)
	v.requiredString(object, "text", 1000000, &text)
	v.enumDefault(object, "protocol", []string{"socks5", "http", "https"}, "socks5", &protocol)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	result, err := rt.Svc.ImportLines(r.Context(), text, protocol)
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (rt *Routes) handleBulk(w http.ResponseWriter, r *http.Request) {
	object, v, ok := objectBody(w, r, maxBodyBytes)
	if !ok {
		return
	}
	var (
		action string
		ids    []string
		all    bool
	)
	v.requiredEnum(object, "action", []string{"enable", "disable", "delete"}, &action)
	v.stringArray(object, "ids", 100000, 200, &ids)
	v.boolDefault(object, "all", false, &all)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	if all {
		proxies, err := rt.Svc.List(r.Context())
		if err != nil {
			httpserver.WriteError(w, r, rt.log(), err)
			return
		}
		ids = make([]string, 0, len(proxies))
		for _, proxy := range proxies {
			ids = append(ids, proxy.ID)
		}
	}
	for _, id := range ids {
		var err error
		if action == "delete" {
			err = rt.Svc.Remove(r.Context(), id)
		} else {
			err = rt.Svc.Change(r.Context(), id, action == "enable")
		}
		if err != nil {
			httpserver.WriteError(w, r, rt.log(), err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, bulkResponse{Changed: len(ids)})
}

func (rt *Routes) handleTest(w http.ResponseWriter, r *http.Request) {
	object, v, ok := objectBody(w, r, maxBodyBytes)
	if !ok {
		return
	}
	var raw string
	v.requiredURL(object, "url", 2048, &raw)
	if err := v.err(); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	target, _ := url.Parse(raw)
	private, err := rt.Svc.IsPrivateDestination(r.Context(), target.String())
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	if (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || private {
		httpserver.WriteError(w, r, rt.log(), httpserver.New(http.StatusBadRequest, "INVALID_TEST_URL",
			"Choose a public HTTP or HTTPS URL without credentials."))
		return
	}
	result, err := rt.Svc.Probe(r.Context(), r.PathValue("id"), target.String())
	if err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (rt *Routes) handlePatch(w http.ResponseWriter, r *http.Request) {
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
	if err := rt.Svc.Change(r.Context(), r.PathValue("id"), enabled); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *Routes) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := rt.Svc.Remove(r.Context(), r.PathValue("id")); err != nil {
		httpserver.WriteError(w, r, rt.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (rt *Routes) log() *slog.Logger {
	if rt.Log != nil {
		return rt.Log
	}
	return slog.Default()
}
