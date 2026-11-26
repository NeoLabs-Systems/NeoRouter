package catalog

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// ModelMetrics are the routing-side numbers the models API adds to one model.
type ModelMetrics struct {
	Reliability  float64
	SpeedScore   float64
	CostScore    float64
	Availability any
	Penalty      float64
	Successes    float64
	Failures     float64
}

// Routing is the slice of the routing domain the models routes consume:
// the scores and the availability of one model, and clearing a cooldown.
type Routing interface {
	// ModelMetrics returns the scores of one model.
	ModelMetrics(ctx context.Context, ref int64) ModelMetrics
	// ClearCooldown forgives a model's cooldown and open breaker.
	ClearCooldown(ctx context.Context, ref int64)
}

// Capability is one routable kind of model.
type Capability struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Routes serves /api/v1/models and /api/v1/model-presets. Authentication is
// applied by the router the handlers are mounted on; the space permission of
// the managing routes is checked here.
type Routes struct {
	Svc *Service
	Log *slog.Logger
	// Routing supplies the per-model routing numbers. Routes without it report
	// zeros.
	Routing Routing
	// LiveProtocols lists the WebSocket dialects a live model's account speaks
	// (accounts.adapterFor(runtime).definition.live), empty when it has none.
	LiveProtocols func(ctx context.Context, accountID string) []string
	// Capabilities lists the routable kinds, the ones a model can be served by.
	Capabilities func() []Capability
	// OwnerID returns the space owner of the request (middleware/space).
	OwnerID func(r *http.Request) string
	// UserID returns the authenticated user of the request (req.auth.userId).
	UserID func(r *http.Request) string
	// RequirePermission guards a handler with a space permission.
	RequirePermission func(permission string, next http.Handler) http.Handler
}

// Mount registers the catalog routes.
func (rt *Routes) Mount(mux *http.ServeMux) {
	manage := func(next http.Handler) http.Handler {
		if rt.RequirePermission == nil {
			return next
		}
		return rt.RequirePermission("models.manage", next)
	}
	mux.Handle("GET /api/v1/models", http.HandlerFunc(rt.handleList))
	mux.Handle("GET /api/v1/models/capabilities", http.HandlerFunc(rt.handleCapabilities))
	mux.Handle("POST /api/v1/models/bulk", manage(http.HandlerFunc(rt.handleBulk)))
	mux.Handle("GET /api/v1/models/{ref}", http.HandlerFunc(rt.handleGet))
	mux.Handle("PATCH /api/v1/models/{ref}", manage(http.HandlerFunc(rt.handlePatch)))
	mux.Handle("POST /api/v1/models/{ref}/reset-health", manage(http.HandlerFunc(rt.handleResetHealth)))
}

// presentedModel is one model with the routing numbers the API adds.
type presentedModel struct {
	Entry
	// LiveProtocols is present (an array) for live models only.
	LiveProtocols any     `json:"liveProtocols,omitempty"`
	Reliability   float64 `json:"reliability"`
	SpeedScore    float64 `json:"speedScore"`
	CostScore     float64 `json:"costScore"`
	Availability  any     `json:"availability"`
}

// present adds the live protocols and the routing scores to one entry.
func (rt *Routes) present(ctx context.Context, entry Entry) presentedModel {
	presented := presentedModel{Entry: entry}
	if entry.Capability == "live" {
		protocols := []string{}
		if rt.LiveProtocols != nil {
			if live := rt.LiveProtocols(ctx, entry.AccountID); live != nil {
				protocols = live
			}
		}
		presented.LiveProtocols = protocols
	}
	if rt.Routing != nil {
		metrics := rt.Routing.ModelMetrics(ctx, entry.Ref)
		presented.Reliability = metrics.Reliability
		presented.SpeedScore = metrics.SpeedScore
		presented.CostScore = metrics.CostScore
		presented.Availability = metrics.Availability
		presented.Stats.Penalty = metrics.Penalty
		presented.Stats.Successes = metrics.Successes
		presented.Stats.Failures = metrics.Failures
	}
	return presented
}

// single returns a query parameter the caller passed exactly once.
func single(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func (rt *Routes) handleList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	capability := "chat"
	if value, ok := single(query["capability"]); ok {
		capability = value
	}
	all, _ := single(query["all"])
	entries, err := rt.Svc.ListForUser(r.Context(), rt.ownerID(r), capability, all == "1")
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	models := make([]presentedModel, 0, len(entries))
	for _, entry := range entries {
		models = append(models, rt.present(r.Context(), entry))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"models": models})
}

// capabilityEntry is one row of the capability summary.
type capabilityEntry struct {
	ID       string `json:"id"`
	Total    int64  `json:"total"`
	Free     int64  `json:"free"`
	Routable bool   `json:"routable"`
}

func (rt *Routes) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	var capabilities []Capability
	if rt.Capabilities != nil {
		capabilities = rt.Capabilities()
	}
	routable := make(map[string]bool, len(capabilities))
	order := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if routable[capability.ID] {
			continue
		}
		routable[capability.ID] = true
		order = append(order, capability.ID)
	}
	summary, err := rt.Svc.CapabilitySummary(r.Context(), rt.ownerID(r))
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	entries := make([]capabilityEntry, 0, len(summary)+len(order))
	for _, item := range summary {
		entries = append(entries, capabilityEntry{ID: item.ID, Total: item.Total, Free: item.Free, Routable: routable[item.ID]})
	}
	// ?all=1 also lists the routable kinds nothing is connected for yet.
	if all, ok := single(r.URL.Query()["all"]); ok && all == "1" {
		for _, id := range order {
			present := false
			for _, entry := range entries {
				if entry.ID == id {
					present = true
					break
				}
			}
			if !present {
				entries = append(entries, capabilityEntry{ID: id, Routable: true})
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"capabilities": entries})
}

func (rt *Routes) handleGet(w http.ResponseWriter, r *http.Request) {
	ref, ok := modelRef(r.PathValue("ref"))
	if !ok {
		rt.fail(w, r, modelNotFound())
		return
	}
	entry, err := rt.Svc.GetModel(r.Context(), rt.ownerID(r), ref)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"model": rt.present(r.Context(), entry)})
}

func (rt *Routes) handlePatch(w http.ResponseWriter, r *http.Request) {
	ref, ok := modelRef(r.PathValue("ref"))
	if !ok {
		rt.fail(w, r, modelNotFound())
		return
	}
	body, err := readObject(w, r)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	update, err := validateModelPatch(body)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	entry, err := rt.Svc.UpdateModel(r.Context(), rt.ownerID(r), ref, update)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"model": rt.present(r.Context(), entry)})
}

func (rt *Routes) handleResetHealth(w http.ResponseWriter, r *http.Request) {
	ref, ok := modelRef(r.PathValue("ref"))
	if !ok {
		rt.fail(w, r, modelNotFound())
		return
	}
	entry, err := rt.Svc.GetModel(r.Context(), rt.ownerID(r), ref)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	if rt.Routing != nil {
		rt.Routing.ClearCooldown(r.Context(), entry.Ref)
	}
	updated, err := rt.Svc.GetModel(r.Context(), rt.ownerID(r), entry.Ref)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"model": rt.present(r.Context(), updated)})
}

func (rt *Routes) handleBulk(w http.ResponseWriter, r *http.Request) {
	body, err := readObject(w, r)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	accountID, enabled, onlyFree, err := validateBulk(body)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	result, err := rt.Svc.BulkSetEnabled(r.Context(), rt.ownerID(r), accountID, enabled, onlyFree)
	if err != nil {
		rt.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (rt *Routes) ownerID(r *http.Request) string {
	if rt.OwnerID == nil {
		return ""
	}
	return rt.OwnerID(r)
}

// fail writes an error in the management error shape.
func (rt *Routes) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.WriteError(w, r, rt.logger(), err)
}
