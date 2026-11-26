package app

// The routing domain's consumers: the catalog it plans over, the quota store,
// the spend ledger, the billing observations, the pool share policy, the
// settings the router reads, the account limits people stated, the provider
// definitions' documented concurrency, and the router keys.

import (
	"context"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/catalog"
	"github.com/NeoLabs-Systems/neorouter/internal/pools"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/settings"
)

// routingCatalogView is the catalog as the routing domain asks for it: one
// person's models of one kind, in routing's own entry shape.
type routingCatalogView struct {
	svc *catalog.Service
	// cache keeps the rows it just returned, so the models route's scores have
	// the entry for the ref it asks about.
	cache *refCache
}

func (c routingCatalogView) ListForUser(userID string, q routing.CatalogQuery) []*routing.Entry {
	capability := q.Capability
	if capability == "" {
		capability = "chat"
	}
	entries, err := c.svc.ListForUser(context.Background(), userID, capability, q.IncludeUnavailable)
	if err != nil {
		return nil
	}
	out := make([]*routing.Entry, 0, len(entries))
	for _, entry := range entries {
		if c.cache != nil {
			c.cache.put(entry)
		}
		out = append(out, routingEntryOf(entry))
	}
	return out
}

// CanonicalID normalises a model id to the identity shared across providers
// (catalog/model_identity.canonicalId).
//
// The catalog keeps that function unexported, so the benchmark identity — the
// same canonical id, mapped through the benchmark record when one matched — is
// what the router sees. See the W4 report: catalog should export CanonicalID.
func (c routingCatalogView) CanonicalID(modelID string) string {
	return c.svc.Metadata.BenchmarkIdentity(modelID, "chat")
}

// routingQuota is the quota store in the shape routing plans against.
type routingQuota struct {
	svc *quota.Service
}

func (q routingQuota) ScopesFor(entry *routing.Entry) []routing.QuotaScope {
	scopes := q.svc.ScopesFor(quotaEntryOf(routingToCatalogEntry(entry)))
	out := make([]routing.QuotaScope, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, routing.QuotaScope{
			Key:     scope.Key,
			Account: scope.AccountID,
			Kind:    scope.Kind,
			Curated: scope.Curated,
		})
	}
	return out
}

func (q routingQuota) Evaluate(entry *routing.Entry, estimatedTokens int, now time.Time) routing.QuotaEvaluation {
	evaluation, err := q.svc.Evaluate(quotaEntryOf(routingToCatalogEntry(entry)), float64(estimatedTokens), now)
	out := routing.QuotaEvaluation{Headroom: 1, Factor: 1}
	if err != nil {
		return out
	}
	out.Headroom = evaluation.Headroom
	out.Factor = evaluation.Factor
	if evaluation.Blocked != nil {
		out.Blocked = &routing.QuotaBlocked{
			Metric:  evaluation.Blocked.Metric,
			Scope:   evaluation.Blocked.Scope,
			ResetAt: timePtrOf(evaluation.Blocked.ResetAt),
		}
	}
	for _, detail := range evaluation.Details {
		out.Details = append(out.Details, routing.QuotaDetail{
			Scope:     detail.Scope,
			Kind:      detail.Kind,
			Metric:    detail.Metric,
			Limit:     detail.Limit,
			Used:      detail.Used,
			Remaining: detail.Remaining,
			Source:    detail.Source,
			ResetAt:   timePtrOf(detail.ResetAt),
		})
	}
	return out
}

func (q routingQuota) UsageInLast(scopeKey string, minutes int, now time.Time) routing.QuotaUsage {
	totals, err := q.svc.UsageInLast(scopeKey, minutes, now)
	if err != nil {
		return routing.QuotaUsage{}
	}
	return routing.QuotaUsage{Requests: float64(totals.Requests), Tokens: float64(totals.Tokens)}
}

func (q routingQuota) DailyLimits(scope routing.QuotaScope, now time.Time) routing.QuotaDailyLimits {
	rpd, tpd, err := q.svc.DailyLimits(quotaScopeOf(scope), now)
	out := routing.QuotaDailyLimits{}
	if err != nil {
		return out
	}
	if rpd != nil {
		out.RPD = *rpd
	}
	if tpd != nil {
		out.TPD = *tpd
	}
	return out
}

func (q routingQuota) DailyWindow(scope routing.QuotaScope, metric string, now time.Time) *routing.QuotaDailyWindow {
	window, err := q.svc.DailyWindow(quotaScopeOf(scope), metric, now)
	if err != nil || window == nil {
		return nil
	}
	out := &routing.QuotaDailyWindow{
		Scope:     window.Scope,
		Metric:    window.Metric,
		Limit:     window.Limit,
		Used:      window.Used,
		Remaining: window.Remaining,
		Recent:    window.Recent,
		Expiring:  window.Expiring,
		Rolling:   window.Rolling,
		StartAt:   time.UnixMilli(window.StartAt),
	}
	if window.ResetAt != nil {
		reset := time.UnixMilli(*window.ResetAt)
		out.ResetAt = &reset
	}
	return out
}

func (q routingQuota) EffectiveLimits(scope routing.QuotaScope) map[string]routing.QuotaLimit {
	limits, err := q.svc.EffectiveLimits(quotaScopeOf(scope))
	if err != nil {
		return nil
	}
	out := make(map[string]routing.QuotaLimit, len(limits))
	for metric, info := range limits {
		out[metric] = routing.QuotaLimit{Value: info.Value, Source: info.Source}
	}
	return out
}

func (q routingQuota) NextReleaseAt(scopeKey string, minutes int, now time.Time) *time.Time {
	at, err := q.svc.NextReleaseAt(scopeKey, minutes, now)
	if err != nil || at == nil {
		return nil
	}
	release := time.UnixMilli(*at)
	return &release
}

func (q routingQuota) AccountState(accountID string) []routing.QuotaWindow {
	states, err := q.svc.AccountState(accountID)
	if err != nil {
		return nil
	}
	out := make([]routing.QuotaWindow, 0, len(states))
	for _, state := range states {
		out = append(out, routing.QuotaWindow{
			Scope:       state.Scope,
			Metric:      state.Metric,
			Limit:       state.Limit,
			Remaining:   state.Remaining,
			ResetAt:     state.ResetAt,
			Source:      state.Source,
			ObservedAt:  state.ObservedAt,
			LocalUsed:   state.LocalUsed,
			RequestCost: state.RequestCost,
		})
	}
	return out
}

func (q routingQuota) SetState(scopeKey, metric string, state routing.QuotaWindow, source string, now time.Time) {
	_ = q.svc.SetState(scopeKey, metric, quota.Observation{
		Metric:    metric,
		Limit:     state.Limit,
		Remaining: state.Remaining,
		ResetAt:   state.ResetAt,
	}, source, now)
}

func (q routingQuota) RecordPlanRequest(accountID string, now time.Time) {
	_ = q.svc.RecordPlanRequest(accountID, now)
}

func (q routingQuota) ObserveHeaders(entry *routing.Entry, headers http.Header, now time.Time) {
	_ = q.svc.ObserveHeaders(quotaEntryOf(routingToCatalogEntry(entry)), map[string][]string(headers), now)
}

func (q routingQuota) LearnLimit(entry *routing.Entry, learned routing.LearnedLimit) {
	_ = q.svc.LearnLimit(quotaEntryOf(routingToCatalogEntry(entry)), quota.Observation{
		Metric: learned.Metric,
		Limit:  &learned.Value,
	})
}

func (q routingQuota) MarkExhausted(entry *routing.Entry, metric string, resetAt *time.Time, now time.Time) {
	var at *int64
	if resetAt != nil {
		value := resetAt.UnixMilli()
		at = &value
	}
	_ = q.svc.MarkExhausted(quotaEntryOf(routingToCatalogEntry(entry)), metric, at, now)
}

func (q routingQuota) ClearObservedExhaustion(accountID string, refs []int64) {
	_ = q.svc.ClearObservedExhaustion(accountID, refs)
}

// quotaScopeOf is a routing scope as the quota store names it.
func quotaScopeOf(scope routing.QuotaScope) quota.Scope {
	return quota.Scope{
		Key:       scope.Key,
		Kind:      scope.Kind,
		AccountID: scope.Account,
		Curated:   scope.Curated,
	}
}

// routingBudget is the spend ledger in the shape the router reads.
type routingBudget struct {
	svc *budget.Service
}

func (b routingBudget) EstimateMaxCost(entry *routing.Entry, inputTokens, maxOutputTokens int) *float64 {
	return budget.EstimateMaxCost(budget.Price{
		IsFree:          entry.IsFree,
		IsPlan:          entry.IsPlan,
		PriceIn:         entry.PriceIn,
		PriceOut:        entry.PriceOut,
		PriceCachedIn:   entry.PriceCachedIn,
		PriceCacheWrite: entry.PriceCacheWrite,
	}, int64(inputTokens), int64(maxOutputTokens))
}

func (b routingBudget) Remaining(key routing.RouterKey, now time.Time) *float64 {
	remaining, err := b.svc.Remaining(budgetKey(key), now)
	if err != nil {
		return nil
	}
	return remaining
}

func (b routingBudget) TokenLimitReached(key routing.RouterKey, now time.Time) bool {
	reached, err := b.svc.TokenLimitReached(budgetKey(key), now)
	return err == nil && reached
}

func (b routingBudget) Pacing(key routing.RouterKey, now time.Time) *routing.Pacing {
	pace, err := b.svc.Pacing(budgetKey(key), now)
	if err != nil || pace == nil {
		return nil
	}
	out := &routing.Pacing{
		State:         pace.State,
		CostShift:     pace.CostShift,
		PerRequestCap: pace.PerRequestCap,
		Spent:         pace.Spent,
		Remaining:     pace.Remaining,
		Forecast:      pace.Forecast,
	}
	if pace.ResetsAt != nil {
		at := time.UnixMilli(*pace.ResetsAt)
		out.ResetsAt = &at
	}
	return out
}

// budgetKey is a router key as the ledger names it.
func budgetKey(key routing.RouterKey) budget.Key {
	out := budget.Key{
		ID:           key.ID,
		BudgetUSD:    key.BudgetUSD,
		BudgetPeriod: key.BudgetPeriod,
		TokenPeriod:  key.TokenPeriod,
	}
	if key.TokenLimit != nil {
		limit := int64(*key.TokenLimit)
		out.TokenLimit = &limit
	}
	return out
}

// routingBilling is the provider domain's traffic observations, which teach the
// catalog and the quota store what real traffic proved.
type routingBilling struct {
	accounts *providers.Service
}

func (b routingBilling) ObserveHeaders(entry *routing.Entry, headers http.Header) {
	b.accounts.ObserveHeaders(context.Background(), observableOf(entry), headers)
}

func (b routingBilling) ObserveFailure(entry *routing.Entry, failure routing.Failure) {
	b.accounts.ObserveFailure(context.Background(), observableOf(entry), failureError(failure))
}

// observableOf is the slice of a routing entry billing_check.js reads.
func observableOf(entry *routing.Entry) providers.Observable {
	if entry == nil {
		return providers.Observable{}
	}
	return providers.Observable{
		Ref:          entry.Ref,
		ID:           entry.ID,
		AccountID:    entry.AccountID,
		DefinitionID: entry.DefinitionID,
		BillingMode:  providers.BillingMode(entry.BillingMode),
		IsFree:       entry.IsFree,
	}
}

// failureError renders a routing failure as the classified provider error the
// accounts domain keeps.
func failureError(failure routing.Failure) error {
	options := providers.ErrorOptions{
		Status:      failure.Status,
		Headers:     failure.Headers,
		NeedsAction: failure.NeedsAction,
		RetryAfter:  failure.RetryAfter,
	}
	if failure.Learned != nil {
		options.Learned = &providers.LearnedLimit{Metric: failure.Learned.Metric, Value: failure.Learned.Value}
	}
	return providers.NewError(providers.Kind(failure.Kind), failure.Message, options)
}

// routingPools is the pool share policy in the shape the router reads.
type routingPools struct {
	svc *pools.Service
}

func (p routingPools) IsPeerEntry(entry *routing.Entry) bool {
	if entry == nil {
		return false
	}
	return pools.IsPeerEntry(entry.DefinitionID)
}

func (p routingPools) ShareRejection(entry *routing.Entry, req routing.ShareRequest, now time.Time) string {
	return p.svc.Rejection(context.Background(), poolsModelOf(routingToCatalogEntry(entry)), pools.RejectOptions{
		InputTokens: float64(req.InputTokens),
		Reserve:     req.Reserve,
		PoolID:      req.PoolID,
	}, now)
}

// routingSettings is the settings store in the shape the router reads it.
type routingSettings struct {
	svc *settings.Service
}

func (s routingSettings) UserSettingIs(userID, key string) bool {
	raw, ok, err := s.svc.GetUserSetting(context.Background(), userID, key)
	if err != nil || !ok {
		return false
	}
	return string(raw) == "true"
}

func (s routingSettings) SetUserSetting(userID, key string, value any) error {
	return s.svc.SetUserSetting(context.Background(), userID, key, value)
}

// routingAccountLimits is what people stated for their connections, as the
// router reads it.
type routingAccountLimits struct {
	limits *quota.AccountLimits
}

func (l routingAccountLimits) Stated(accountID string) routing.StatedLimits {
	values := l.limits.Get(accountID)
	return routing.StatedLimits{
		RPM:        int(values[quota.RPM]),
		RPD:        int(values[quota.RPD]),
		TPM:        int(values[quota.TPM]),
		TPD:        int(values[quota.TPD]),
		Concurrent: int(values["concurrent"]),
	}
}

// routingDefinitions is the provider registry as the router reads a
// definition's documented concurrency.
type routingDefinitions struct {
	registry *providers.Registry
}

func (d routingDefinitions) DocumentedConcurrency(entry *routing.Entry) int {
	if entry == nil {
		return 0
	}
	def, ok := d.registry.Get(entry.DefinitionID)
	if !ok {
		return 0
	}
	if entry.SlotPlan != "" && def.Billing.FlatRate != nil {
		for _, plan := range def.Billing.FlatRate.Plans {
			if plan.ID == entry.SlotPlan && plan.Concurrent > 0 {
				return plan.Concurrent
			}
		}
	}
	return def.Limits.Account.Concurrent
}

// routingKeys is the router-key store as the router reads a key's per-minute
// allowance and load.
type routingKeys struct {
	keys *routerkeys.Keys
}

func (k routingKeys) RPMState(key routing.RouterKey, now time.Time) *routing.RPMState {
	state := k.keys.RPMStateOf(routingKeyRow(key), now)
	if state == nil {
		return nil
	}
	return &routing.RPMState{Limit: int(state.Limit), Remaining: int(state.Remaining), ResetSeconds: state.ResetSeconds}
}

func (k routingKeys) InFlight(key routing.RouterKey) int {
	if k.keys.Concurrency == nil {
		return 0
	}
	return int(k.keys.Concurrency.InFlight(key.ID))
}

// routingKeyRow is a routing key as the key store stores it.
func routingKeyRow(key routing.RouterKey) *routerkeys.Row {
	row := &routerkeys.Row{
		ID:             key.ID,
		UserID:         key.UserID,
		Name:           key.Name,
		Mode:           key.Mode,
		BudgetUSD:      key.BudgetUSD,
		BudgetPeriod:   key.BudgetPeriod,
		AllowedModels:  key.AllowedModelsJSON,
		Profile:        key.Profile,
		TokenPeriod:    key.TokenPeriod,
		Priority:       key.Priority,
		MaxPricePerMil: key.MaxPricePerMillion,
		PoolAccess:     key.PoolAccess,
		RoutingGroup:   &key.RoutingGroup,
		AllowFree:      key.AllowFree == nil || *key.AllowFree,
		AllowPlan:      key.AllowPlan == nil || *key.AllowPlan,
	}
	if key.RPMLimit != nil {
		limit := int64(*key.RPMLimit)
		row.RPMLimit = &limit
	}
	if key.TokenLimit != nil {
		limit := int64(*key.TokenLimit)
		row.TokenLimit = &limit
	}
	if key.ConcurrentLimit != nil {
		limit := int64(*key.ConcurrentLimit)
		row.ConcurrentLimit = &limit
	}
	if key.PeerLinkID != "" {
		link := key.PeerLinkID
		row.PeerLinkID = &link
	}
	return row
}
