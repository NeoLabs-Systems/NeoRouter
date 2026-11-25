package budget

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

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

func testService(t *testing.T) *Service {
	t.Helper()
	service := New(testDB(t))
	t.Cleanup(service.Reset)
	return service
}

// insertKey writes a user and one router key so the ledger's foreign keys hold.
func insertKey(t *testing.T, service *Service, id string) {
	t.Helper()
	_, err := service.DB.W.ExecContext(context.Background(), `INSERT INTO users (id, username, email, password_hash, role) VALUES ('u', 'u', 'u@example.test', 'x', 'admin')
		ON CONFLICT(id) DO NOTHING`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	_, err = service.DB.W.ExecContext(context.Background(), `INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode) VALUES (?, 'u', ?, ?, ?, 'both')`,
		id, id, id, "hash-"+id)
	if err != nil {
		t.Fatalf("insert key: %v", err)
	}
}

func money(v float64) *float64 { return &v }
func count(v int64) *int64     { return &v }

func TestPeriodBounds(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 34, 56, 0, time.UTC)
	daily := int64(time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC).UnixMilli() * 1)
	if got := PeriodStart("daily", now); got != daily {
		t.Fatalf("daily start = %d", got)
	}
	if end := PeriodEnd("daily", now); end == nil || *end != time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("daily end = %v", end)
	}
	if got := PeriodStart("monthly", now); got != time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("monthly start = %d", got)
	}
	if end := PeriodEnd("monthly", now); end == nil || *end != time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("monthly end = %v", end)
	}
	if got := PeriodStart("total", now); got != 0 {
		t.Fatalf("total start = %d", got)
	}
	if end := PeriodEnd("total", now); end != nil {
		t.Fatalf("total end = %v", end)
	}
	// A December period ends in the next January.
	december := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	if end := PeriodEnd("monthly", december); end == nil || *end != time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("december end = %v", end)
	}
}

func TestPacingOfABudgetRunningOutBeforeThePeriodEnds(t *testing.T) {
	service := testService(t)
	insertKey(t, service, "spent")
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return now })
	if err := service.Record(Key{ID: "spent"}, 60, ""); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// The sixty dollars were spent twenty hours ago.
	if _, err := service.DB.W.ExecContext(context.Background(), `UPDATE budget_ledger SET created_at = ?`, now.Add(-20*time.Hour).UnixMilli()); err != nil {
		t.Fatalf("age the ledger: %v", err)
	}
	key := Key{ID: "spent", BudgetUSD: money(100), BudgetPeriod: "monthly"}
	pace, err := service.Pacing(key, now)
	if err != nil {
		t.Fatalf("Pacing: %v", err)
	}
	if pace == nil {
		t.Fatal("no pacing")
	}
	if pace.State != PacingTight {
		t.Fatalf("state = %q, forecast %v spent %v", pace.State, pace.Forecast, pace.Spent)
	}
	if pace.Forecast == nil || *pace.Forecast <= 100 {
		t.Fatalf("forecast = %v", pace.Forecast)
	}
	if pace.ResetsAt == nil || *pace.ResetsAt != time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("resetsAt = %v", pace.ResetsAt)
	}
	if pace.Spent != 60 || math.Abs(pace.Remaining-40) > 1e-9 {
		t.Fatalf("spent %v remaining %v", pace.Spent, pace.Remaining)
	}
	if pace.CostShift != maxCostShift {
		t.Fatalf("costShift = %v", pace.CostShift)
	}
}

func TestPacingOfAFreshKeyIsNotTight(t *testing.T) {
	service := testService(t)
	insertKey(t, service, "fresh")
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return now })
	pace, err := service.Pacing(Key{ID: "fresh", BudgetUSD: money(100), BudgetPeriod: "monthly"}, now)
	if err != nil {
		t.Fatalf("Pacing: %v", err)
	}
	if pace.State != PacingAhead {
		t.Fatalf("state = %q", pace.State)
	}
	if pace.CostShift != minCostShift {
		t.Fatalf("costShift = %v", pace.CostShift)
	}
	if pace.Forecast == nil || *pace.Forecast != 0 {
		t.Fatalf("forecast = %v", pace.Forecast)
	}
}

func TestOneTimeBudgetHasNoForecast(t *testing.T) {
	service := testService(t)
	insertKey(t, service, "once")
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return now })
	pace, err := service.Pacing(Key{ID: "once", BudgetUSD: money(100), BudgetPeriod: "total"}, now)
	if err != nil {
		t.Fatalf("Pacing: %v", err)
	}
	if pace.State != PacingNone || pace.CostShift != 1 || pace.Forecast != nil || pace.ResetsAt != nil {
		t.Fatalf("pace = %+v", pace)
	}
	if pace.PerRequestCap != 25 {
		t.Fatalf("perRequestCap = %v", pace.PerRequestCap)
	}
	// A one-time budget already spent is over.
	if err := service.Record(Key{ID: "once"}, 100, ""); err != nil {
		t.Fatalf("Record: %v", err)
	}
	service.Reset() // the pacing history cache holds a stale spend
	pace, err = service.Pacing(Key{ID: "once", BudgetUSD: money(100), BudgetPeriod: "total"}, now)
	if err != nil {
		t.Fatalf("Pacing: %v", err)
	}
	if pace.State != PacingOver || pace.Remaining != 0 || pace.PerRequestCap != 0 {
		t.Fatalf("pace = %+v", pace)
	}
}

func TestKeyWithoutBudgetHasNoPacing(t *testing.T) {
	service := testService(t)
	pace, err := service.Pacing(Key{ID: "free"}, time.Now())
	if err != nil || pace != nil {
		t.Fatalf("Pacing = %+v, %v", pace, err)
	}
	if remaining, err := service.Remaining(Key{ID: "free"}, time.Now()); err != nil || remaining != nil {
		t.Fatalf("Remaining = %v, %v", remaining, err)
	}
}

func TestReservationsHoldTheBudgetUntilSettled(t *testing.T) {
	service := testService(t)
	insertKey(t, service, "held")
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return now })
	if err := service.Record(Key{ID: "held"}, 10, ""); err != nil {
		t.Fatalf("Record: %v", err)
	}
	key := Key{ID: "held", BudgetUSD: money(100), BudgetPeriod: "monthly"}

	reservation := service.Reserve(key, 30)
	if got := service.Reserved("held"); got != 30 {
		t.Fatalf("reserved = %v", got)
	}
	remaining, err := service.Remaining(key, now)
	if err != nil {
		t.Fatalf("Remaining: %v", err)
	}
	if math.Abs(*remaining-60) > 1e-9 {
		t.Fatalf("remaining = %v", *remaining)
	}
	if err := reservation.Settle(5, "request-1"); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if got := service.Reserved("held"); got != 0 {
		t.Fatalf("reserved after settle = %v", got)
	}
	spent, err := service.Spent(key, now)
	if err != nil {
		t.Fatalf("Spent: %v", err)
	}
	if spent != 15 {
		t.Fatalf("spent = %v", spent)
	}
	// Releasing twice is harmless and a zero reservation books nothing.
	reservation.Release()
	service.Reserve(key, 0).Settle(0, "")
	if spent, _ := service.Spent(key, now); spent != 15 {
		t.Fatalf("spent = %v", spent)
	}
}

func TestTokensUsedAndTheTokenLimit(t *testing.T) {
	service := testService(t)
	insertKey(t, service, "tokens")
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return now })
	record := func(tokens int64, cost float64, at time.Time, status string) {
		_, err := service.DB.W.ExecContext(context.Background(), `INSERT INTO requests (id, user_id, router_key_id, created_at, api_format, requested_model, status, input_tokens, output_tokens, cost_usd)
			VALUES (?, 'u', 'tokens', ?, 'openai', 'auto', ?, ?, ?, ?)`,
			"r-"+at.Format("150405")+"-"+status+"-"+time.Duration(tokens).String(), at.UnixMilli(), status, tokens, 0, cost)
		if err != nil {
			t.Fatalf("insert request: %v", err)
		}
	}
	record(100, 0, now.Add(-time.Hour), "success")
	record(50, 0, now.Add(-time.Hour), "success")
	// Paid requests and failures do not count against the free token allowance.
	record(500, 0.25, now.Add(-time.Hour), "success")
	record(500, 0, now.Add(-time.Hour), "error")
	// ... nor does anything before the period.
	record(999, 0, time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), "success")

	key := Key{ID: "tokens", TokenLimit: count(150), TokenPeriod: "monthly"}
	used, err := service.TokensUsed(key, now)
	if err != nil {
		t.Fatalf("TokensUsed: %v", err)
	}
	if used != 150 {
		t.Fatalf("tokensUsed = %v", used)
	}
	reached, err := service.TokenLimitReached(key, now)
	if err != nil {
		t.Fatalf("TokenLimitReached: %v", err)
	}
	if !reached {
		t.Fatal("the allowance is used up")
	}
	if reached, _ := service.TokenLimitReached(Key{ID: "tokens", TokenLimit: count(151)}, now); reached {
		t.Fatal("a higher allowance is not used up")
	}
	if reached, _ := service.TokenLimitReached(Key{ID: "tokens"}, now); reached {
		t.Fatal("no token limit is never used up")
	}
	// A daily token period only counts today.
	daily, err := service.TokensUsed(Key{ID: "tokens", TokenPeriod: "daily"}, now)
	if err != nil {
		t.Fatalf("TokensUsed: %v", err)
	}
	if daily != 150 {
		t.Fatalf("daily tokensUsed = %v", daily)
	}
}

func TestCostOfPricesCacheAndOutput(t *testing.T) {
	// 1 USD per million prompt tokens, 2 USD per million completion tokens,
	// half price for cached reads, 1.5x for cache writes.
	entry := Price{
		PriceIn:         money(1),
		PriceOut:        money(2),
		PriceCachedIn:   money(0.5),
		PriceCacheWrite: money(1.5),
	}
	cost := CostOf(entry, &Usage{PromptTokens: 1000, CachedTokens: 400, CacheCreationInputTokens: 200, CompletionTokens: 500})
	// 400 plain prompt (0.0004) + 400 cached (0.0002) + 200 written (0.0003) + 500 output (0.001)
	if math.Abs(cost-0.0019) > 1e-12 {
		t.Fatalf("cost = %v", cost)
	}
	// Cache writes never exceed the prompt: the excess is charged as ordinary input.
	cost = CostOf(entry, &Usage{PromptTokens: 100, CachedTokens: 0, CacheCreationInputTokens: 900, CompletionTokens: 0})
	if math.Abs(cost-0.00015) > 1e-12 {
		t.Fatalf("clamped creation cost = %v", cost)
	}
	// Without a cached or cache-write price both fall back to the prompt price.
	plain := Price{PriceIn: money(1), PriceOut: money(2)}
	cost = CostOf(plain, &Usage{PromptTokens: 1000, CompletionTokens: 500})
	if math.Abs(cost-0.002) > 1e-12 {
		t.Fatalf("plain cost = %v", cost)
	}
	if got := CostOf(Price{IsFree: true}, &Usage{PromptTokens: 1000}); got != 0 {
		t.Fatalf("free cost = %v", got)
	}
	if got := CostOf(Price{IsPlan: true}, &Usage{PromptTokens: 1000}); got != 0 {
		t.Fatalf("plan cost = %v", got)
	}
	if got := CostOf(Price{}, &Usage{PromptTokens: 1000}); got != 0 {
		t.Fatalf("unpriced cost = %v", got)
	}
	if got := CostOf(entry, nil); got != 0 {
		t.Fatalf("no usage cost = %v", got)
	}
}

func TestEstimateMaxCostReservesTheWorstCase(t *testing.T) {
	entry := Price{PriceIn: money(1), PriceOut: money(2), PriceCacheWrite: money(4)}
	estimate := EstimateMaxCost(entry, 1000, 500)
	if estimate == nil || math.Abs(*estimate-(1000*4+500*2)/1e6) > 1e-12 {
		t.Fatalf("estimate = %v", estimate)
	}
	if estimate := EstimateMaxCost(Price{IsFree: true}, 1000, 500); estimate == nil || *estimate != 0 {
		t.Fatalf("free estimate = %v", estimate)
	}
	if estimate := EstimateMaxCost(Price{}, 1000, 500); estimate != nil {
		t.Fatalf("unpriced estimate = %v", *estimate)
	}
}
