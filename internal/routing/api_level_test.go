package routing

import (
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// Ported routing-level assertions from the API tests:
//   test/api/last_resort.test.js, test/api/long_shots.test.js,
//   test/api/task_routing.test.js, test/api/slot_plans.test.js.
// Their HTTP/gateway halves (provider calls, x-neorouter-* headers, usage logs,
// /usage/quotas) need the gateway and live in spec 11; what routing must decide
// is asserted here.

func TestASlowModelThatWorksAnswersWhenTheFastOneIsRateLimited(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	slow := f.entry("ok-slow", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.5)
		e.Stats.TTFTMs = floatPtr(9000)
	})
	fast := f.entry("rl-fast", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.5)
		e.Stats.TTFTMs = floatPtr(300)
	})
	// The fast one is rate-limited for five hours.
	f.svc.stats.recordFailure(fast.Ref, FailureRecord{CooldownUntil: now.Add(5 * time.Hour).UnixMilli(), CooldownReason: strPtr("rate_limited")}, now)
	maxTTFT := 2000
	style, err := f.svc.CreateProfile("u", ProfileInput{
		Name:        strPtr("Snappy"),
		Weights:     &Weights{Intelligence: 0.3, Speed: 0.4, Reliability: 0.2, Cost: 0.1},
		Constraints: &Constraints{MaxTTFTMs: &maxTTFT, Adaptive: true, Exploration: 0.08, IncludeProviders: []string{}, ExcludeProviders: []string{}, ExcludeModels: []string{}},
	}, "u")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey(style.ID), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	ranked := plan.Rank(RankOptions{IncludeRejected: true, Sample: false, Now: now})
	rejected := map[string]string{}
	for _, item := range ranked {
		if item.Rejected != "" {
			rejected[item.Entry.ID] = item.Rejected
		}
	}
	if rejected[slow.ID] != "too_slow" {
		t.Fatalf("slow model rejection = %q, want too_slow", rejected[slow.ID])
	}
	if rejected[fast.ID] != "rate_limited" {
		t.Fatalf("fast model rejection = %q, want rate_limited", rejected[fast.ID])
	}
	if choice := plan.Next(NextOptions{Now: now}); choice != nil {
		t.Fatalf("nothing should be chosen yet: %s", choice.Entry.ID)
	}
	if !plan.EnableLastResort(now, nil) {
		t.Fatalf("a working model held back only by a preference is a last resort")
	}
	choice := plan.Next(NextOptions{Now: now})
	if choice == nil || choice.Entry.ID != slow.ID {
		t.Fatalf("choice = %v, want the slow model", choice)
	}
	if !choice.Entry.LastResort {
		t.Fatalf("the attempt is not marked as a last resort")
	}
}

func TestAModelThatOnceNeededMoreOutputRoomStillAnswers(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("ok-slow", "a", func(e *Entry) { e.IsFree = true })
	// A free model with a learned floor gets the room it needs (boosted), so it
	// is not filtered out by a small max_tokens.
	f.svc.stats.raiseMinOutputTokens(model.Ref, 16000, now)
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: RequestInfo{Messages: []task.Message{{Role: "user", Content: "hi"}}, MaxTokens: intPtr(4096)},
		RequestedModel: "auto", InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	choice := plan.Next(NextOptions{Now: now})
	if choice == nil || choice.Entry.ID != model.ID {
		t.Fatalf("choice = %v, want the model with the learned floor", choice)
	}
	if boosted := f.svc.BoostedMaxTokens(model, plan.Request, now); boosted == nil || *boosted != 16000 {
		t.Fatalf("boosted max tokens = %v, want 16000", boosted)
	}
	// A paid model is not given extra room, so it is skipped when nothing else
	// can serve it at this size.
	paid := f.entry("paid-small", "a", func(e *Entry) { e.PriceOut = floatPtr(2) })
	plan = mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: RequestInfo{Messages: []task.Message{{Role: "user", Content: "hi"}}, MaxTokens: intPtr(4096)},
		RequestedModel: paid.ID, InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	f.svc.stats.raiseMinOutputTokens(paid.Ref, 16000, now)
	if got := f.svc.BoostedMaxTokens(paid, plan.Request, now); got != nil {
		t.Fatalf("a paid model is not boosted: %v", *got)
	}
}

func TestWithEveryModelCoolingDownARequestStillTriesOne(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	first := f.entry("ok-one", "a")
	second := f.entry("ok-two", "a")
	holdBack := func(reason string, hours int) {
		for _, entry := range []*Entry{first, second} {
			f.svc.stats.recordFailure(entry.Ref, FailureRecord{CooldownUntil: now.Add(time.Duration(hours) * time.Hour).UnixMilli(), CooldownReason: &reason}, now)
		}
	}
	holdBack("rate_limited", 5)
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	if choice := plan.Next(NextOptions{Now: now}); choice != nil {
		t.Fatalf("a cooling-down model was chosen: %s", choice.Entry.ID)
	}
	if !plan.EnableLongShots(now, nil) {
		t.Fatalf("a long shot should be enabled")
	}
	choice := plan.Next(NextOptions{Now: now})
	if choice == nil {
		t.Fatalf("a request should still try one model instead of giving up")
	}
	if !choice.Entry.TryingLuck {
		t.Fatalf("the attempt is not marked as a long shot")
	}
	// Only a few such tries per request.
	tries := 1
	for tries < maxLongShots+1 {
		if plan.Next(NextOptions{Now: now}) == nil {
			break
		}
		tries++
	}
	if tries > maxLongShots {
		t.Fatalf("tries = %d, want at most %d", tries, maxLongShots)
	}
}

func TestAHeldBackModelIsALongShotAtMostOncePerGapAcrossRequests(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	models := []*Entry{f.entry("ok-one", "a"), f.entry("ok-two", "a")}
	holdBack := func() {
		for _, entry := range models {
			f.svc.stats.recordFailure(entry.Ref, FailureRecord{CooldownUntil: now.Add(5 * time.Hour).UnixMilli(), CooldownReason: strPtr("rate_limited")}, now)
		}
	}
	holdBack()
	tried := 0
	for round := 0; round <= len(models); round++ {
		plan := mustPlan(t, f.svc, PlanOptions{
			UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
			InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
		})
		if plan.EnableLongShots(now, nil) {
			if choice := plan.Next(NextOptions{Now: now}); choice != nil {
				tried++
			}
		}
		holdBack()
	}
	if tried > len(models) {
		t.Fatalf("each model at most once: %d tries for %d models", tried, len(models))
	}
	if tried != len(models) {
		t.Fatalf("tried = %d, want %d", tried, len(models))
	}
}

func TestWhatNeedsAPersonIsNeverALongShot(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("needs-action", "a")
	reason := "needs_action"
	f.svc.stats.recordFailure(model.Ref, FailureRecord{CooldownUntil: now.Add(20 * time.Hour).UnixMilli(), CooldownReason: &reason}, now)
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	if plan.EnableLongShots(now, nil) {
		t.Fatalf("what needs a person is never a long shot")
	}
	message := ExplainNoModel(plan.RejectionSummary(now, nil), "chat")
	if !strings.Contains(message, "only you can do") {
		t.Fatalf("explanation = %q", message)
	}
}

func TestWhatAPersonMustFixIsNotTried(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("no-payment", "a")
	reason := "payment_required"
	f.svc.stats.recordFailure(model.Ref, FailureRecord{CooldownUntil: now.Add(20 * time.Hour).UnixMilli(), CooldownReason: &reason}, now)
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
	})
	if plan.EnableLongShots(now, nil) {
		t.Fatalf("a payment problem is not a long shot")
	}
	if choice := plan.Next(NextOptions{Now: now}); choice != nil {
		t.Fatalf("a payment problem was chosen: %s", choice.Entry.ID)
	}
	summary := plan.RejectionSummary(now, nil)
	if summary["payment_required"] != 1 {
		t.Fatalf("summary = %v", summary)
	}
}

func TestMarkingAConnectionAsFixedForgetsItsCooldownsAndRest(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	models := []*Entry{f.entry("ok-one", "a"), f.entry("ok-two", "a")}
	for _, entry := range models {
		f.svc.stats.recordFailure(entry.Ref, FailureRecord{CooldownUntil: now.Add(5 * time.Hour).UnixMilli(), CooldownReason: strPtr("rate_limited")}, now)
	}
	for i := 0; i < 40; i++ {
		f.svc.health.onFailure("a", now)
	}
	if f.svc.health.restingUntil("a", now) == 0 {
		t.Fatalf("resting after many failures")
	}
	fixed := f.svc.MarkAccountFixed("a", now)
	if fixed.Models < 2 {
		t.Fatalf("fixed models = %d, want at least 2", fixed.Models)
	}
	if f.svc.health.restingUntil("a", now) != 0 {
		t.Fatalf("restingUntil after markFixed")
	}
	for _, entry := range models {
		if got := f.svc.stats.availability(entry.Ref, now); !got.Available {
			t.Fatalf("model %d still unavailable: %+v", entry.Ref, got)
		}
	}
	if got := f.svc.health.chancesLeft("a"); got != ChancesAfterFixed {
		t.Fatalf("chances = %d, want %d", got, ChancesAfterFixed)
	}
}

func TestAfterMarkingAsFixedTheNextFailuresAreForgivenThenNormalRulesApply(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("ok-one", "a")
	f.svc.health.markFixed("a")
	failure := func() {
		f.svc.OnFailure(model, Failure{Kind: KindRateLimit, Message: "Rate limit reached"}, FailureOptions{Now: now})
	}
	for i := 0; i < ChancesAfterFixed; i++ {
		failure()
	}
	if got := f.svc.stats.availability(model.Ref, now); !got.Available {
		t.Fatalf("forgiven failures leave no cooldown")
	}
	failure()
	if got := f.svc.stats.availability(model.Ref, now); got.Available {
		t.Fatalf("the next one counts")
	}
	if f.svc.stats.snapshot(model.Ref, now).Failures <= 0 {
		t.Fatalf("failures were not recorded")
	}
	// A success ends the new start.
	f.svc.health.markFixed("a")
	f.svc.health.onSuccess("a")
	if got := f.svc.health.chancesLeft("a"); got != 0 {
		t.Fatalf("chances = %d, want 0", got)
	}
}

func TestThePromptTypeDecidesBetweenModelsOfEqualOverallIntelligence(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	coder := f.entry("ok-coder", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.6)
		e.Skills = map[string]float64{"coding": 0.95, "instruction": 0.3, "knowledge": 0.4}
	})
	writer := f.entry("ok-writer", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.6)
		e.Skills = map[string]float64{"coding": 0.3, "instruction": 0.95, "knowledge": 0.6}
	})
	one := 1.0
	zero := 0.0
	exploration := 0.01
	style, err := f.svc.CreateProfile("u", ProfileInput{
		Name:    strPtr("Quality only"),
		Weights: &Weights{Intelligence: one, Speed: zero, Reliability: zero, Cost: zero},
		Constraints: &Constraints{
			Adaptive: false, Exploration: exploration, IncludeProviders: []string{}, ExcludeProviders: []string{}, ExcludeModels: []string{},
		},
	}, "u")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	best := func(prompt string) string {
		plan := mustPlan(t, f.svc, PlanOptions{
			UserID: "u", Key: testKey(style.ID), Request: planRequest(prompt), RequestedModel: "auto",
			InputTokens: 10, Random: func() float64 { return 0 }, Now: now,
		})
		if plan.Task == nil {
			t.Fatalf("no task classification")
		}
		ranked := plan.Rank(RankOptions{Sample: false, Now: now})
		if len(ranked) == 0 {
			t.Fatalf("no candidates")
		}
		return ranked[0].Entry.ID
	}
	if got := best("Write a TypeScript function that debounces calls; handle edge cases and add unit tests"); got != coder.ID {
		t.Fatalf("code prompt picked %s", got)
	}
	if got := best("Write a heartfelt wedding speech for my sister, warm in tone with a funny story"); got != writer.ID {
		t.Fatalf("prose prompt picked %s", got)
	}
	// Apps can name the task.
	hinted := task.Classify(task.Request{Messages: []task.Message{{Role: "user", Content: "Write a heartfelt wedding speech"}}}, 0, "coding")
	if hinted.Primary != "coding" || !hinted.Hinted {
		t.Fatalf("hint = %+v", hinted)
	}
}

func TestSlotWaitPolicy(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	flat := &Entry{Ref: 1, AccountID: "flat", IsFlatRate: true, OverflowPolicy: "auto", CanonicalID: "m", ContextWindow: 100000}
	if got := f.svc.slotBusyReason(flat, now, now); got != "account_busy" {
		t.Fatalf("busy reason = %q", got)
	}
	overflow := *flat
	overflow.OverflowPolicy = "overflow"
	if got := f.svc.slotBusyReason(&overflow, now, now); got != "slot_overflow" {
		t.Fatalf("an overflowing plan reports slot_overflow: %q", got)
	}
	// A short maximum wait overflows instead of waiting.
	short := *flat
	short.SlotWaitMs = intPtr(20)
	if got := f.svc.slotBusyReason(&short, now, now.Add(50*time.Millisecond)); got != "slot_overflow" {
		t.Fatalf("an expired wait overflows: %q", got)
	}
	// A queue policy waits up to the account's bound.
	queue := *flat
	queue.OverflowPolicy = "queue"
	queue.SlotWaitMs = intPtr(1500)
	until := f.svc.slotWaitUntil([]*Entry{&queue}, nil, now, now, true)
	if until == nil || *until != now.Add(1500*time.Millisecond).UnixMilli() {
		t.Fatalf("queue wait = %v, want +1500ms", until)
	}
	// An auto policy queues when no comparable source is free.
	if until := f.svc.slotWaitUntil([]*Entry{flat}, nil, now, now, true); until == nil {
		t.Fatalf("auto queues when nothing comparable is free")
	}
	// A comparable free model means no waiting.
	free := &Candidate{Entry: &Entry{Ref: 2, AccountID: "free", IsFree: true, CanonicalID: "m", ContextWindow: 100000}}
	if until := f.svc.slotWaitUntil([]*Entry{flat}, []*Candidate{free}, now, now, true); until != nil {
		t.Fatalf("a comparable free model means no waiting: %v", until)
	}
	// An available plan also means no waiting.
	planCandidate := &Candidate{Entry: &Entry{Ref: 3, AccountID: "plan", IsPlan: true}}
	if until := f.svc.slotWaitUntil([]*Entry{flat}, []*Candidate{planCandidate}, now, now, true); until != nil {
		t.Fatalf("an available plan means no waiting: %v", until)
	}
}
