package routing

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// The public surface the gateway, the live relay and the routes call, plus the
// rejections those paths depend on.

type fakeKeys struct {
	rpm      map[string]RPMState
	inFlight map[string]int
}

func (k *fakeKeys) RPMState(key RouterKey, now time.Time) *RPMState {
	if state, ok := k.rpm[key.ID]; ok {
		return &state
	}
	return nil
}

func (k *fakeKeys) InFlight(key RouterKey) int { return k.inFlight[key.ID] }

func TestRequestBudgetAndHeaders(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	f.account("a", "u", "custom_openai", 0, "paid")
	f.entry("one", "a", func(e *Entry) { e.DefinitionID = "custom_openai"; e.BillingMode = "paid" })
	f.limits.set("a", StatedLimits{Concurrent: 3})
	f.svc.opts.Keys = &fakeKeys{
		rpm:      map[string]RPMState{"k": {Limit: 60, Remaining: 42, ResetSeconds: 12}},
		inFlight: map[string]int{"k": 1},
	}
	key := testKey("balanced")
	budget := f.svc.RequestBudgetForKey(key, now)
	if budget.Requests == nil || budget.Requests.PerMinute != 60 || budget.Requests.Remaining != 42 || budget.Requests.ResetSeconds != 12 {
		t.Fatalf("requests = %+v", budget.Requests)
	}
	if budget.Requests.IntervalMs != 1000 {
		t.Fatalf("interval = %d, want 1000", budget.Requests.IntervalMs)
	}
	if budget.Concurrent.Limit == nil || *budget.Concurrent.Limit != 3 || budget.Concurrent.InFlight != 0 || budget.Concurrent.Available == nil || *budget.Concurrent.Available != 3 {
		t.Fatalf("concurrent = %+v", budget.Concurrent)
	}
	headers := BudgetHeaders(budget)
	if headers["x-ratelimit-limit-requests"] != "60" || headers["x-ratelimit-remaining-requests"] != "42" || headers["x-ratelimit-reset-requests"] != "12s" {
		t.Fatalf("headers = %v", headers)
	}
	if headers["X-NeoRouter-Concurrency-Limit"] != "3" || headers["X-NeoRouter-Concurrency-Available"] != "3" {
		t.Fatalf("headers = %v", headers)
	}
	// A key's own concurrency limit wins over the connections'.
	ownLimit := 2
	key.ConcurrentLimit = &ownLimit
	budget = f.svc.RequestBudgetForKey(key, now)
	if budget.Concurrent.Limit == nil || *budget.Concurrent.Limit != 2 || budget.Concurrent.InFlight != 1 || budget.Concurrent.Available == nil || *budget.Concurrent.Available != 1 {
		t.Fatalf("own concurrent = %+v", budget.Concurrent)
	}
	// With no known cap nothing is promised.
	f.limits.set("a", StatedLimits{})
	budget = f.svc.RequestBudgetForKey(testKey("balanced"), now)
	if budget.Concurrent.Limit != nil {
		t.Fatalf("limit = %v, want null", *budget.Concurrent.Limit)
	}
	if budget.Concurrent.Available != nil {
		t.Fatalf("available = %v, want null", *budget.Concurrent.Available)
	}
}

func TestQueueClaimsAndBusyWaiting(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m", "a")
	f.limits.set("a", StatedLimits{Concurrent: 1})
	release := f.svc.Lease(model)
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	if !plan.WaitingOnBusy(now, nil) {
		t.Fatalf("a model at its account limit is busy")
	}
	high := plan.Key
	high.Priority = "high"
	plan.Key = high
	plan.JoinQueue(now, nil)
	release()
	// Capacity is free again, but a normal request still leaves what the
	// high-priority one waits for alone (until it is served).
	normal := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	summary := normal.RejectionSummary(now, nil)
	if summary["held_for_priority"] != 1 {
		t.Fatalf("summary = %v, want the model held for a higher priority", summary)
	}
	plan.LeaveQueue()
	plan.LeaveQueue() // idempotent
	normal = mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	if summary := normal.RejectionSummary(now, nil); summary["held_for_priority"] != 0 {
		t.Fatalf("summary after leaving the queue = %v", summary)
	}
}

func TestAccountFailuresMoveTheRequestOn(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m", "a")
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	plan.NoteFailure(model, KindServer)
	plan.NoteFailure(model, KindTimeout)
	if summary := plan.RejectionSummary(now, nil); summary["account_failing"] != 1 {
		t.Fatalf("summary = %v, want the account failing after two failures", summary)
	}
	// A failure kind that says nothing about the connection does not count.
	other := f.entry("m2", "b")
	plan2 := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	plan2.NoteFailure(other, KindBadRequest)
	if plan2.AccountFailures["b"] != 0 {
		t.Fatalf("a bad request counted against the connection")
	}
	// The requested model is still tried despite its account's failures.
	plan3 := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: model.ID,
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	plan3.AccountFailures["a"] = maxFailuresPerAccount
	if choice := plan3.Next(NextOptions{Now: now}); choice == nil {
		t.Fatalf("the model the client asked for must still be tried")
	}
}

func TestQueueWaitMsTakesAPlansSlotWaits(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	f.entry("m", "a", func(e *Entry) {
		e.IsFlatRate = true
		e.OverflowPolicy = "queue"
		e.SlotWaitMs = intPtr(2000)
	})
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	if got := plan.QueueWaitMs(500); got != 2000 {
		t.Fatalf("queue wait = %d, want 2000", got)
	}
}

func TestBudgetRejections(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("paid", "a", func(e *Entry) {
		e.PriceIn = floatPtr(10)
		e.PriceOut = floatPtr(40)
	})
	// A budget with an unknown price rejects the model.
	f.budget.estimate = nil
	key := testKey("balanced")
	key.BudgetUSD = floatPtr(1)
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: key, Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	if summary := plan.RejectionSummary(now, nil); summary["price_unknown"] != 1 {
		t.Fatalf("summary = %v, want price_unknown", summary)
	}
	// A price above what is left rejects it as exhausted.
	f.budget.estimate = floatPtr(0.5)
	f.budget.remaining = floatPtr(0.1)
	plan = mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: key, Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	if summary := plan.RejectionSummary(now, nil); summary["budget_exhausted"] != 1 {
		t.Fatalf("summary = %v, want budget_exhausted", summary)
	}
	// A used-up token allowance stops free and plan models.
	free := f.entry("free", "a", func(e *Entry) { e.IsFree = true })
	f.budget.tokenLimit = true
	f.budget.estimate = floatPtr(0)
	plan = mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: key, Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	_ = free
	summary := plan.RejectionSummary(now, nil)
	if summary["token_limit"] != 1 {
		t.Fatalf("summary = %v, want token_limit for the free model", summary)
	}
	// Over pace, a request far above its share of the budget is a last resort.
	f.budget.tokenLimit = false
	f.budget.pacing = &Pacing{CostShift: 1, PerRequestCap: 0.001}
	f.budget.estimate = floatPtr(1)
	f.budget.remaining = floatPtr(100)
	plan = mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: key, Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	overPace := false
	for _, item := range plan.Rank(RankOptions{Sample: false, Now: now}) {
		if item.Entry.Ref == model.Ref {
			overPace = item.Score < 0.3 // 0.15 × a score that would otherwise be higher
		}
	}
	if !overPace {
		t.Fatalf("an over-pace request was not priced down")
	}
}

func TestPlanNameAndKindHelpers(t *testing.T) {
	if !IsAutoModel("auto") || !IsAutoModel("auto-free:fast") || !IsAutoModel("pool:work") {
		t.Fatalf("auto names")
	}
	if IsAutoModel("groq/llama") {
		t.Fatalf("an explicit model is not auto")
	}
	parsed := ParseModel("auto-free:fast")
	if !parsed.Auto || parsed.Kind != "free" || parsed.Profile != "fast" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed := ParseModel("pool:work"); parsed.Group != "work" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if got := ModelKind(&Entry{IsFree: true}); got != kindFree {
		t.Fatalf("kind = %s", got)
	}
	if got := ModelKind(&Entry{IsPlan: true}); got != kindPlan {
		t.Fatalf("kind = %s", got)
	}
	if got := ModelKind(&Entry{}); got != kindPaid {
		t.Fatalf("kind = %s", got)
	}
	if !GlobToRegExp("groq/*").MatchString("groq/llama") || GlobToRegExp("groq/*").MatchString("openai/gpt") {
		t.Fatalf("glob")
	}
	key := testKey("balanced")
	if got := KindAllowed(&Entry{}, key, false); got != "" {
		t.Fatalf("paid allowed: %q", got)
	}
	freeOnly := key
	freeOnly.Mode = "free_only"
	if got := KindAllowed(&Entry{}, freeOnly, false); got != "not_free" {
		t.Fatalf("free-only mode: %q", got)
	}
	denied := false
	key.AllowFree = &denied
	if got := KindAllowed(&Entry{IsFree: true}, key, false); got != "not_allowed_for_key" {
		t.Fatalf("free models off: %q", got)
	}
	if !NeedsCredits(&Entry{BillingMode: "free_tier", FreeReason: "paid"}) {
		t.Fatalf("a paid model on a free connection needs credits")
	}
	if NeedsCredits(&Entry{BillingMode: "paid", FreeReason: "paid"}) {
		t.Fatalf("a paid connection still uses it")
	}
	if got := CachedTokens(100, 250); got != 100 {
		t.Fatalf("cached tokens are never more than the prompt: %d", got)
	}
	if got := CachedTokens(100, -5); got != 0 {
		t.Fatalf("cached tokens are never negative: %d", got)
	}
}

func TestPlanHeadersLearnPlanWindows(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	entry := &Entry{AccountID: "acc-codex", IsPlan: true}
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "25")
	headers.Set("x-codex-primary-window-minutes", "300")
	headers.Set("x-codex-primary-reset-at", "1760000000")
	headers.Set("x-codex-secondary-used-percent", "40")
	headers.Set("x-codex-secondary-window-minutes", "10080")
	f.svc.plansObserveHeaders(entry, headers, now)
	windows := map[string]QuotaWindow{}
	for _, window := range f.quota.accountState("acc-codex") {
		windows[window.Metric] = window
	}
	primary, ok := windows[planMetric5h]
	if !ok || primary.Limit == nil || *primary.Limit != 100 || primary.Remaining == nil || *primary.Remaining != 75 {
		t.Fatalf("primary window = %+v", primary)
	}
	if primary.ResetAt == nil || *primary.ResetAt != 1_760_000_000_000 {
		t.Fatalf("reset at = %v", primary.ResetAt)
	}
	if secondary, ok := windows[planMetricWeek]; !ok || secondary.Remaining == nil || *secondary.Remaining != 60 {
		t.Fatalf("secondary window = %+v", secondary)
	}
}

func TestLiveEventsAndSlowAttempts(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m", "a")
	release := f.svc.Lease(model)
	if counts := f.svc.Inflight(model); counts.Model != 1 || counts.Account != 1 {
		t.Fatalf("inflight = %+v", counts)
	}
	release()
	release() // idempotent
	if counts := f.svc.Inflight(model); counts.Model != 0 || counts.Account != 0 {
		t.Fatalf("inflight after release = %+v", counts)
	}
	f.svc.OnSlow(model, 4200, now)
	snapshot := f.svc.stats.snapshot(model.Ref, now)
	if snapshot.LatencyMs == nil || *snapshot.LatencyMs != 4200 {
		t.Fatalf("a slow attempt lowers the speed estimate: %v", snapshot.LatencyMs)
	}
	// A failed attempt marks the running attempt as failed in the live view.
	f.svc.LiveStarted("u", "req-1", "mine", "")
	f.svc.LiveAttempt("u", "req-1", "a", model.ID)
	f.svc.LiveFailed("u", "req-1", "a")
	read := f.svc.ReadLive(t.Context(), "u", 0, 0)
	if len(read.InFlight) != 1 || len(read.InFlight[0].Attempts) != 1 || read.InFlight[0].Attempts[0].State != "failed" {
		t.Fatalf("in flight = %+v", read.InFlight)
	}
	if read.InFlight[0].Task != nil {
		t.Fatalf("a request without a task has no task field: %v", *read.InFlight[0].Task)
	}
	// A finished request leaves the in-flight list.
	f.svc.LiveFinished("u", "req-1", true, model.ID)
	read = f.svc.ReadLive(t.Context(), "u", 0, 0)
	if len(read.InFlight) != 0 {
		t.Fatalf("in flight = %+v", read.InFlight)
	}
}

func TestKindPatternsNarrowOnlyTheKindsTheyNameAModelOf(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	speech := f.entry("whisper", "a", func(e *Entry) {
		e.Capability = "speech"
		e.ModelID = "whisper"
		e.ID = "a/whisper"
	})
	chat := f.entry("gpt", "a", func(e *Entry) { e.ModelID = "gpt"; e.ID = "a/gpt" })
	key := testKey("balanced")
	key.AllowedModelsJSON = mustJSON([]string{"kind:chat", "kind:speech", "a/gpt"})
	// The key takes only the kinds it lists.
	if !f.svc.AllowedByKey(chat, key, now) {
		t.Fatalf("a chat model named by the patterns is allowed")
	}
	if f.svc.AllowedByKey(f.entry("other", "a", func(e *Entry) { e.Capability = "image"; e.ID = "a/other" }), key, now) {
		t.Fatalf("an image model is not one of the listed kinds")
	}
	// Model patterns that name no model of a kind leave that kind open.
	open := testKey("balanced")
	open.AllowedModelsJSON = mustJSON([]string{"kind:speech", "a/gpt"})
	if !f.svc.AllowedByKey(speech, open, now) {
		t.Fatalf("speech models are covered by a pattern naming a chat model")
	}
	if f.svc.AllowedByKey(chat, open, now) {
		t.Fatalf("the chat model is not matched by its pattern? it is named directly")
	}
}

func TestRequirementsAndTokenEstimate(t *testing.T) {
	request := RequestInfo{Messages: []task.Message{
		{Role: "user", Content: []task.Part{{Type: "text", Text: "hello"}, {Type: "image_url"}}},
	}, Tools: []task.Tool{{Type: "function", Function: task.ToolFunction{Name: "probe"}}}, ResponseFormat: &task.ResponseFormat{Type: "json_object"}}
	needs := Requirements(request)
	if !needs.Vision || !needs.Tools || !needs.JSON {
		t.Fatalf("needs = %+v", needs)
	}
	tokens := EstimateInputTokens(request)
	if tokens < 850 {
		t.Fatalf("an image counts flat: %d", tokens)
	}
}

func TestInitializeAndSlotUtilization(t *testing.T) {
	f := newFixture(t)
	f.account("flat", "u", "custom_openai", flatRate, "paid")
	entry := f.entry("work", "flat", func(e *Entry) {
		e.DefinitionID = "custom_openai"
		e.BillingMode = "paid"
		e.IsFlatRate = true
		e.SlotWaitMs = intPtr(1500)
	})
	f.limits.set("flat", StatedLimits{Concurrent: 3})
	maintenance, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.svc.Initialize(maintenance); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	release := f.svc.Lease(entry)
	time.Sleep(5 * time.Millisecond)
	release()
	time.Sleep(5 * time.Millisecond)
	snapshot := f.svc.SlotSnapshot("flat", time.Now())
	if snapshot.ObservedMs <= 0 || snapshot.PeakBusy != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.Series) == 0 {
		t.Fatalf("no buckets were written: %+v", snapshot)
	}
	if snapshot.Limit == nil || *snapshot.Limit != 3 {
		t.Fatalf("limit = %v", snapshot.Limit)
	}
	if snapshot.IdleShare == nil || *snapshot.IdleShare < 0 || *snapshot.IdleShare > 1 {
		t.Fatalf("idle share = %v", snapshot.IdleShare)
	}
}

func TestModelStatsAndAvailabilityForTheLearningView(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m", "a")
	f.svc.OnFailure(model, Failure{Kind: KindRateLimit, Message: "slow down"}, FailureOptions{Now: now})
	availability := f.svc.ModelAvailability(model.Ref, now)
	if availability.Available || availability.Reason != "rate_limited" {
		t.Fatalf("availability = %+v", availability)
	}
	stats := f.svc.ModelStats(model.Ref, now)
	if stats.Failures <= 0 || stats.CooldownReason == nil || *stats.CooldownReason != "rate_limited" {
		t.Fatalf("stats = %+v", stats)
	}
	// The skills per kind the learning view weighs.
	coding := task.SkillsOfKind("coding")
	if coding["coding"] != 1 || coding["agentic"] != 0.2 {
		t.Fatalf("coding skills = %v", coding)
	}
	if len(task.SkillsOfKind("nonsense")) != 0 {
		t.Fatalf("an unknown kind has no skills")
	}
	if all := task.SkillsByKind(); len(all) != len(task.Kinds) {
		t.Fatalf("skills by kind = %d entries", len(all))
	}
}
