package quota

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

// testDB opens a migrated database in a temp directory.
func testDB(t *testing.T) *dbx.DB {
	t.Helper()
	db, err := dbx.Open(context.Background(), filepath.Join(t.TempDir(), "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(context.Background(), db.W); err != nil {
		t.Fatalf("schema.Ensure: %v", err)
	}
	return db
}

// fixedClock is a clock the test moves by hand.
type fixedClock struct{ at time.Time }

func (c *fixedClock) Now() time.Time          { return c.at }
func (c *fixedClock) Advance(d time.Duration) { c.at = c.at.Add(d) }

func newClock() *fixedClock {
	return &fixedClock{at: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}
}

func testService(t *testing.T) *Service {
	t.Helper()
	service := New(testDB(t), nil)
	service.SetClock(newClock().Now)
	return service
}

func TestUsageOfIsASlidingWindow(t *testing.T) {
	service := testService(t)
	clock := newClock()
	service.SetClock(clock.Now)
	scope := ModelScopeKey(7)

	if err := service.RecordRequest(Entry{Ref: 7, AccountID: "acct"}, 100, clock.Now()); err != nil {
		t.Fatalf("RecordRequest: %v", err)
	}
	clock.Advance(30 * time.Second)
	usage, err := service.UsageOf(scope, clock.Now())
	if err != nil {
		t.Fatalf("UsageOf: %v", err)
	}
	if usage.RPM != 1 || usage.TPM != 100 {
		t.Fatalf("same minute: %+v", usage)
	}
	// The previous minute still counts in full at the start of the next one.
	clock.Advance(30 * time.Second)
	usage, _ = service.UsageOf(scope, clock.Now())
	if usage.RPM != 1 || usage.TPM != 100 {
		t.Fatalf("start of the next minute: %+v", usage)
	}
	// Halfway through it only half of it does.
	clock.Advance(30 * time.Second)
	usage, _ = service.UsageOf(scope, clock.Now())
	if usage.RPM != 0.5 || usage.TPM != 50 {
		t.Fatalf("halfway through: %+v", usage)
	}
	if usage.RPD != 1 || usage.TPD != 100 {
		t.Fatalf("the daily window keeps the whole minute: %+v", usage)
	}
	// Two minutes on, the minute is out of the sliding window but still in the day.
	clock.Advance(30 * time.Second)
	usage, _ = service.UsageOf(scope, clock.Now())
	if usage.RPM != 0 || usage.TPM != 0 || usage.RPD != 1 {
		t.Fatalf("out of the minute window: %+v", usage)
	}
}

func TestUsageWindowsAndNextRelease(t *testing.T) {
	service := testService(t)
	clock := newClock()
	service.SetClock(clock.Now)
	entry := Entry{Ref: 7, AccountID: "acct"}

	for i := 0; i < 3; i++ {
		if err := service.RecordRequest(entry, 10, clock.Now()); err != nil {
			t.Fatalf("RecordRequest: %v", err)
		}
	}
	last, err := service.UsageInLast(ModelScopeKey(7), 60, clock.Now())
	if err != nil {
		t.Fatalf("UsageInLast: %v", err)
	}
	if last.Requests != 3 || last.Tokens != 30 {
		t.Fatalf("UsageInLast = %+v", last)
	}
	clock.Advance(2 * time.Minute)
	last, _ = service.UsageInLast(ModelScopeKey(7), 60, clock.Now())
	if last.Requests != 3 {
		t.Fatalf("an hour-wide window still holds the minute: %+v", last)
	}
	release, err := service.NextReleaseAt(ModelScopeKey(7), 60, clock.Now())
	if err != nil {
		t.Fatalf("NextReleaseAt: %v", err)
	}
	if release == nil || *release != newClock().at.Add(time.Hour).UnixMilli() {
		t.Fatalf("NextReleaseAt = %v", release)
	}
	// A refused request is taken back out.
	if err := service.RefundRequest(entry, 10, clock.Now()); err != nil {
		t.Fatalf("RefundRequest: %v", err)
	}
	after, _ := service.UsageInLast(PoolScopeKey("acct"), 60, clock.Now())
	if after.Requests != 2 {
		t.Fatalf("refunded pool usage = %+v", after)
	}
}

type definitions map[string]*Definition

func (d definitions) QuotaDefinition(id string) *Definition { return d[id] }

func TestEvaluateBlocksAndPricesHeadroom(t *testing.T) {
	service := testService(t)
	service.defs = definitions{"host": {Limits: Limits{Pool: "account", Account: map[string]float64{RPM: 10}}}}
	entry := Entry{Ref: 1, AccountID: "acct", DefinitionID: "host", ModelID: "one"}
	clock := newClock()
	service.SetClock(clock.Now)

	scopes := service.ScopesFor(entry)
	if len(scopes) != 2 || scopes[0].Key != "m:1" || scopes[1].Key != "p:acct" || scopes[1].Kind != KindPool {
		t.Fatalf("scopes = %+v", scopes)
	}
	for i := 0; i < 10; i++ {
		if err := service.RecordRequest(entry, 0, clock.Now()); err != nil {
			t.Fatalf("RecordRequest: %v", err)
		}
	}
	evaluation, err := service.Evaluate(entry, 0, clock.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if evaluation.Blocked == nil || evaluation.Blocked.Metric != RPM || evaluation.Blocked.Scope != "p:acct" {
		t.Fatalf("blocked = %+v", evaluation.Blocked)
	}
	if evaluation.Headroom != 0 || evaluation.Factor != 0.1 {
		t.Fatalf("headroom %v factor %v", evaluation.Headroom, evaluation.Factor)
	}
	if len(evaluation.Details) != 1 {
		t.Fatalf("details = %+v", evaluation.Details)
	}
	detail := evaluation.Details[0]
	if detail.Limit == nil || *detail.Limit != 10 || detail.Used != 10 || detail.Remaining != 0 || detail.Source != SourceCurated {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestStatedAccountLimitOutranksTheDefinition(t *testing.T) {
	service := testService(t)
	service.defs = definitions{"host": {Limits: Limits{Pool: "account", Account: map[string]float64{RPM: 10}}}}
	entry := Entry{Ref: 1, AccountID: "acct", DefinitionID: "host", ModelID: "one"}
	insertAccount(t, service, "u", "acct", "host")
	twenty := 20.0
	if _, err := service.AccountLimits().Set("acct", map[string]*float64{RPM: &twenty}, time.Now()); err != nil {
		t.Fatalf("AccountLimits.Set: %v", err)
	}
	scopes := service.ScopesFor(entry)
	limits := service.effectiveLimits(scopes[1])
	if limits[RPM].Value != 20 || limits[RPM].Source != SourceManual {
		t.Fatalf("pool limits = %+v", limits)
	}
	// A stated limit on an account also creates the pool scope on its own.
	bare := Entry{Ref: 2, AccountID: "acct", DefinitionID: "host", ModelID: "two"}
	if scopes := service.ScopesFor(bare); len(scopes) != 2 {
		t.Fatalf("a stated limit makes the pool scope: %+v", scopes)
	}
}

func TestObservedExhaustionAndRenewal(t *testing.T) {
	service := testService(t)
	clock := newClock()
	service.SetClock(clock.Now)
	service.defs = definitions{"host": {Limits: Limits{Pool: "account"}}}
	entry := Entry{Ref: 1, AccountID: "acct", DefinitionID: "host", ModelID: "one"}
	resetAt := clock.Now().Add(time.Hour).UnixMilli()
	if err := service.MarkExhausted(entry, RPM, &resetAt, clock.Now()); err != nil {
		t.Fatalf("MarkExhausted: %v", err)
	}
	evaluation, err := service.Evaluate(entry, 0, clock.Now())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if evaluation.Blocked == nil || evaluation.Blocked.Metric != RPM || evaluation.Blocked.ResetAt == nil || *evaluation.Blocked.ResetAt != resetAt {
		t.Fatalf("blocked = %+v", evaluation.Blocked)
	}
	states, err := service.AccountState("acct")
	if err != nil {
		t.Fatalf("AccountState: %v", err)
	}
	if len(states) != 1 || states[0].Source != SourceError || states[0].Remaining == nil || *states[0].Remaining != 0 {
		t.Fatalf("state = %+v", states)
	}
	// Observed exhaustion is what a raised limit clears.
	if err := service.ClearObservedExhaustion("acct", nil); err != nil {
		t.Fatalf("ClearObservedExhaustion: %v", err)
	}
	if states, _ := service.AccountState("acct"); len(states) != 0 {
		t.Fatalf("state after clearing = %+v", states)
	}
}

func TestAccountStateShowsARenewedWindow(t *testing.T) {
	service := testService(t)
	clock := newClock()
	service.SetClock(clock.Now)
	limit, remaining := 100.0, 0.0
	resetAt := clock.Now().Add(time.Hour).UnixMilli()
	if err := service.SetState(PoolScopeKey("acct"), RPD, Observation{Limit: &limit, Remaining: &remaining, ResetAt: &resetAt}, SourceHeader, clock.Now()); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	states, err := service.AccountState("acct")
	if err != nil {
		t.Fatalf("AccountState: %v", err)
	}
	if len(states) != 1 || states[0].Limit == nil || *states[0].Limit != 100 || states[0].Renewed {
		t.Fatalf("state = %+v", states)
	}
	clock.Advance(2 * time.Hour)
	renewed, err := service.AccountStateNow("acct", clock.Now())
	if err != nil {
		t.Fatalf("AccountStateNow: %v", err)
	}
	if len(renewed) != 1 || !renewed[0].Renewed || renewed[0].ResetAt != nil || renewed[0].Remaining == nil || *renewed[0].Remaining != 100 {
		t.Fatalf("renewed = %+v", renewed)
	}
}

func TestDailyWindowAnchorsToTheResetTime(t *testing.T) {
	service := testService(t)
	clock := newClock()
	service.SetClock(clock.Now)
	scope := Scope{Key: PoolScopeKey("acct"), Kind: KindPool, Curated: map[string]float64{RPD: 100}}
	limit, remaining := 100.0, 100.0
	resetAt := clock.Now().Add(6 * time.Hour).UnixMilli()
	if err := service.SetState(scope.Key, RPD, Observation{Limit: &limit, Remaining: &remaining, ResetAt: &resetAt}, SourceHeader, clock.Now()); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	for i := 0; i < 4; i++ {
		if err := service.AddUsage(scope.Key, 1, 0, clock.Now()); err != nil {
			t.Fatalf("AddUsage: %v", err)
		}
	}
	window, err := service.DailyWindow(scope, RPD, clock.Now())
	if err != nil {
		t.Fatalf("DailyWindow: %v", err)
	}
	if window == nil {
		t.Fatal("no daily window")
	}
	if window.Rolling || window.ResetAt == nil || *window.ResetAt != resetAt {
		t.Fatalf("window = %+v", window)
	}
	if window.Limit != 100 || window.Used != 4 || window.Remaining != 96 || window.Source != SourceCurated {
		t.Fatalf("window = %+v", window)
	}
	// Without a reset time the window rolls and knows what is about to expire.
	rolling := Scope{Key: ModelScopeKey(8), Kind: KindModel, Curated: map[string]float64{TPD: 1000}}
	if err := service.SetLimit(rolling.Key, TPD, 1000, SourceHeader); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}
	window, err = service.DailyWindow(rolling, TPD, clock.Now())
	if err != nil {
		t.Fatalf("DailyWindow: %v", err)
	}
	if window == nil || !window.Rolling || window.ResetAt != nil || window.Source != SourceHeader {
		t.Fatalf("rolling window = %+v", window)
	}
}

func TestLearnedLimitOnlyTightensAndOutranksCurated(t *testing.T) {
	service := testService(t)
	clock := newClock()
	service.SetClock(clock.Now)
	entry := Entry{Ref: 1, AccountID: "acct", DefinitionID: "host", ModelID: "one"}
	service.defs = definitions{"host": {Limits: Limits{Defaults: map[string]float64{RPM: 60}}}}

	if err := service.ObserveHeaders(entry, map[string][]string{
		"X-RateLimit-Limit-Requests":     {"12"},
		"X-RateLimit-Remaining-Requests": {"3"},
	}, clock.Now()); err != nil {
		t.Fatalf("ObserveHeaders: %v", err)
	}
	scopes := service.ScopesFor(entry)
	limits := service.effectiveLimits(scopes[0])
	if limits[RPM].Value != 12 || limits[RPM].Source != SourceHeader {
		t.Fatalf("header limit = %+v", limits)
	}
	// A learned limit that is looser than the header is ignored.
	if err := service.LearnLimit(entry, Observation{Metric: RPM, Limit: floatPtr(100)}); err != nil {
		t.Fatalf("LearnLimit: %v", err)
	}
	if limits := service.effectiveLimits(scopes[0]); limits[RPM].Value != 12 || limits[RPM].Source != SourceHeader {
		t.Fatalf("looser learned limit won: %+v", limits)
	}
	// A tighter one is kept.
	if err := service.LearnLimit(entry, Observation{Metric: RPM, Limit: floatPtr(5)}); err != nil {
		t.Fatalf("LearnLimit: %v", err)
	}
}

func TestUsageAndLimitsSurviveARestart(t *testing.T) {
	db := testDB(t)
	service := New(db, nil)
	clock := newClock()
	service.SetClock(clock.Now)
	entry := Entry{Ref: 7, AccountID: "acct"}
	if err := service.RecordRequest(entry, 25, clock.Now()); err != nil {
		t.Fatalf("RecordRequest: %v", err)
	}
	if err := service.SetLimit(ModelScopeKey(7), RPM, 42, SourceManual); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}
	insertAccount(t, service, "u", "acct", "a")
	limit, remaining := 42.0, 7.0
	if err := service.SetState(PoolScopeKey("acct"), RPM, Observation{Limit: &limit, Remaining: &remaining}, SourceHeader, clock.Now()); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := service.Flush(true); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	reopened := New(db, nil)
	reopened.SetClock(clock.Now)
	usage, err := reopened.UsageOf(ModelScopeKey(7), clock.Now())
	if err != nil {
		t.Fatalf("UsageOf: %v", err)
	}
	if usage.RPM != 1 || usage.TPM != 25 {
		t.Fatalf("usage after reload = %+v", usage)
	}
	limits := reopened.effectiveLimits(Scope{Key: ModelScopeKey(7), Kind: KindModel})
	if limits[RPM].Value != 42 || limits[RPM].Source != SourceManual {
		t.Fatalf("limits after reload = %+v", limits)
	}
	states, err := reopened.AccountState("acct")
	if err != nil {
		t.Fatalf("AccountState: %v", err)
	}
	if len(states) != 1 || states[0].Scope != "p:acct" || states[0].Limit == nil || *states[0].Limit != 42 {
		t.Fatalf("states = %+v", states)
	}
}

func TestRefundedAndZeroRequestsKeepBucketsHonest(t *testing.T) {
	service := testService(t)
	clock := newClock()
	service.SetClock(clock.Now)
	entry := Entry{Ref: 7, AccountID: "acct"}
	if err := service.RecordRequest(entry, 100, clock.Now()); err != nil {
		t.Fatalf("RecordRequest: %v", err)
	}
	if err := service.RefundRequest(entry, 0, clock.Now()); err != nil {
		t.Fatalf("RefundRequest: %v", err)
	}
	usage, err := service.UsageOf(ModelScopeKey(7), clock.Now())
	if err != nil {
		t.Fatalf("UsageOf: %v", err)
	}
	if usage.RPM != 0 || usage.TPM != 100 {
		t.Fatalf("usage = %+v", usage)
	}
	if err := service.RecordTokens(entry, 0, clock.Now()); err != nil {
		t.Fatalf("RecordTokens: %v", err)
	}
}
