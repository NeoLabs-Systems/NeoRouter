package routing

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// --- catalog ---------------------------------------------------------------

type fakeCatalog struct {
	mu      sync.Mutex
	entries []*Entry
}

func (c *fakeCatalog) ListForUser(userID string, q CatalogQuery) []*Entry {
	capability := q.Capability
	if capability == "" {
		capability = capabilityChat
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*Entry
	for _, entry := range c.entries {
		entryCapability := entry.Capability
		if entryCapability == "" {
			entryCapability = capabilityChat
		}
		if entry.UserID != userID || entryCapability != capability {
			continue
		}
		if !q.IncludeUnavailable && !entry.Available {
			continue
		}
		copied := *entry
		out = append(out, &copied)
	}
	return out
}

func (c *fakeCatalog) CanonicalID(modelID string) string {
	return modelID
}

func (c *fakeCatalog) add(entry *Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, entry)
}

func (c *fakeCatalog) replace(entry *Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index, existing := range c.entries {
		if existing.Ref == entry.Ref {
			c.entries[index] = entry
			return
		}
	}
	c.entries = append(c.entries, entry)
}

// --- quota -----------------------------------------------------------------

type fakeBucket struct {
	minute   int64
	requests float64
	tokens   float64
}

type fakeState struct {
	scope       string
	metric      string
	limit       *float64
	remaining   *float64
	resetAt     *int64
	source      string
	observedAt  int64
	localUsed   *float64
	requestCost *float64
}

// fakeQuota is the part of the quota service routing touches: limits and
// observed windows per scope, usage buckets, and the states plan windows use.
type fakeQuota struct {
	mu      sync.Mutex
	limits  map[string]map[string]float64
	buckets map[string][]fakeBucket
	states  map[string]fakeState
}

func newFakeQuota() *fakeQuota {
	return &fakeQuota{limits: map[string]map[string]float64{}, buckets: map[string][]fakeBucket{}, states: map[string]fakeState{}}
}

func (q *fakeQuota) setLimit(scope, metric string, value float64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.limits[scope] == nil {
		q.limits[scope] = map[string]float64{}
	}
	q.limits[scope][metric] = value
}

func (q *fakeQuota) addUsage(scope string, requests, tokens float64, now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	minute := now.UnixMilli() / minuteMs
	buckets := q.buckets[scope]
	for index := range buckets {
		if buckets[index].minute == minute {
			buckets[index].requests += requests
			buckets[index].tokens += tokens
			return
		}
	}
	q.buckets[scope] = append(buckets, fakeBucket{minute: minute, requests: requests, tokens: tokens})
}

func (q *fakeQuota) recordRequest(entry *Entry, tokens int, now time.Time) {
	q.addUsage("m:"+formatRef(entry.Ref), 1, float64(tokens), now)
	q.addUsage("p:"+entry.AccountID, 1, float64(tokens), now)
}

func (q *fakeQuota) usageInLast(scopeKey string, minutes int, now time.Time) QuotaUsage {
	q.mu.Lock()
	defer q.mu.Unlock()
	current := now.UnixMilli() / minuteMs
	from := current - int64(minutes) + 1
	var out QuotaUsage
	for _, bucket := range q.buckets[scopeKey] {
		if bucket.minute >= from && bucket.minute <= current {
			out.Requests += bucket.requests
			out.Tokens += bucket.tokens
		}
	}
	return out
}

// setState mirrors quota_service.setState: for a plan window it records how
// much local usage stands behind the observation, and learns what one request
// costs from the spend since the previous observation.
func (q *fakeQuota) setState(scopeKey, metric string, window QuotaWindow, source string, now time.Time) {
	q.mu.Lock()
	key := scopeKey + "|" + metric
	previous, hadPrevious := q.states[key]
	isPlan := len(metric) > 5 && metric[:5] == "plan_"
	var localUsed *float64
	if isPlan {
		usage := q.usageInLastLocked(scopeKey, 1440, now)
		localUsed = &usage.Requests
	}
	requestCost := window.RequestCost
	if requestCost == nil && hadPrevious {
		requestCost = previous.requestCost
	}
	if isPlan && hadPrevious && previous.resetAt != nil && window.ResetAt != nil && *previous.resetAt == *window.ResetAt && window.Remaining != nil && previous.remaining != nil && localUsed != nil && previous.localUsed != nil {
		count := *localUsed - *previous.localUsed
		spent := *previous.remaining - *window.Remaining
		if count > 0 && spent > 0 {
			sample := spent / count
			if requestCost == nil {
				requestCost = &sample
			} else {
				blended := *requestCost*(1-policyPlanRateAlpha) + sample*policyPlanRateAlpha
				requestCost = &blended
			}
		}
	}
	q.states[key] = fakeState{
		scope: scopeKey, metric: metric, limit: window.Limit, remaining: window.Remaining,
		resetAt: window.ResetAt, source: source, observedAt: now.UnixMilli(), localUsed: localUsed, requestCost: requestCost,
	}
	q.mu.Unlock()
}

// usageInLastLocked is usageInLast for a caller that holds the lock.
func (q *fakeQuota) usageInLastLocked(scopeKey string, minutes int, now time.Time) QuotaUsage {
	current := now.UnixMilli() / minuteMs
	from := current - int64(minutes) + 1
	var out QuotaUsage
	for _, bucket := range q.buckets[scopeKey] {
		if bucket.minute >= from && bucket.minute <= current {
			out.Requests += bucket.requests
			out.Tokens += bucket.tokens
		}
	}
	return out
}

func (q *fakeQuota) setStateSimple(scopeKey, metric string, remaining float64, resetAt int64, now time.Time) {
	limit := 100.0
	window := QuotaWindow{Limit: &limit, Remaining: &remaining, ResetAt: &resetAt}
	q.setState(scopeKey, metric, window, "headers", now)
}

func (q *fakeQuota) accountState(accountID string) []QuotaWindow {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []QuotaWindow
	prefixes := []string{"a:" + accountID, "p:" + accountID}
	for _, state := range q.states {
		for _, prefix := range prefixes {
			if state.scope != prefix {
				continue
			}
			out = append(out, QuotaWindow{
				Scope: state.scope, Metric: state.metric, Limit: state.limit, Remaining: state.remaining,
				ResetAt: state.resetAt, Source: state.source, ObservedAt: state.observedAt,
				LocalUsed: state.localUsed, RequestCost: state.requestCost,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Metric < out[j].Metric
	})
	return out
}

func (q *fakeQuota) scopesFor(entry *Entry) []QuotaScope {
	return []QuotaScope{
		{Key: "m:" + formatRef(entry.Ref), Account: entry.AccountID, Kind: "model", Curated: map[string]float64{}},
		{Key: "p:" + entry.AccountID, Account: entry.AccountID, Kind: "pool", Curated: map[string]float64{}},
	}
}

func (q *fakeQuota) effectiveLimits(scope QuotaScope) map[string]QuotaLimit {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := map[string]QuotaLimit{}
	for metric, value := range q.limits[scope.Key] {
		out[metric] = QuotaLimit{Value: value, Source: "manual"}
	}
	return out
}

func (q *fakeQuota) liveState(scopeKey, metric string, now time.Time) *fakeState {
	state, ok := q.states[scopeKey+"|"+metric]
	if !ok {
		return nil
	}
	if state.resetAt != nil && *state.resetAt <= now.UnixMilli() {
		return nil
	}
	return &state
}

func (q *fakeQuota) dailyLimits(scope QuotaScope, now time.Time) QuotaDailyLimits {
	limits := q.effectiveLimits(scope)
	out := QuotaDailyLimits{}
	if limit, ok := limits["rpd"]; ok {
		out.RPD = limit.Value
	}
	if limit, ok := limits["tpd"]; ok {
		out.TPD = limit.Value
	}
	return out
}

func (q *fakeQuota) usageInRange(scopeKey string, startAt, endAt time.Time) QuotaUsage {
	q.mu.Lock()
	defer q.mu.Unlock()
	from := startAt.UnixMilli() / minuteMs
	to := endAt.UnixMilli() / minuteMs
	var out QuotaUsage
	for _, bucket := range q.buckets[scopeKey] {
		if bucket.minute >= from && bucket.minute <= to {
			out.Requests += bucket.requests
			out.Tokens += bucket.tokens
		}
	}
	return out
}

func (q *fakeQuota) dailyWindow(scope QuotaScope, metric string, now time.Time) *QuotaDailyWindow {
	limits := q.effectiveLimits(scope)
	limit, ok := limits[metric]
	if !ok || limit.Value <= 0 {
		// A state the provider reported also sets the window's limit.
		q.mu.Lock()
		state, has := q.states[scope.Key+"|"+metric]
		q.mu.Unlock()
		if !has || state.limit == nil || *state.limit <= 0 {
			return nil
		}
		limit = QuotaLimit{Value: *state.limit, Source: state.source}
	}
	// A reported balance with a reset time is a fixed window, as
	// quota.dailyBounds decides.
	q.mu.Lock()
	state, has := q.states[scope.Key+"|"+metric]
	q.mu.Unlock()
	if has && state.resetAt != nil && state.source != "error" && *state.resetAt-state.observedAt <= 24*hourMs && state.remaining != nil {
		renewed := *state.resetAt
		remaining := *state.remaining
		if renewed <= now.UnixMilli() {
			// A window whose reset passed is full again (quota.liveState drops
			// the stale observation).
			days := (now.UnixMilli()-renewed)/(24*hourMs) + 1
			renewed += days * 24 * hourMs
			remaining = limit.Value
		}
		used := limit.Value - remaining
		if used < 0 {
			used = 0
		}
		reset := time.UnixMilli(renewed)
		return &QuotaDailyWindow{
			Scope: scope.Key, Metric: metric, Limit: limit.Value, Used: used,
			Remaining: maxf(0, remaining), Recent: 0,
			StartAt: time.UnixMilli(renewed - 24*hourMs), ResetAt: &reset,
		}
	}
	start := now.Add(-24 * time.Hour)
	used := q.usageInRange(scope.Key, start, now)
	field := used.Requests
	if metric == "tpd" {
		field = used.Tokens
	}
	recentWindow := q.usageInLast(scope.Key, 60, now)
	recent := recentWindow.Requests
	if metric == "tpd" {
		recent = recentWindow.Tokens
	}
	remaining := limit.Value - field
	if remaining < 0 {
		remaining = 0
	}
	return &QuotaDailyWindow{
		Scope: scope.Key, Metric: metric, Limit: limit.Value, Used: limit.Value - remaining,
		Remaining: remaining, Recent: recent, Rolling: true, StartAt: start,
	}
}

func (q *fakeQuota) nextReleaseAt(scopeKey string, minutes int, now time.Time) *time.Time {
	q.mu.Lock()
	defer q.mu.Unlock()
	var soonest *time.Time
	for _, bucket := range q.buckets[scopeKey] {
		at := time.UnixMilli((bucket.minute + int64(minutes)) * minuteMs)
		if at.UnixMilli() <= now.UnixMilli() {
			continue
		}
		if soonest == nil || at.Before(*soonest) {
			value := at
			soonest = &value
		}
	}
	return soonest
}

func (q *fakeQuota) evaluate(entry *Entry, estimatedTokens int, now time.Time) QuotaEvaluation {
	out := QuotaEvaluation{Headroom: 1, Factor: 1}
	for _, scope := range q.scopesFor(entry) {
		for _, metric := range []string{"rpd", "tpd", "rpm", "tpm"} {
			window := q.dailyWindow(scope, metric, now)
			if window == nil {
				continue
			}
			headroom := window.Remaining / window.Limit
			if headroom < out.Headroom {
				out.Headroom = headroom
			}
			need := 1.0
			if metric == "tpd" || metric == "tpm" {
				need = float64(estimatedTokens)
			}
			if window.Remaining < need && out.Blocked == nil {
				out.Blocked = &QuotaBlocked{Metric: metric, Scope: scope.Key}
			}
		}
	}
	if out.Headroom < 0.2 {
		out.Factor = 0.1 + 0.9*(out.Headroom/0.2)
	}
	return out
}

func (q *fakeQuota) observeHeaders(entry *Entry, headers http.Header, now time.Time) {}

func (q *fakeQuota) learnLimit(entry *Entry, learned LearnedLimit) {
	q.setLimit("m:"+formatRef(entry.Ref), learned.Metric, learned.Value)
}

func (q *fakeQuota) markExhausted(entry *Entry, metric string, resetAt *time.Time, now time.Time) {
	zero := 0.0
	limit := 100.0
	var at *int64
	if resetAt != nil {
		value := resetAt.UnixMilli()
		at = &value
	}
	q.setState("m:"+formatRef(entry.Ref), metric, QuotaWindow{Limit: &limit, Remaining: &zero, ResetAt: at}, "error", now)
}

func (q *fakeQuota) clearObservedExhaustion(accountID string, refs []int64) {}

func (q *fakeQuota) recordPlanRequest(accountID string, now time.Time) {
	q.addUsage("a:"+accountID, 1, 0, now)
}

// Quota interface adapters (exported method names of the interface).
func (q *fakeQuota) ScopesFor(entry *Entry) []QuotaScope { return q.scopesFor(entry) }
func (q *fakeQuota) Evaluate(entry *Entry, estimatedTokens int, now time.Time) QuotaEvaluation {
	return q.evaluate(entry, estimatedTokens, now)
}
func (q *fakeQuota) UsageInLast(scopeKey string, minutes int, now time.Time) QuotaUsage {
	return q.usageInLast(scopeKey, minutes, now)
}
func (q *fakeQuota) DailyLimits(scope QuotaScope, now time.Time) QuotaDailyLimits {
	return q.dailyLimits(scope, now)
}
func (q *fakeQuota) DailyWindow(scope QuotaScope, metric string, now time.Time) *QuotaDailyWindow {
	return q.dailyWindow(scope, metric, now)
}
func (q *fakeQuota) EffectiveLimits(scope QuotaScope) map[string]QuotaLimit {
	return q.effectiveLimits(scope)
}
func (q *fakeQuota) NextReleaseAt(scopeKey string, minutes int, now time.Time) *time.Time {
	return q.nextReleaseAt(scopeKey, minutes, now)
}
func (q *fakeQuota) AccountState(accountID string) []QuotaWindow { return q.accountState(accountID) }
func (q *fakeQuota) SetState(scopeKey, metric string, window QuotaWindow, source string, now time.Time) {
	q.setState(scopeKey, metric, window, source, now)
}
func (q *fakeQuota) RecordPlanRequest(accountID string, now time.Time) {
	q.recordPlanRequest(accountID, now)
}
func (q *fakeQuota) ObserveHeaders(entry *Entry, headers http.Header, now time.Time) {
	q.observeHeaders(entry, headers, now)
}
func (q *fakeQuota) LearnLimit(entry *Entry, learned LearnedLimit) { q.learnLimit(entry, learned) }
func (q *fakeQuota) MarkExhausted(entry *Entry, metric string, resetAt *time.Time, now time.Time) {
	q.markExhausted(entry, metric, resetAt, now)
}
func (q *fakeQuota) ClearObservedExhaustion(accountID string, refs []int64) {
	q.clearObservedExhaustion(accountID, refs)
}

// --- budget ----------------------------------------------------------------

type fakeBudget struct {
	remaining  *float64
	tokenLimit bool
	pacing     *Pacing
	estimate   *float64
}

func (b *fakeBudget) EstimateMaxCost(entry *Entry, inputTokens, maxOutputTokens int) *float64 {
	return b.estimate
}

func (b *fakeBudget) Remaining(key RouterKey, now time.Time) *float64 { return b.remaining }

func (b *fakeBudget) TokenLimitReached(key RouterKey, now time.Time) bool { return b.tokenLimit }

func (b *fakeBudget) Pacing(key RouterKey, now time.Time) *Pacing { return b.pacing }

// --- settings, pools, limits, providers, keys ------------------------------

type fakeSettings struct {
	mu     sync.Mutex
	values map[string]bool
}

func newFakeSettings() *fakeSettings { return &fakeSettings{values: map[string]bool{}} }

func (s *fakeSettings) UserSettingIs(userID, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[userID+"|"+key]
}

func (s *fakeSettings) SetUserSetting(userID, key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	flag, _ := value.(bool)
	s.values[userID+"|"+key] = flag
	return nil
}

func (s *fakeSettings) set(userID, key string, value bool) {
	_ = s.SetUserSetting(userID, key, value)
}

type fakePools struct{}

func (fakePools) IsPeerEntry(entry *Entry) bool { return entry.DefinitionID == peerDefinition }
func (fakePools) ShareRejection(entry *Entry, req ShareRequest, now time.Time) string {
	if entry.DefinitionID == peerDefinition {
		return "peer_no_forward"
	}
	return ""
}

type fakeAccountLimits struct {
	mu     sync.Mutex
	limits map[string]StatedLimits
}

func newFakeAccountLimits() *fakeAccountLimits {
	return &fakeAccountLimits{limits: map[string]StatedLimits{}}
}

func (l *fakeAccountLimits) Stated(accountID string) StatedLimits {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limits[accountID]
}

func (l *fakeAccountLimits) set(accountID string, limits StatedLimits) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limits[accountID] = limits
}

type fakeProviders struct {
	mu         sync.Mutex
	concurrent map[string]int
}

func newFakeProviders() *fakeProviders {
	return &fakeProviders{concurrent: map[string]int{}}
}

func (p *fakeProviders) DocumentedConcurrency(entry *Entry) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.concurrent[entry.DefinitionID]
}

// --- fixture ---------------------------------------------------------------

type fixture struct {
	t         *testing.T
	db        *dbx.DB
	svc       *Service
	catalog   *fakeCatalog
	quota     *fakeQuota
	budget    *fakeBudget
	settings  *fakeSettings
	limits    *fakeAccountLimits
	providers *fakeProviders
	nextModel int64
	accounts  map[string]bool
}

func testConfig() config.Config {
	cfg, err := config.Load(func(string) string { return "" }, "/tmp", "/tmp")
	if err != nil {
		panic(err)
	}
	return cfg
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, t.TempDir()+"/test.sqlite3", dbx.Options{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.W.Exec(`INSERT INTO users (id, username, email, password_hash, role) VALUES
		('u', 'u', 'u@test.local', 'x', 'admin'), ('v', 'v', 'v@test.local', 'x', 'user')`); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	f := &fixture{t: t, db: db, catalog: &fakeCatalog{}, quota: newFakeQuota(), budget: &fakeBudget{}, settings: newFakeSettings(), limits: newFakeAccountLimits(), providers: newFakeProviders(), accounts: map[string]bool{}}
	cfg := testConfig()
	f.svc = New(Options{
		DB:            db,
		Cfg:           func() config.Config { return cfg },
		Catalog:       f.catalog,
		Quota:         f.quota,
		Budget:        f.budget,
		Pools:         fakePools{},
		Settings:      f.settings,
		AccountLimits: f.limits,
		Providers:     f.providers,
		Random:        func() float64 { return 0.5 },
	})
	return f
}

// account inserts a provider account.
func (f *fixture) account(id, userID, definitionID string, prepaid int, billingMode string) {
	f.t.Helper()
	_, err := f.db.W.Exec(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode, enabled, status, prepaid)
		VALUES (?, ?, ?, ?, ?, ?, 1, 'healthy', ?)`, id, userID, definitionID, id, id, billingMode, prepaid)
	if err != nil {
		f.t.Fatalf("insert account %s: %v", id, err)
	}
	if f.accounts != nil {
		f.accounts[id] = true
	}
}

// entry inserts a model row and returns the routing entry for it. The account
// is created on first use (a Groq free-tier connection, like the Node
// fixture).
func (f *fixture) entry(modelID, accountID string, overrides ...func(*Entry)) *Entry {
	f.t.Helper()
	if !f.accounts[accountID] {
		f.accounts[accountID] = true
		f.account(accountID, "u", "groq", 0, "free_tier")
	}
	f.nextModel++
	canonical := modelID
	result, err := f.db.W.Exec(`INSERT INTO models (account_id, model_id, canonical_id, capability) VALUES (?, ?, ?, 'chat')`, accountID, modelID, canonical)
	if err != nil {
		f.t.Fatalf("insert model: %v", err)
	}
	ref, err := result.LastInsertId()
	if err != nil {
		f.t.Fatalf("model id: %v", err)
	}
	entry := &Entry{
		Ref:            ref,
		ID:             accountID + "/" + modelID,
		ModelID:        modelID,
		Name:           modelID,
		CanonicalID:    canonical,
		Capability:     capabilityChat,
		AccountID:      accountID,
		UserID:         "u",
		ProviderSlug:   accountID,
		ProviderKey:    accountID,
		ProviderName:   accountID,
		DefinitionID:   "groq",
		BillingMode:    "free_tier",
		OverflowPolicy: "auto",
		Enabled:        true,
		Available:      true,
		AccountEnabled: true,
		AccountStatus:  "healthy",
		PriceIn:        floatPtr(1),
		PriceOut:       floatPtr(2),
		Intelligence:   floatPtr(0.5),
	}
	for _, override := range overrides {
		override(entry)
	}
	f.catalog.add(entry)
	return entry
}

// sqlEntry inserts a model row for an account, for tests that only need the row.
func (f *fixture) modelRow(modelID, accountID string) int64 {
	f.t.Helper()
	result, err := f.db.W.Exec(`INSERT INTO models (account_id, model_id, canonical_id) VALUES (?, ?, ?)`, accountID, modelID, modelID)
	if err != nil {
		f.t.Fatalf("insert model: %v", err)
	}
	ref, _ := result.LastInsertId()
	return ref
}

// key is a router key as a request carries it.
func testKey(profile string) RouterKey {
	return RouterKey{ID: "k", UserID: "u", Name: "k", Mode: "both", BudgetPeriod: "total", AllowedModelsJSON: "[]", Profile: profile}
}

func planRequest(content string) RequestInfo {
	return RequestInfo{Messages: []task.Message{{Role: "user", Content: content}}}
}

func mustPlan(t *testing.T, svc *Service, opts PlanOptions) *RoutingPlan {
	t.Helper()
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	plan, err := svc.NewPlan(opts)
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	return plan
}

// testNow is a realistic clock: the task-experience store stamps new rows with
// the wall clock (as the Node code does), so tests must not sit in 1970.
func testNow() time.Time { return time.UnixMilli(1_760_000_000_000) }

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
}
