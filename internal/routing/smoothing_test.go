package routing

import (
	"testing"
	"time"
)

// Even out usage: allowances are spent at a steady pace once there is enough
// data, and high-priority keys and paid models are left alone.

func (f *fixture) user(id string) {
	f.t.Helper()
	if _, err := f.db.W.Exec(`INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, 'x', 'user')`, id, id, id+"@example.test"); err != nil {
		// A fixture may be shared; an existing user is fine.
		return
	}
}

func (f *fixture) freeEntry(ref int64, accountID string) *Entry {
	return &Entry{
		Ref: ref, ID: accountID + "/some-model", ModelID: "some-model", AccountID: accountID,
		DefinitionID: "custom_openai", BillingMode: "free_tier", IsFree: true,
		Enabled: true, Available: true, AccountEnabled: true, AccountStatus: "healthy",
	}
}

func TestReadinessNeedsAWeekOfHistoryEnoughRequestsAndKnownLimits(t *testing.T) {
	f := newFixture(t)
	f.user("u1")
	now := testNow()
	empty := f.svc.SmoothingReadiness("u1", now)
	if empty.Ready {
		t.Fatalf("an empty account is ready")
	}
	if empty.Days != 0 || empty.ActiveDays != 0 || empty.Requests != 0 {
		t.Fatalf("empty readiness = %+v", empty)
	}
	if _, err := f.svc.SetSmoothingEnabled("u1", true, now); err == nil || httpCode(err) != 409 {
		t.Fatalf("setEnabled = %v, want 409 NOT_ENOUGH_DATA", err)
	}

	// Nine days of answers, but to models without a known limit: still not
	// ready.
	for day := 0; day < 9; day++ {
		for i := 0; i < 15; i++ {
			createdAt := now.Add(-time.Duration(day)*24*time.Hour - time.Duration(i)*time.Minute - time.Hour)
			if _, err := f.db.W.Exec(`INSERT INTO requests (id, user_id, created_at, api_format, requested_model, status)
				VALUES (?, 'u1', ?, 'chat', 'auto', 'success')`, "r-"+formatRef(int64(day))+"-"+formatRef(int64(i)), createdAt.UnixMilli()); err != nil {
				t.Fatalf("insert request: %v", err)
			}
		}
	}
	f.svc.resetSmoothing()
	unknownLimits := f.svc.SmoothingReadiness("u1", now)
	if unknownLimits.Days < 8 || unknownLimits.ActiveDays < 8 {
		t.Fatalf("readiness = %+v", unknownLimits)
	}
	if unknownLimits.Requests != 135 {
		t.Fatalf("requests = %d, want 135", unknownLimits.Requests)
	}
	if unknownLimits.Coverage != 0 {
		t.Fatalf("coverage = %v, want 0", unknownLimits.Coverage)
	}
	if unknownLimits.Ready {
		t.Fatalf("limits must be known for the traffic")
	}
}

func TestKnownLimitsAreCountedOverFreeTrafficOnly(t *testing.T) {
	f := newFixture(t)
	f.user("u3")
	now := testNow()
	if _, err := f.db.W.Exec(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode, enabled, status)
		VALUES ('acc3', 'u3', 'custom_openai', 'mine', 'Mine', 'free_tier', 1, 'healthy')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	refs := map[string]int64{}
	counts := []struct {
		name string
		free bool
		rows int
	}{{"daily", true, 30}, {"minute", true, 30}, {"unknown", true, 20}, {"paid", false, 200}}
	for _, item := range counts {
		ref := f.modelRow("model-"+item.name, "acc3")
		refs[item.name] = ref
		entry := f.freeEntry(ref, "acc3")
		entry.UserID = "u3"
		entry.IsFree = item.free
		entry.ID = "mine/model-" + item.name
		entry.ModelID = "model-" + item.name
		f.catalog.add(entry)
	}
	f.quota.setLimit("m:"+formatRef(refs["daily"]), "rpd", 1000)
	f.quota.setLimit("m:"+formatRef(refs["minute"]), "rpm", 30)
	for _, item := range counts {
		for i := 0; i < item.rows; i++ {
			createdAt := now.Add(-time.Duration(i%6)*24*time.Hour - time.Hour)
			if _, err := f.db.W.Exec(`INSERT INTO requests (id, user_id, created_at, api_format, requested_model, model_ref, status)
				VALUES (?, 'u3', ?, 'chat', 'auto', ?, 'success')`, item.name+"-"+formatRef(int64(i)), createdAt.UnixMilli(), refs[item.name]); err != nil {
				t.Fatalf("insert request: %v", err)
			}
		}
	}
	f.svc.resetSmoothing()
	readiness := f.svc.SmoothingReadiness("u3", now)
	if readiness.Coverage != 0.75 {
		list := f.catalog.ListForUser("u3", CatalogQuery{Capability: "chat", IncludeUnavailable: true})
		t.Logf("entries=%d", len(list))
		for _, entry := range list {
			t.Logf("entry ref=%d id=%s free=%v def=%s paceable=%v known=%v", entry.Ref, entry.ID, entry.IsFree, entry.DefinitionID, paceable(entry), f.svc.limitKnown(entry, now))
		}
	}
	approx(t, readiness.Coverage, 0.75)
}

func TestADailyLimitIsSpreadOverTheDayAnHourGetsItsShare(t *testing.T) {
	f := newFixture(t)
	f.user("u2")
	now := testNow()
	entry := f.freeEntry(1, "acc-1")
	f.quota.setLimit("m:1", "rpd", 240)
	context := SmoothingContext{UserID: "u2", Now: now}
	// No history: every hour counts the same, 240 / 24 × 1.6 ≈ 16 requests an
	// hour.
	if f.svc.SmoothingHeld(entry, context) {
		t.Fatalf("an empty hour was held")
	}
	for i := 0; i < 15; i++ {
		f.quota.recordRequest(entry, 100, now.Add(-time.Duration(i)*time.Minute))
	}
	if f.svc.SmoothingHeld(entry, context) {
		t.Fatalf("15 of about 16 is fine")
	}
	f.quota.recordRequest(entry, 100, now)
	if !f.svc.SmoothingHeld(entry, context) {
		t.Fatalf("past its share for this hour it is held")
	}
	highPriority := context
	highPriority.Priority = "high"
	if f.svc.SmoothingHeld(entry, highPriority) {
		t.Fatalf("a high-priority key still gets through")
	}
	paid := *entry
	paid.IsFree = false
	if f.svc.SmoothingHeld(&paid, context) {
		t.Fatalf("paid models are not paced")
	}
	// An hour later the window has moved on.
	later := context
	later.Now = now.Add(time.Hour + time.Minute)
	if f.svc.SmoothingHeld(entry, later) {
		t.Fatalf("an hour later the window has moved on")
	}
}

func TestAPlanMayRunOnlyALittleAheadOfTheTimeThatHasPassed(t *testing.T) {
	f := newFixture(t)
	f.user("u2")
	now := testNow()
	plan := f.freeEntry(3, "acc-3")
	plan.IsFree = false
	plan.IsPlan = true
	context := SmoothingContext{UserID: "u2", Now: now}
	// A week with six days to go: 1/7 of it has passed.
	f.quota.setStateSimple("a:acc-3", planMetricWeek, 90, now.Add(6*24*time.Hour).UnixMilli(), now)
	if f.svc.SmoothingHeld(plan, context) {
		t.Fatalf("10 %% used at 14 %% of the week")
	}
	f.quota.setStateSimple("a:acc-3", planMetricWeek, 60, now.Add(6*24*time.Hour).UnixMilli(), now)
	if !f.svc.SmoothingHeld(plan, context) {
		t.Fatalf("40 %% used at 14 %% of the week")
	}
	later := context
	later.Now = now.Add(4 * 24 * time.Hour)
	if f.svc.SmoothingHeld(plan, later) {
		t.Fatalf("by day five, 40 %% is behind schedule")
	}
}

func TestALearnedOutputRoomFloorIsRecheckedAfterAWhile(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	f.account("a", "u", "groq", 0, "free_tier")
	ref := f.modelRow("floor", "a")
	f.svc.stats.raiseMinOutputTokens(ref, 16000, now)
	if got := f.svc.stats.outputFloor(ref, now.Add(time.Minute)); got != 16000 {
		t.Fatalf("output floor = %d, want 16000", got)
	}
	stale := now.Add(time.Duration(outputFloorRecheckMs+1) * time.Millisecond)
	if got := f.svc.stats.outputFloor(ref, stale); got != 0 {
		t.Fatalf("tried without it again: %d", got)
	}
	// Running out of room again sets the floor anew, not on top of the stale
	// one.
	f.svc.stats.raiseMinOutputTokens(ref, 4000, stale)
	if got := f.svc.stats.outputFloor(ref, stale.Add(time.Millisecond)); got != 4000 {
		t.Fatalf("output floor = %d, want 4000", got)
	}
}

func TestPacingUsesTheProviderBalanceAndActualReset(t *testing.T) {
	f := newFixture(t)
	f.user("u2")
	now := time.UnixMilli(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC).UnixMilli())
	entry := f.freeEntry(5, "acc-balance")
	f.quota.setLimit("m:5", "rpd", 240)
	resetAt := now.Add(12 * time.Hour).UnixMilli()
	f.quota.setState("m:5", "rpd", QuotaWindow{Limit: floatPtr(240), Remaining: floatPtr(20), ResetAt: &resetAt}, "header", now)
	context := SmoothingContext{UserID: "u2", Now: now}
	if !f.svc.SmoothingHeld(entry, context) {
		t.Fatalf("an empty recent hour does not hide a nearly spent daily allowance")
	}
	wake := f.svc.SmoothingNextAt(entry, context)
	if wake == nil || *wake <= now.UnixMilli() || *wake > now.Add(12*time.Hour).UnixMilli() {
		t.Fatalf("nextAt = %v", wake)
	}
	f.quota.recordRequest(entry, 10, now.Add(time.Millisecond))
	// Last resort must not silently consume the reserved allowance.
	key := testKey("balanced")
	key.UserID = "u2"
	planCtx := &PlanContext{
		StartedAt: now, Key: key, Request: RequestInfo{}, Constraints: DefaultConstraints(),
		PrimaryRefs: map[int64]bool{}, AccountFailures: map[string]int{}, Smoothing: true, LastResort: true,
		UserID: "u2", Group: nil,
	}
	if got := f.svc.Rejection(entry, planCtx, now.Add(2*time.Millisecond)); got != "paced" {
		t.Fatalf("rejection = %q, want paced", got)
	}
	after := context
	after.Now = time.UnixMilli(resetAt + 1)
	if f.svc.SmoothingHeld(entry, after) {
		t.Fatalf("a renewed allowance is not held")
	}
}

func TestPercentagePlanBurnIsReservedBetweenUpstreamObservations(t *testing.T) {
	f := newFixture(t)
	f.user("u2")
	now := testNow()
	plan := f.freeEntry(7, "acc-learned-plan")
	plan.IsFree = false
	plan.IsPlan = true
	resetAt := now.Add(4 * time.Hour).UnixMilli()
	limit := 100.0
	remaining := 90.0
	f.quota.setState("a:acc-learned-plan", planMetric5h, QuotaWindow{Limit: &limit, Remaining: &remaining, ResetAt: &resetAt}, "headers", now)
	f.svc.planOnSent(plan, now.Add(time.Millisecond))
	f.quota.setState("a:acc-learned-plan", planMetric5h, QuotaWindow{Limit: &limit, Remaining: floatPtr(85), ResetAt: &resetAt}, "headers", now.Add(2*time.Millisecond))
	if room := f.svc.planRoomOf(plan, now.Add(2*time.Millisecond)); room == nil || room.Share != 0.85 {
		t.Fatalf("room = %v, want 0.85", room)
	}
	f.svc.planOnSent(plan, now.Add(3*time.Millisecond))
	f.svc.planOnSent(plan, now.Add(4*time.Millisecond))
	if room := f.svc.planRoomOf(plan, now.Add(4*time.Millisecond)); room == nil || room.Share != 0.75 {
		t.Fatalf("later in-flight requests reserve measured plan units: %v", room)
	}
	context := SmoothingContext{UserID: "u2", Now: now.Add(4 * time.Millisecond)}
	if !f.svc.SmoothingHeld(plan, context) {
		t.Fatalf("a plan running ahead of the window is held")
	}
	flat := *plan
	flat.IsFlatRate = true
	if f.svc.SmoothingHeld(&flat, context) {
		t.Fatalf("paid slots have no metered allowance to spread")
	}
}
