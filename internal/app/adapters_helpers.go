package app

// The smaller seams: the quota store's catalog source, the spaces the routes
// read, the key store's budget figures, the pool key issuer, the media and live
// account adapters, and the two gateway hooks that read the request log.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/apps"
	"github.com/NeoLabs-Systems/neorouter/internal/auth"
	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/gateway"
	"github.com/NeoLabs-Systems/neorouter/internal/live"
	"github.com/NeoLabs-Systems/neorouter/internal/localmodels"
	"github.com/NeoLabs-Systems/neorouter/internal/pools"
	"github.com/NeoLabs-Systems/neorouter/internal/processing"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// quotaCatalogSource is the catalog's rows in the shape the quota store counts
// capacity over.
type quotaCatalogSource struct {
	svc *catalog.Service
}

func (c quotaCatalogSource) ListForUser(ctx context.Context, userID, capability string, includeUnavailable bool) ([]quota.Entry, error) {
	entries, err := c.svc.ListForUser(ctx, userID, capability, includeUnavailable)
	if err != nil {
		return nil, err
	}
	out := make([]quota.Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, quotaEntryOf(entry))
	}
	return out, nil
}

// usageSpace is the request's space in the shape the usage routes read.
func usageSpace(ctx context.Context) (usage.Space, bool) {
	space, ok := teams.SpaceFromContext(ctx)
	if !ok || space == nil {
		return usage.Space{}, false
	}
	return usage.Space{OwnerID: space.OwnerID, ActorID: space.ActorID}, true
}

// keysSpace is the request's space in the shape the key routes read.
func keysSpace(ctx context.Context) (routerkeys.Space, bool) {
	space, ok := teams.SpaceFromContext(ctx)
	if !ok || space == nil {
		return routerkeys.Space{}, false
	}
	return routerkeys.Space{OwnerID: space.OwnerID, ActorID: space.ActorID}, true
}

// routerkeysBudget is the spend ledger in the shape the key routes report it.
type routerkeysBudget struct {
	svc *budget.Service
}

func (b routerkeysBudget) Spent(routerKeyID, budgetPeriod string, now time.Time) (float64, error) {
	return b.svc.Spent(budget.Key{ID: routerKeyID, BudgetPeriod: budgetPeriod}, now)
}

func (b routerkeysBudget) TokensUsed(routerKeyID, tokenPeriod string, now time.Time) (int64, error) {
	return b.svc.TokensUsed(budget.Key{ID: routerKeyID, TokenPeriod: tokenPeriod}, now)
}

func (b routerkeysBudget) PeriodEnd(period string, now time.Time) *int64 {
	return budget.PeriodEnd(period, now)
}

func (b routerkeysBudget) Pace(routerKeyID string, budgetUSD *float64, budgetPeriod string, now time.Time) *routerkeys.Pace {
	pace, err := b.svc.Pacing(budget.Key{ID: routerKeyID, BudgetUSD: budgetUSD, BudgetPeriod: budgetPeriod}, now)
	if err != nil || pace == nil {
		return nil
	}
	return &routerkeys.Pace{State: pace.State, Forecast: pace.Forecast}
}

// poolKeyIssuer issues the router key a pool partner calls this instance with:
// lowest priority, free models only, the balanced profile.
type poolKeyIssuer struct {
	keys *routerkeys.Keys
}

func (i poolKeyIssuer) CreatePoolKey(ctx context.Context, userID, name string) (string, string, error) {
	priority := "low"
	profile := "balanced"
	allowFree := true
	allowPlan := false
	allowPaid := false
	in := routerkeys.Input{
		Name:      routerkeys.Field[string]{Set: true, Value: &name},
		Priority:  routerkeys.Field[string]{Set: true, Value: &priority},
		Profile:   routerkeys.Field[string]{Set: true, Value: &profile},
		AllowFree: routerkeys.Field[bool]{Set: true, Value: &allowFree},
		AllowPlan: routerkeys.Field[bool]{Set: true, Value: &allowPlan},
		AllowPaid: routerkeys.Field[bool]{Set: true, Value: &allowPaid},
	}
	key, secret, err := i.keys.Create(userID, in, "", nil)
	if err != nil {
		return "", "", err
	}
	return key.ID, secret, nil
}

// poolsCatalogEntry is a pool model in the catalog's shape, for the quota store.
func poolsCatalogEntry(model pools.Model) catalog.Entry {
	return catalog.Entry{
		Ref:          model.Ref,
		AccountID:    model.AccountID,
		UserID:       model.UserID,
		DefinitionID: model.DefinitionID,
		IsFree:       model.IsFree,
		IsPlan:       model.IsPlan,
	}
}

// poolsRoutingEntry is a pool model in the router's shape, for the concurrency
// and load figures.
func poolsRoutingEntry(model pools.Model) *routing.Entry {
	return &routing.Entry{
		Ref:          model.Ref,
		AccountID:    model.AccountID,
		UserID:       model.UserID,
		DefinitionID: model.DefinitionID,
		IsFree:       model.IsFree,
		IsPlan:       model.IsPlan,
	}
}

// assessSensitivity judges how sensitive a request is, for the pool privacy
// rule (processing/sensitivity).
func assessSensitivity(svc *processing.Service) func(canonical *gateway.Obj) (string, []string) {
	return func(canonical *gateway.Obj) (string, []string) {
		assessment := processing.Assess(svc.Engine, processing.Canonical(canonicalMap(canonical)))
		return assessment.Level, assessment.Reasons
	}
}

// accountsAdapter is the provider account service as media reads it.
type accountsAdapter struct {
	accounts *providers.Service
}

func (a accountsAdapter) Adapter(accountID string) providers.Adapter {
	ctx := context.Background()
	runtime, err := a.accounts.Runtime(ctx, accountID)
	if err != nil || runtime == nil {
		return nil
	}
	adapter, err := a.accounts.AdapterFor(ctx, runtime)
	if err != nil {
		return nil
	}
	return adapter
}

// liveAccounts resolves a catalog entry's provider connection for the live
// relay: the account's own values plus the definition it was created from.
type liveAccounts struct {
	accounts *providers.Service
	registry *providers.Registry
}

func (l liveAccounts) Runtime(accountID string) (live.Connection, bool) {
	runtime, err := l.accounts.Runtime(context.Background(), accountID)
	if err != nil || runtime == nil {
		return live.Connection{}, false
	}
	def, ok := l.registry.Get(runtime.DefinitionID)
	if !ok {
		return live.Connection{}, false
	}
	return live.Connection{Definition: def, Account: runtime.Account}, true
}

// privacyStatusOf is the processing privacy model's status in the shape the
// local-model page reports it.
func privacyStatusOf(status processing.PrivacyStatus) localmodels.PrivacyStatus {
	out := localmodels.PrivacyStatus{
		Enabled:  status.Enabled,
		Model:    status.Model,
		MemoryMB: int64(status.MemoryMB),
		Status:   status.Status,
		Progress: map[string]any{},
	}
	if status.Progress != nil {
		out.Progress["percent"] = *status.Progress
	}
	if status.Error != nil {
		out.Error = *status.Error
	}
	return out
}

// recordKeyFailure remembers why a request with a valid key failed before it
// reached a model (apps/key_activity).
func (d *deps) recordKeyFailure(keyID string, status int, code, message, model string) {
	if d.Apps == nil {
		return
	}
	d.Apps.RecordFailure(keyID, apps.Failure{Status: status, Code: code, Message: message, Model: model})
}

// recordFeedback teaches routing how a model did on a kind of task.
//
// The routing domain keeps its task experience store unexported, so the wiring
// writes through a second store over the same table: the feedback is stored and
// the learning views read it, but routing's in-process cache picks it up only
// after a restart. See the W4 report (routing should export RecordFeedback).
func (d *deps) recordFeedback(modelRef int64, taskName string, good bool) {
	experience := task.NewExperience(d.db)
	experience.RecordFeedback(modelRef, taskName, good, d.now())
}

// quotasView is GET /api/v1/usage/quotas: per provider account, the shared
// allowance windows plus every model that has a known limit
// (server/routes/usage.js).
func (d *deps) quotasView(userID string, now time.Time) (any, error) {
	ctx := context.Background()
	accounts, err := d.Accounts.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	groups := make([]*quotaAccountView, 0, len(accounts))
	byID := map[string]*quotaAccountView{}
	for _, account := range accounts {
		group := &quotaAccountView{
			Account:     account,
			Pool:        []quota.Detail{},
			Models:      []quotaModelView{},
			Probes:      []quota.State{},
			Concurrency: concurrencyOf(d.Routing, account.ID, account.DefinitionID, now),
		}
		probes, probeErr := d.Quota.AccountStateNow(account.ID, now)
		if probeErr == nil {
			group.Probes = probes
		}
		groups = append(groups, group)
		byID[account.ID] = group
	}
	entries, err := d.catalogEntriesFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		group, ok := byID[entry.AccountID]
		if !ok {
			continue
		}
		if entry.IsFlatRate && group.Slots == nil {
			snapshot := d.Routing.SlotSnapshot(entry.AccountID, now)
			group.Slots = &snapshot
		}
		evaluation, evalErr := d.Quota.Evaluate(quotaEntryOf(entry), 0, now)
		if evalErr != nil {
			continue
		}
		var own []quota.Detail
		seen := map[string]bool{}
		for _, detail := range evaluation.Details {
			if detail.Kind == quota.KindPool {
				if !seen[detail.Metric] {
					seen[detail.Metric] = true
					group.Pool = append(group.Pool, detail)
				}
				continue
			}
			own = append(own, detail)
		}
		if len(own) > 0 || entry.IsFree {
			group.Models = append(group.Models, quotaModelView{
				Ref:     entry.Ref,
				ID:      entry.ID,
				Name:    entry.Name,
				IsFree:  entry.IsFree,
				Windows: append([]quota.Detail{}, own...),
			})
		}
	}
	out := make([]quotaAccountView, 0, len(groups))
	for _, group := range groups {
		group.Limits = summarizeLimits(group.Pool, group.Models)
		out = append(out, *group)
	}
	return map[string]any{"accounts": out}, nil
}

// catalogEntriesFor lists every kind of model a person can route to.
func (d *deps) catalogEntriesFor(ctx context.Context, userID string) ([]catalog.Entry, error) {
	var out []catalog.Entry
	for _, capability := range gateway.Capabilities {
		entries, err := d.Catalog.ListForUser(ctx, userID, capability.ID, false)
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	return out, nil
}

// concurrencyOf is concurrency.limitFor for one account.
func concurrencyOf(svc *routing.Service, accountID, definitionID string, now time.Time) *routing.ConcurrencyLimit {
	return svc.LimitFor(&routing.Entry{AccountID: accountID, DefinitionID: definitionID}, now)
}

// quotaAccountView is one provider account of GET /usage/quotas, in the key
// order the dashboard reads.
type quotaAccountView struct {
	Account       providers.PublicAccount   `json:"account"`
	Pool          []quota.Detail            `json:"pool"`
	Models        []quotaModelView          `json:"models"`
	Probes        []quota.State             `json:"probes"`
	Concurrency   *routing.ConcurrencyLimit `json:"concurrency"`
	Slots         *routing.SlotSnapshot     `json:"slots,omitempty"`
	AllowancePace *float64                  `json:"allowancePace,omitempty"`
	Limits        []quotaLimitRow           `json:"limits"`
}

// quotaModelView is one model's windows inside an account of GET /usage/quotas.
type quotaModelView struct {
	Ref     int64          `json:"ref"`
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	IsFree  bool           `json:"isFree"`
	Windows []quota.Detail `json:"windows"`
}

// quotaLimitRow is one metric of an account's limits, as a person reads it.
type quotaLimitRow struct {
	Metric   string              `json:"metric"`
	Shared   *quotaSharedLimit   `json:"shared"`
	PerModel *quotaPerModelLimit `json:"perModel"`
}

type quotaSharedLimit struct {
	Limit  *float64 `json:"limit"`
	Source string   `json:"source"`
}

type quotaPerModelLimit struct {
	Min     float64  `json:"min"`
	Max     float64  `json:"max"`
	Models  int      `json:"models"`
	Sources []string `json:"sources"`
}

// limitMetrics are the windows a provider allowance is stated in, in the order
// the page lists them (limit_summary.js).
var limitMetrics = []string{quota.RPM, quota.RPD, quota.TPM, quota.TPD}

// limitSourceOrder is the strongest first (limit_summary.js SOURCE_ORDER).
var limitSourceOrder = []string{quota.SourceManual, quota.SourceHeader, quota.SourceLearned, quota.SourceCurated}

// summarizeLimits is server/services/quota/limit_summary.js: per metric, what
// the whole account shares and what each model gets.
func summarizeLimits(pool []quota.Detail, models []quotaModelView) []quotaLimitRow {
	out := []quotaLimitRow{}
	for _, metric := range limitMetrics {
		var shared *quota.Detail
		for i := range pool {
			if pool[i].Metric == metric && isLimitDetail(pool[i]) {
				shared = &pool[i]
				break
			}
		}
		var own []quota.Detail
		for _, model := range models {
			for _, window := range model.Windows {
				if window.Metric == metric && isLimitDetail(window) {
					own = append(own, window)
				}
			}
		}
		if shared == nil && len(own) == 0 {
			continue
		}
		row := quotaLimitRow{Metric: metric}
		if shared != nil {
			row.Shared = &quotaSharedLimit{Limit: shared.Limit, Source: shared.Source}
		}
		if len(own) > 0 {
			min, max := 0.0, 0.0
			for i, window := range own {
				value := 0.0
				if window.Limit != nil {
					value = *window.Limit
				}
				if i == 0 || value < min {
					min = value
				}
				if i == 0 || value > max {
					max = value
				}
			}
			row.PerModel = &quotaPerModelLimit{Min: min, Max: max, Models: len(own), Sources: sourcesOf(own)}
		}
		out = append(out, row)
	}
	return out
}

// isLimitDetail reports a window that states a real limit: a window without a
// positive limit is only an "out until later" marker.
func isLimitDetail(detail quota.Detail) bool {
	return detail.Limit != nil && *detail.Limit > 0 && detail.Source != quota.SourceError
}

// sourcesOf lists where a metric's limits came from, strongest first.
func sourcesOf(windows []quota.Detail) []string {
	seen := map[string]bool{}
	for _, window := range windows {
		seen[window.Source] = true
	}
	out := []string{}
	for _, source := range limitSourceOrder {
		if seen[source] {
			out = append(out, source)
		}
	}
	return out
}

var _ = json.Marshal

// timePtrOf is an epoch-millisecond instant as a pointer to time.Time.
func timePtrOf(value *int64) *time.Time {
	if value == nil {
		return nil
	}
	converted := time.UnixMilli(*value)
	return &converted
}

// userIDOf is req.auth.userId of the session middleware.
func userIDOf(r *http.Request) string {
	userID, ok := auth.Actor(r)
	if !ok {
		return ""
	}
	return userID
}

// usageCatalogLookup is the catalog's rows in the shape the usage views read.
func usageCatalogLookup(svc *catalog.Service) func(ctx context.Context, userID, capability string, includeUnavailable bool) ([]usage.CatalogEntry, error) {
	return func(ctx context.Context, userID, capability string, includeUnavailable bool) ([]usage.CatalogEntry, error) {
		entries, err := svc.ListForUser(ctx, userID, capability, includeUnavailable)
		if err != nil {
			return nil, err
		}
		out := make([]usage.CatalogEntry, 0, len(entries))
		for _, entry := range entries {
			out = append(out, usageEntryOf(entry))
		}
		return out, nil
	}
}
