package live

import (
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/budget"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/quota"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
	"github.com/NeoLabs-Systems/neorouter/internal/usage"
)

// The consumers below are the narrow surfaces this package reads from other
// domains; internal/app satisfies them with the real services.

// Routing ranks the catalog for one session and records what it learned.
type Routing interface {
	NewPlan(opts routing.PlanOptions) (*routing.RoutingPlan, error)
	// Lease marks one attempt in flight on a model and its account; the returned
	// function releases it.
	Lease(entry *routing.Entry) func()
	// OnSuccess records what a provider that accepted the session measured.
	OnSuccess(entry *routing.Entry, measurement routing.Measurement, headers http.Header, now time.Time)
	// OnFailure records what a refused attempt says about a model and returns the
	// policy for the caller (whether another model makes sense).
	OnFailure(entry *routing.Entry, failure routing.Failure, opts routing.FailureOptions) routing.Policy
}

// RouterKeys authenticates the router key a session carries and counts its
// per-minute allowance.
type RouterKeys interface {
	Authenticate(token string) (*routerkeys.Row, error)
	ConsumeRPM(key *routerkeys.Row, now time.Time) routerkeys.RPMResult
}

// KeySlots counts a key's sessions in flight against its limit;
// *routerkeys.Concurrency satisfies it.
type KeySlots interface {
	Acquire(key *routerkeys.Row) (func(), error)
}

// Accounts resolves a catalog entry's provider connection: getRuntime plus the
// definition the account was created from (and adapterFor). ok is false when
// the account is gone.
type Accounts interface {
	Runtime(accountID string) (Connection, bool)
}

// Quota counts what a session used against a connection's allowance;
// *quota.Service satisfies it.
type Quota interface {
	RecordRequest(entry quota.Entry, tokens int64, now time.Time) error
	RecordTokens(entry quota.Entry, tokens int64, now time.Time) error
}

// Budget reads what a key may still spend and books the session's cost;
// *budget.Service satisfies it.
type Budget interface {
	Remaining(key budget.Key, now time.Time) (*float64, error)
	Reserve(key budget.Key, amount float64) *budget.Reservation
}

// Usage writes the request log row; *usage.Service satisfies it.
type Usage interface {
	Record(entry usage.Entry) error
}

// routerKeyOf is the router key as routing reads it: the row's columns under
// routing's names.
func routerKeyOf(row *routerkeys.Row) routing.RouterKey {
	key := routing.RouterKey{
		ID:                 row.ID,
		UserID:             row.UserID,
		Name:               row.Name,
		Mode:               row.Mode,
		BudgetUSD:          row.BudgetUSD,
		BudgetPeriod:       row.BudgetPeriod,
		RPMLimit:           intPtrOf(row.RPMLimit),
		AllowedModelsJSON:  row.AllowedModels,
		Profile:            row.Profile,
		TokenPeriod:        row.TokenPeriod,
		Priority:           row.Priority,
		MaxPricePerMillion: row.MaxPricePerMil,
		PoolAccess:         row.PoolAccess,
		ConcurrentLimit:    intPtrOf(row.ConcurrentLimit),
		// The database flags are never null; nil would mean "allowed".
		AllowFree: boolPtrOf(row.AllowFree),
		AllowPlan: boolPtrOf(row.AllowPlan),
	}
	if row.TokenLimit != nil {
		limit := float64(*row.TokenLimit)
		key.TokenLimit = &limit
	}
	if row.RoutingGroup != nil {
		key.RoutingGroup = *row.RoutingGroup
	}
	if row.PeerLinkID != nil {
		key.PeerLinkID = *row.PeerLinkID
	}
	return key
}

// budgetKeyOf is the same key as the budget ledger names it.
func budgetKeyOf(row *routerkeys.Row) budget.Key {
	return budget.Key{
		ID:           row.ID,
		BudgetUSD:    row.BudgetUSD,
		BudgetPeriod: row.BudgetPeriod,
		TokenLimit:   row.TokenLimit,
		TokenPeriod:  row.TokenPeriod,
	}
}

// quotaEntryOf is a routing entry as the quota service names it.
func quotaEntryOf(entry *routing.Entry) quota.Entry {
	return quota.Entry{
		Ref:            entry.Ref,
		ID:             entry.ID,
		Name:           entry.Name,
		AccountID:      entry.AccountID,
		DefinitionID:   entry.DefinitionID,
		ModelID:        entry.ModelID,
		IsFree:         entry.IsFree,
		IsPlan:         entry.IsPlan,
		IsFlatRate:     entry.IsFlatRate,
		Enabled:        entry.Enabled,
		AccountEnabled: entry.AccountEnabled,
		Available:      entry.Available,
	}
}

// priceOf is the price picture the budget maths reads off a catalog entry.
func priceOf(entry *routing.Entry) budget.Price {
	return budget.Price{
		IsFree:          entry.IsFree,
		IsPlan:          entry.IsPlan,
		PriceIn:         entry.PriceIn,
		PriceOut:        entry.PriceOut,
		PriceCachedIn:   entry.PriceCachedIn,
		PriceCacheWrite: entry.PriceCacheWrite,
	}
}

// routingFailure is a classified provider failure as routing reads it.
func routingFailure(failure *providers.ProviderError) routing.Failure {
	out := routing.Failure{
		Kind:        routing.Kind(failure.Kind),
		Message:     failure.Message,
		Status:      failure.Status,
		NeedsAction: failure.NeedsAction,
		Headers:     failure.Headers,
	}
	if failure.RetryAfterKnown {
		wait := failure.RetryAfter
		out.RetryAfter = &wait
	}
	if failure.Learned != nil {
		out.Learned = &routing.LearnedLimit{Metric: failure.Learned.Metric, Value: failure.Learned.Value}
	}
	return out
}

// providerErrorOf keeps a classified failure, and classifies anything else as
// the transport failure it is (fromNetworkError).
func providerErrorOf(err error) *providers.ProviderError {
	if classified := providers.AsProviderError(err); classified != nil {
		return classified
	}
	return providers.FromNetwork(err)
}

// recordRequest counts one request against the chosen connection's allowance
// (quota.recordRequest(entry, 0)).
func (s *Service) recordRequest(entry *routing.Entry, now time.Time) {
	if s.opts.Quota == nil || entry == nil {
		return
	}
	if err := s.opts.Quota.RecordRequest(quotaEntryOf(entry), 0, now); err != nil {
		s.log().Warn("Live session: record request against quota", "model", entry.ID, "error", err)
	}
}

// recordTokens counts the tokens a provider reported (quota.recordTokens).
func (s *Service) recordTokens(entry *routing.Entry, tokens int64, now time.Time) {
	if s.opts.Quota == nil || entry == nil || tokens == 0 {
		return
	}
	if err := s.opts.Quota.RecordTokens(quotaEntryOf(entry), tokens, now); err != nil {
		s.log().Warn("Live session: record tokens against quota", "model", entry.ID, "error", err)
	}
}

// remaining is what the key may still spend (budget.remaining), nil without a
// budget.
func (s *Service) remaining(row *routerkeys.Row, now time.Time) *float64 {
	if s.opts.Budget == nil {
		return nil
	}
	remaining, err := s.opts.Budget.Remaining(budgetKeyOf(row), now)
	if err != nil {
		s.log().Warn("Live session: read budget", "error", err)
		return nil
	}
	return remaining
}

// settle books the session's cost and releases the reservation
// (budget.reserve(key, 0).settle(cost, id)).
func (s *Service) settle(row *routerkeys.Row, cost float64, requestID string) {
	if s.opts.Budget == nil {
		return
	}
	if err := s.opts.Budget.Reserve(budgetKeyOf(row), 0).Settle(cost, requestID); err != nil {
		s.log().Warn("Live session: book cost", "error", err)
	}
}

func intPtrOf(value *int64) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func boolPtrOf(value bool) *bool { return &value }

func floatPtrOf(value float64) *float64 { return &value }
