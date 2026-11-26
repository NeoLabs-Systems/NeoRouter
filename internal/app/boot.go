package app

// The route tree and the boot sequence of server/app.js + server/index.js: the
// public sign-in endpoints, the session-scoped API in the order
// server/routes/index.js mounts it, the gateway outside the compression
// middleware, the static site last.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/auth"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/gateway"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/localmodels"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/proxies"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/scheduler"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
)

// mountManagement registers everything behind the root mux: the shell's own
// routes, the public API, the gateway (outside compression), the management API
// and the root-level pages.
func (d *deps) mountManagement(mux *http.ServeMux) {
	d.mountGateway(d.gatewayMux)

	// The sign-in endpoints and the connect pages are outside the session
	// middleware (routes/index.js mounts them before requireSession).
	d.Auth.Routes(mux)
	d.OAuth.Routes(mux)

	// The API the session and the space scope: requireSession → resolveSpace.
	api := http.NewServeMux()
	d.mountAPI(api)
	mux.Handle("/api/v1/", d.Auth.Middleware(d.Teams.SpaceMiddleware(auth.Actor, api)))

	// Root-level pages: pools federation, the invite page, the gateway
	// reference, the public status and the static site (app.js order).
	d.Pools.FederationRoutes(mux)
	d.Pools.JoinRoute(mux)
	d.Status.Routes(mux)
	d.mountGatewayMounts(mux)
}

// mountAPI registers the session-scoped endpoints in the order
// server/routes/index.js mounts them.
func (d *deps) mountAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/info", d.handleInfo)

	d.Teams.Routes(mux)
	d.Pools.Routes(mux)
	d.Accounts.Routes(mux, providers.RouteDeps{
		Space:   providersSpace,
		Require: requirePermissionFunc(),
	})
	d.Discovery.Routes(mux, requirePermissionHandler)
	d.mountCatalog(mux)
	d.Keys.Routes(mux)
	d.Apps.Routes(mux)
	d.OAuth.ManagementRoutes(mux)
	d.Routing.Routes(mux, routing.RouteDeps{
		Owner: func(r *http.Request) (ownerID, actorID string) {
			space, _ := teams.SpaceFromContext(r.Context())
			if space == nil {
				return "", ""
			}
			return space.OwnerID, space.ActorID
		},
		Require: requirePermissionError,
	})
	d.Usage.Routes(mux)
	d.Media.PlaygroundRoutes(mux)
	d.Processing.Routes(mux)
	d.mountAdmin(mux)
	// Anything else under /api/v1 is the management 404 (error_handler.js
	// notFound), not the mux's own text answer.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		httpserver.NotFound(w)
	})
}

// mountCatalog mounts the models and model-preset routes with their cross-domain
// numbers.
func (d *deps) mountCatalog(mux *http.ServeMux) {
	cache := newRefCache()
	d.refCache = cache
	routes := &catalog.Routes{
		Svc: d.Catalog,
		Log: d.log,
		Routing: catalogMetrics{
			svc:   d.Routing,
			cache: cache,
			now:   d.now,
		},
		LiveProtocols: func(ctx context.Context, accountID string) []string {
			return gatewayAccounts{accounts: d.Accounts, registry: d.Registry}.LiveProtocols(accountID)
		},
		Capabilities: func() []catalog.Capability {
			out := make([]catalog.Capability, 0, len(gateway.Capabilities))
			for _, capability := range gateway.Capabilities {
				out = append(out, catalog.Capability{ID: capability.ID, Label: capability.Label})
			}
			return out
		},
		OwnerID: func(r *http.Request) string {
			space, _ := teams.SpaceFromContext(r.Context())
			if space == nil {
				return ""
			}
			return space.OwnerID
		},
		UserID: func(r *http.Request) string {
			return userIDOf(r)
		},
		RequirePermission: requirePermissionHandler,
	}
	routes.Mount(mux)
	routes.MountPresets(mux)
}

// mountAdmin registers /api/v1/admin: the caller must be in their own space and
// be an administrator (app.js mounts requireOwnSpace + requireAdmin).
func (d *deps) mountAdmin(mux *http.ServeMux) {
	admin := http.NewServeMux()
	d.Settings.Routes(admin)
	d.mountAdminUsers(admin)
	(&proxies.Routes{Svc: d.Proxies, Log: d.log, Admin: adminGate(d.Auth)}).Mount(admin)
	(&localmodels.Routes{Svc: d.Local, Log: d.log, Admin: adminGate(d.Auth), UserID: auth.Actor}).Mount(admin)
	d.Updates.Routes(admin, func(next http.Handler) http.Handler { return next })
	d.mountMetadataRefresh(admin)
	admin.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		httpserver.NotFound(w)
	})
	mux.Handle("/api/v1/admin/", teams.RequireOwnSpace(d.Auth.AdminOnly(admin)))
}

// mountAdminUsers registers the user administration of server/routes/admin.js.
func (d *deps) mountAdminUsers(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		users, err := d.adminUsers(r)
		if err != nil {
			httpserver.WriteError(w, r, d.log, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": users})
	})
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Disabled *bool   `json:"disabled"`
			Role     *string `json:"role"`
		}
		if err := httpserver.ReadJSONManagement(w, r, &body, httpserver.DefaultJSONBodyLimit); err != nil {
			return
		}
		actorID := userIDOf(r)
		if body.Disabled != nil {
			if err := d.Auth.SetUserDisabled(r.Context(), actorID, r.PathValue("id"), *body.Disabled); err != nil {
				httpserver.WriteError(w, r, d.log, err)
				return
			}
		}
		if body.Role != nil {
			if err := d.Auth.SetUserRole(r.Context(), actorID, r.PathValue("id"), *body.Role); err != nil {
				httpserver.WriteError(w, r, d.log, err)
				return
			}
		}
		users, err := d.adminUsers(r)
		if err != nil {
			httpserver.WriteError(w, r, d.log, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": users})
	})
	mux.HandleFunc("DELETE /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := d.Auth.DeleteUser(r.Context(), userIDOf(r), r.PathValue("id")); err != nil {
			httpserver.WriteError(w, r, d.log, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/admin/team-audit", func(w http.ResponseWriter, r *http.Request) {
		events, err := d.Teams.AuditList(r.Context(), teams.ParseLimit(r.URL.Query().Get("limit")))
		if err != nil {
			httpserver.WriteError(w, r, d.log, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
	})
}

// adminUsers is listUsers of server/routes/admin.js: every account with the
// owner of the team it is in.
func (d *deps) adminUsers(r *http.Request) ([]adminUser, error) {
	users, err := d.Auth.ListUsers(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]adminUser, 0, len(users))
	for _, user := range users {
		item := adminUser{PublicUser: user}
		membership, memberErr := d.Teams.Membership(r.Context(), user.ID)
		if memberErr == nil && membership != nil && membership.OwnerUserID != "" {
			owner, ownerErr := d.Teams.TeamUser(r.Context(), membership.OwnerUserID)
			if ownerErr == nil {
				item.TeamOwner = owner
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// adminUser is one account of GET /api/v1/admin/users.
type adminUser struct {
	auth.PublicUser
	TeamOwner *teams.TeamUser `json:"teamOwner"`
}

// mountMetadataRefresh registers POST /api/v1/admin/metadata/refresh.
func (d *deps) mountMetadataRefresh(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/admin/metadata/refresh", func(w http.ResponseWriter, r *http.Request) {
		results, err := d.Catalog.Metadata.RefreshSources(r.Context())
		if err != nil {
			httpserver.WriteError(w, r, d.log, err)
			return
		}
		status, statusErr := d.Catalog.Metadata.Status()
		if statusErr != nil {
			httpserver.WriteError(w, r, d.log, statusErr)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": results, "metadata": status})
	})
}

// handleInfo is GET /api/v1/info: the version, the gateway address and the
// routable kinds.
func (d *deps) handleInfo(w http.ResponseWriter, r *http.Request) {
	origin := publicOrigin(d.cfg().PublicURL, r)
	httpx.WriteJSON(w, http.StatusOK, infoResponse{
		Version:      d.version,
		Endpoint:     origin + "/v1",
		Capabilities: gateway.List(),
	})
}

// mountGateway registers the gateway's routes on the mux that is served outside
// the compression middleware.
func (d *deps) mountGateway(mux *http.ServeMux) {
	d.Gateway.Routes(mux)
	d.Media.Routes(mux)
	d.Live.Routes(mux)
	// A path under /v1 that no route claims is the same management 404 the JS
	// server answers with once the gateway router falls through.
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		httpserver.NotFound(w)
	})
}

// mountGatewayMounts sends the gateway's own management paths (the playground
// chat and the Markdown reference) to that same mux from the root, since it is
// otherwise only reachable under /v1.
func (d *deps) mountGatewayMounts(mux *http.ServeMux) {
	inner := d.gatewayMux
	mux.Handle("/api/v1/playground/chat", httpserver.Gzip(inner))
	mux.Handle("/api/docs", httpserver.Gzip(inner))
	mux.Handle("/api", httpserver.Gzip(inner))
}

// publicOrigin is utils/public_origin.js: the configured address, else the
// request's own.
func publicOrigin(publicURL string, r *http.Request) string {
	if trimmed := strings.TrimSpace(publicURL); trimmed != "" {
		return strings.TrimRight(trimmed, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return scheme + "://" + r.Host
}

type infoResponse struct {
	Version      string                   `json:"version"`
	Endpoint     string                   `json:"endpoint"`
	Capabilities []gateway.CapabilityInfo `json:"capabilities"`
}

// providersSpace is the request's space in the shape the providers routes read.
func providersSpace(r *http.Request) (providers.Space, bool) {
	space, ok := teams.SpaceFromContext(r.Context())
	if !ok || space == nil {
		return providers.Space{}, false
	}
	role := ""
	if session, ok := auth.FromContext(r.Context()); ok && session != nil {
		role = session.User.Role
	}
	return providers.Space{OwnerID: space.OwnerID, ActorID: space.ActorID, IsOwner: space.IsOwner, Role: role}, true
}

// adminGate is the admin guard the proxies routes require.
func adminGate(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return svc.AdminOnly(next)
	}
}

// buildScheduler wires every background task of server/workers/scheduler.js.
func (d *deps) buildScheduler() *scheduler.Service {
	svc := &scheduler.Service{
		Cfg: d.cfg,
		Log: d.log,
	}
	forEachAccount := func(ctx context.Context, task func(context.Context, string) error) error {
		ids, err := d.Accounts.AllEnabledIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := task(ctx, id); err != nil {
				d.log.Warn("Account task failed", "accountId", id, "error", err)
			}
		}
		return nil
	}
	svc.QuotaFlush = func() {
		if err := d.Quota.Flush(false); err != nil {
			d.log.Warn("quota flush", "error", err)
		}
	}
	svc.ValidateKeys = func(ctx context.Context) error {
		return forEachAccount(ctx, func(ctx context.Context, accountID string) error {
			_, err := d.Accounts.ValidateAccount(ctx, accountID)
			return err
		})
	}
	svc.VerifyBilling = func(ctx context.Context) error {
		return forEachAccount(ctx, func(ctx context.Context, accountID string) error {
			_, err := d.Accounts.VerifyAccount(ctx, accountID)
			return err
		})
	}
	svc.SyncModels = func(ctx context.Context) error {
		return forEachAccount(ctx, func(ctx context.Context, accountID string) error {
			_, err := d.Catalog.SyncAccount(ctx, accountID)
			return err
		})
	}
	svc.RefreshMetadata = func(ctx context.Context) error {
		if _, err := d.Catalog.Metadata.RefreshSources(ctx); err != nil {
			return err
		}
		// The news feed reads the metadata the refresh just stored.
		return d.Discovery.News.Observe(ctx)
	}
	svc.ProbeQuotas = func(ctx context.Context) error {
		return forEachAccount(ctx, func(ctx context.Context, accountID string) error {
			_, err := d.Accounts.ProbeQuota(ctx, accountID)
			return err
		})
	}
	svc.ProbePlans = func(ctx context.Context) error {
		ids, err := d.Accounts.PrepaidIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := d.Accounts.ProbeQuota(ctx, id); err != nil {
				d.log.Warn("Plan probe failed", "accountId", id, "error", err)
			}
		}
		return nil
	}
	svc.CheckUpdates = func(ctx context.Context) error {
		d.Updates.Check(ctx)
		return nil
	}
	svc.PruneLogs = func(ctx context.Context) error {
		return d.Usage.Prune(logRetentionDays, time.Now())
	}
	svc.CheckProxies = func(ctx context.Context) error {
		_, err := d.Proxies.CheckAll(ctx, d.Proxies.Policy().CheckURL)
		return err
	}
	svc.SyncPools = func(ctx context.Context) error {
		d.Pools.SyncAll(ctx)
		return nil
	}
	svc.ProxyCheckInterval = func() time.Duration {
		return time.Duration(d.Proxies.Policy().CheckEveryMs) * time.Millisecond
	}
	return svc
}

// logRetentionDays is usage.prune(LOG_RETENTION_DAYS) of scheduler.js.
const logRetentionDays = 90

var _ = slog.Default
