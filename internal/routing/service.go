// Package routing is NeoRouter's learning router: it ranks the user's catalog
// models for one request, explains why a model was left out, and keeps what it
// learns about models and connections.
//
// Nothing here is a global. A Service owns the in-memory state (stats cache,
// in-flight counts, learned concurrency, account health, priority queues,
// pacing caches, cache affinity, live events), so tests and two Services in one
// process do not share it.
package routing

import (
	"log/slog"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// Catalog is what routing reads from the model catalog.
type Catalog interface {
	// ListForUser returns the user's models for one capability, in the order
	// the catalog lists them (account slug, model id).
	ListForUser(userID string, q CatalogQuery) []*Entry
	// CanonicalID normalises a model id to the identity shared across providers
	// (catalog/model_identity.canonicalId).
	CanonicalID(modelID string) string
}

// CatalogQuery is listForUser's options.
type CatalogQuery struct {
	Capability         string // "chat" when empty
	IncludeUnavailable bool
}

// Quota is the quota service as routing uses it: what is left, what was
// observed, and the bookkeeping routing itself triggers from failures and plan
// sends.
type Quota interface {
	ScopesFor(entry *Entry) []QuotaScope
	Evaluate(entry *Entry, estimatedTokens int, now time.Time) QuotaEvaluation
	UsageInLast(scopeKey string, minutes int, now time.Time) QuotaUsage
	DailyLimits(scope QuotaScope, now time.Time) QuotaDailyLimits
	DailyWindow(scope QuotaScope, metric string, now time.Time) *QuotaDailyWindow
	EffectiveLimits(scope QuotaScope) map[string]QuotaLimit
	NextReleaseAt(scopeKey string, minutes int, now time.Time) *time.Time
	AccountState(accountID string) []QuotaWindow
	SetState(scopeKey, metric string, state QuotaWindow, source string, now time.Time)
	RecordPlanRequest(accountID string, now time.Time)
	ObserveHeaders(entry *Entry, headers http.Header, now time.Time)
	LearnLimit(entry *Entry, learned LearnedLimit)
	MarkExhausted(entry *Entry, metric string, resetAt *time.Time, now time.Time)
	ClearObservedExhaustion(accountID string, refs []int64)
}

// QuotaScope is one usage scope a model counts against: its own (kind "model",
// key "m:<ref>") or its account's (kind "pool", key "p:<account>").
type QuotaScope struct {
	Key     string
	Account string
	Kind    string // "model" | "pool"
	Curated map[string]float64
}

// QuotaLimit is one effective limit with where it came from.
type QuotaLimit struct {
	Value  float64
	Source string
}

// QuotaDailyLimits is a scope's known daily limits (0 = unknown).
type QuotaDailyLimits struct {
	RPD float64
	TPD float64
}

// QuotaUsage is requests and tokens.
type QuotaUsage struct {
	Requests float64
	Tokens   float64
}

// QuotaDailyWindow is the window a daily metric is currently counted in.
type QuotaDailyWindow struct {
	Scope     string
	Metric    string
	Limit     float64
	Used      float64
	Remaining float64
	Recent    float64
	Expiring  float64
	Rolling   bool
	StartAt   time.Time
	ResetAt   *time.Time
}

// QuotaWindow is one observed window of an account or model (plan windows).
type QuotaWindow struct {
	Scope       string
	Metric      string
	Limit       *float64
	Remaining   *float64
	ResetAt     *int64 // Unix ms
	Source      string
	ObservedAt  int64 // Unix ms
	LocalUsed   *float64
	RequestCost *float64
}

// QuotaEvaluation is quota.evaluate's result: how much headroom a model has,
// the scoring factor that follows from it, and the first exhausted metric.
type QuotaEvaluation struct {
	Blocked  *QuotaBlocked
	Headroom float64
	Factor   float64
	Details  []QuotaDetail
}

// QuotaBlocked names the metric that is used up.
type QuotaBlocked struct {
	Metric  string
	Scope   string
	ResetAt *time.Time
}

// QuotaDetail is one metric's state, for the UI and tests.
type QuotaDetail struct {
	Scope     string
	Kind      string
	Metric    string
	Limit     *float64
	Used      float64
	Remaining float64
	Source    string
	ResetAt   *time.Time
}

// Budget is the budget service as routing uses it: what a request may cost and
// how fast the key is spending.
type Budget interface {
	// EstimateMaxCost is the worst-case price of one request, nil when a price
	// is unknown.
	EstimateMaxCost(entry *Entry, inputTokens, maxOutputTokens int) *float64
	// Remaining is what the key may still spend in its period, nil without a
	// budget.
	Remaining(key RouterKey, now time.Time) *float64
	// TokenLimitReached reports the key's token allowance being used up.
	TokenLimitReached(key RouterKey, now time.Time) bool
	// Pacing is budget pacing for a key, nil without a budget.
	Pacing(key RouterKey, now time.Time) *Pacing
}

// Pacing is how far ahead of its budget a key is.
type Pacing struct {
	State         string // none | ahead | on_track | tight | over
	CostShift     float64
	PerRequestCap float64
	Spent         float64
	Remaining     float64
	Forecast      *float64
	ResetsAt      *time.Time
}

// Billing is the provider billing check: what routing learns about an account's
// billing state from traffic.
type Billing interface {
	ObserveHeaders(entry *Entry, headers http.Header)
	ObserveFailure(entry *Entry, failure Failure)
}

// Pools is the pool share policy as routing uses it.
type Pools interface {
	IsPeerEntry(entry *Entry) bool
	// ShareRejection is share_policy.rejection: "" when the partner may use the
	// model, otherwise the rejection reason.
	ShareRejection(entry *Entry, req ShareRequest, now time.Time) string
}

// ShareRequest is what a partner's request asks of a shared model.
type ShareRequest struct {
	InputTokens int
	Reserve     *float64
	PoolID      string
}

// Settings is the user settings store.
type Settings interface {
	// UserSettingIs reports whether a setting is exactly true.
	UserSettingIs(userID, key string) bool
	SetUserSetting(userID, key string, value any) error
}

// AccountLimits is the limits a person stated for a connection.
type AccountLimits interface {
	Stated(accountID string) StatedLimits
}

// StatedLimits are the metric values a person stated (0 = not stated).
type StatedLimits struct {
	RPM        int
	RPD        int
	TPM        int
	TPD        int
	Concurrent int
}

// ProviderDefinitions is the provider registry as routing uses it: the
// concurrency a provider documents for an account.
type ProviderDefinitions interface {
	// DocumentedConcurrency is a provider definition's stated concurrency for
	// this entry (limits.account.concurrent, or the selected flat-rate slot
	// plan's concurrent); 0 when unknown.
	DocumentedConcurrency(entry *Entry) int
}

// RouterKeys is the router key service as pacing needs it
// (router_key_service.rpmState + key_concurrency.inFlight).
type RouterKeys interface {
	// RPMState is the key's per-minute limit and how much of it is left, nil
	// without a per-minute limit.
	RPMState(key RouterKey, now time.Time) *RPMState
	// InFlight counts the key's requests in flight.
	InFlight(key RouterKey) int
}

// RPMState is a key's per-minute allowance.
type RPMState struct {
	Limit        int
	Remaining    int
	ResetSeconds int
}

// AccountCard is one connection as the live view draws it.
type AccountCard struct {
	ID   string
	Key  string // provider key (provider/account_service providerKey)
	Name string // provider name
	Slug string // the account's slug
}

// Options builds a Service. DB, Cfg and Catalog are required; every other
// consumer is optional and its absence disables the part that needs it (tests
// supply fakes).
type Options struct {
	DB      *dbx.DB
	Cfg     func() config.Config
	Log     *slog.Logger
	Catalog Catalog

	Quota         Quota
	Budget        Budget
	Billing       Billing
	Pools         Pools
	Settings      Settings
	AccountLimits AccountLimits
	Providers     ProviderDefinitions
	Keys          RouterKeys

	// Random is math/rand's source for the softmax draw and Thompson sampling.
	Random func() float64

	// PublicUser turns a user id into the public user object a profile shows as
	// its creator (teams/members.publicUser). Optional.
	PublicUser func(userID string) any

	// Accounts lists the user's enabled connections for the live view's cards
	// (the accounts, reduced to what a card shows). Optional: without it the
	// provider_accounts table is read directly.
	Accounts func(userID string) []AccountCard
}

// Service is the routing domain.
type Service struct {
	opts Options

	stats      *statsStore
	load       *loadTracker
	health     *accountHealth
	conc       *concurrencyTracker
	affinity   *affinityStore
	waiting    *priorityQueue
	capacity   *capacityTracker
	smoother   *smoother
	slots      *slotUsage
	live       *liveFeed
	retries    *task.RetryTracker
	experience *task.Experience
	planSent   *planSentStore
	covers     *coverCache

	longShotsMu sync.Mutex
	longShots   map[int64]int64 // model ref -> last long-shot attempt (Unix ms)
}

// New builds a Service.
func New(opts Options) *Service {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.Random == nil {
		opts.Random = rand.Float64
	}
	s := &Service{opts: opts}
	s.stats = newStatsStore(opts.DB, opts.Log)
	s.load = newLoadTracker()
	s.health = newAccountHealth()
	s.conc = newConcurrencyTracker(opts.AccountLimits, opts.Providers)
	s.affinity = newAffinityStore()
	s.waiting = newPriorityQueue()
	s.capacity = newCapacityTracker()
	s.smoother = newSmoother(s)
	s.slots = newSlotUsage(s)
	s.live = newLiveFeed()
	s.retries = task.NewRetryTracker()
	s.experience = task.NewExperience(opts.DB)
	s.planSent = newPlanSentStore()
	s.covers = &coverCache{items: map[string]cacheEntry[bool]{}}
	s.longShots = make(map[int64]int64)
	return s
}

// DB is the shared database handle.
func (s *Service) DB() *dbx.DB { return s.opts.DB }

// cfg is the current configuration (read per decision so a reloaded config is
// seen).
func (s *Service) cfg() config.Config {
	if s.opts.Cfg == nil {
		return config.Config{}
	}
	return s.opts.Cfg()
}

func (s *Service) routingCfg() config.Routing { return s.cfg().Routing }

func (s *Service) log() *slog.Logger {
	if s.opts.Log == nil {
		return slog.Default()
	}
	return s.opts.Log
}

// catalogList is catalog.listForUser with the default capability.
func (s *Service) catalogList(userID, capability string) []*Entry {
	if s.opts.Catalog == nil {
		return nil
	}
	if capability == "" {
		capability = capabilityChat
	}
	return s.opts.Catalog.ListForUser(userID, CatalogQuery{Capability: capability})
}

// canonicalID is catalog/model_identity.canonicalId.
func (s *Service) canonicalID(modelID string) string {
	if s.opts.Catalog == nil {
		return modelID
	}
	return s.opts.Catalog.CanonicalID(modelID)
}
