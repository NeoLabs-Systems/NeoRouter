package routing

import (
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

func fixedRandom() float64 { return 0.5 }

// Scoring, stats and learning.

func TestPresetsFavourTheirDimension(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	smart := f.entry("m1", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.95)
		e.PriceIn = floatPtr(10)
		e.PriceOut = floatPtr(40)
	})
	cheap := f.entry("m2", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.3)
		e.IsFree = true
	})
	score := func(entry *Entry, preset string) float64 {
		weights := presetByID(preset).Weights
		return f.svc.score(entry, scoreOptions{Weights: weights, Sample: false, Random: fixedRandom, Now: now}).Score
	}
	if !(score(smart, "smart") > score(cheap, "smart")) {
		t.Fatalf("smart preset did not prefer the smarter model")
	}
	want := []string{"balanced", "fast", "smart"}
	for index, preset := range Presets {
		if preset.ID != want[index] {
			t.Fatalf("presets = %v", Presets)
		}
	}
	// Earlier preset names still resolve.
	if got := f.svc.ResolveProfile("", "best"); got == nil || got.ID != "smart" {
		t.Fatalf("resolve(best) = %v", got)
	}
	if got := f.svc.ResolveProfile("", "cheap"); got == nil || got.ID != "balanced" {
		t.Fatalf("resolve(cheap) = %v", got)
	}
}

func TestFailuresLowerReliabilityAndSuccessesRestoreIt(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m1", "a")
	before := Reliability(f.svc.stats.snapshot(model.Ref, now), false, nil)
	for i := 0; i < 5; i++ {
		f.svc.OnFailure(model, Failure{Kind: KindServer, Message: "boom"}, FailureOptions{Now: now})
	}
	after := Reliability(f.svc.stats.snapshot(model.Ref, now), false, nil)
	if !(after < before) {
		t.Fatalf("reliability did not fall: %v -> %v", before, after)
	}
	if got := f.svc.stats.availability(model.Ref, now); got.Reason != "circuit_open" {
		t.Fatalf("availability = %+v, want circuit_open after three failures in 15 minutes", got)
	}
	f.svc.stats.clearCooldown(model.Ref, now)
	for i := 0; i < 30; i++ {
		f.svc.OnSuccess(model, Measurement{LatencyMs: floatPtr(500), TTFTMs: floatPtr(100), OutputTokens: 100, GenerationMs: 400}, nil, now)
	}
	if got := Reliability(f.svc.stats.snapshot(model.Ref, now), false, nil); !(got > after) {
		t.Fatalf("reliability did not recover: %v", got)
	}
}

func TestQuotaExhaustionEscalatesTheCooldownLadder(t *testing.T) {
	f := newFixture(t)
	model := f.entry("m1", "a")
	now := testNow()
	var durations []int64
	for step := 0; step < 5; step++ {
		at := now.Add(time.Duration(step) * time.Second)
		f.svc.OnFailure(model, Failure{Kind: KindQuotaExhausted, Message: "daily quota exceeded"}, FailureOptions{Now: at})
		durations = append(durations, f.svc.stats.snapshot(model.Ref, at).CooldownUntil-at.UnixMilli())
	}
	want := append(append([]int64{}, quotaLadderMs...), quotaLadderMs[len(quotaLadderMs)-1])
	if len(durations) != len(want) {
		t.Fatalf("durations = %v", durations)
	}
	for index := range want {
		if durations[index] != want[index] {
			t.Fatalf("durations = %v, want %v", durations, want)
		}
	}
}

func TestRateLimitsRespectRetryAfterAndContextErrorsOnlyTeachTheLimit(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m1", "a")
	retry := 300 * time.Second
	f.svc.OnFailure(model, Failure{Kind: KindRateLimit, Message: "slow down", RetryAfter: &retry}, FailureOptions{Now: now})
	if got := f.svc.stats.snapshot(model.Ref, now).CooldownUntil; got != now.UnixMilli()+300_000 {
		t.Fatalf("cooldown_until = %d, want %d", got, now.UnixMilli()+300_000)
	}
	other := f.entry("m2", "a")
	f.svc.OnFailure(other, Failure{Kind: KindContextLength, Message: "too long"}, FailureOptions{EstimatedTokens: 10000, Now: now})
	if got := f.svc.stats.snapshot(other.Ref, now).Failures; got != 0 {
		t.Fatalf("failures = %v, want 0", got)
	}
	var contextWindow *int64
	if err := f.db.R.QueryRow(`SELECT context_window FROM models WHERE id = ?`, other.Ref).Scan(&contextWindow); err != nil {
		t.Fatalf("read context window: %v", err)
	}
	if contextWindow == nil || *contextWindow != 9000 {
		t.Fatalf("context_window = %v, want 9000", contextWindow)
	}
}

func TestPenaltiesDecayOverTime(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m1", "a")
	f.svc.OnFailure(model, Failure{Kind: KindServer, Message: "x"}, FailureOptions{Now: now})
	fresh := f.svc.stats.snapshot(model.Ref, now).Penalty
	later := f.svc.stats.snapshot(model.Ref, now.Add(30*time.Minute)).Penalty
	if !(later < fresh/10) {
		t.Fatalf("penalty %v did not fade below %v", later, fresh/10)
	}
}

func TestAdaptiveWeightsLeanOnIntelligenceForHardRequestsAndOnPriceForEasyOnes(t *testing.T) {
	weights := presetByID("balanced").Weights
	sum := func(w Weights) float64 { return w.Intelligence + w.Speed + w.Reliability + w.Cost }
	inBetween := 0.4
	if got := AdaptWeights(weights, &inBetween, true); got != weights {
		t.Fatalf("in between, the profile stands: %+v", got)
	}
	hardDifficulty := 1.0
	hard := AdaptWeights(weights, &hardDifficulty, true)
	if !(hard.Intelligence > weights.Intelligence) {
		t.Fatalf("hard request did not lean on intelligence")
	}
	easyDifficulty := 0.05
	easy := AdaptWeights(weights, &easyDifficulty, true)
	if !(easy.Intelligence < weights.Intelligence) || !(easy.Cost > weights.Cost) || easy.Speed != weights.Speed {
		t.Fatalf("easy request = %+v", easy)
	}
	for _, shifted := range []Weights{hard, easy} {
		if diff := sum(shifted) - sum(weights); diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("weights changed their sum: %v", diff)
		}
	}
	if got := AdaptWeights(weights, &easyDifficulty, false); got != weights {
		t.Fatalf("a style can turn adaptation off: %+v", got)
	}
	if got := AdaptWeights(weights, nil, true); got != weights {
		t.Fatalf("requests without a prompt keep the weights: %+v", got)
	}
}

func TestAZeroFreeQuotaPausesTheModelForADay(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m1", "a")
	failure := Failure{Kind: KindQuotaExhausted, Message: "You exceeded your current quota", Learned: &LearnedLimit{Metric: "rpd", Value: 0}}
	f.svc.OnFailure(model, failure, FailureOptions{Now: now})
	snapshot := f.svc.stats.snapshot(model.Ref, now)
	if snapshot.CooldownReason == nil || *snapshot.CooldownReason != "no_free_quota" {
		t.Fatalf("cooldown reason = %v", snapshot.CooldownReason)
	}
	if snapshot.CooldownUntil != now.UnixMilli()+dayMs {
		t.Fatalf("cooldown until = %d", snapshot.CooldownUntil)
	}
}

func TestModelsThatStayMissingBackOffFurtherEachTime(t *testing.T) {
	f := newFixture(t)
	model := f.entry("m1", "a")
	now := testNow()
	var durations []int64
	for step := 0; step < 3; step++ {
		at := now.Add(time.Duration(step) * time.Millisecond)
		f.svc.OnFailure(model, Failure{Kind: KindNotFound, Message: "Function not found for account"}, FailureOptions{Now: at})
		durations = append(durations, f.svc.stats.snapshot(model.Ref, at).CooldownUntil-at.UnixMilli())
	}
	want := []int64{6 * 3600_000, 24 * 3600_000, 7 * 24 * 3600_000}
	for index := range want {
		if durations[index] != want[index] {
			t.Fatalf("durations = %v, want %v", durations, want)
		}
	}
}

func TestRateLimitOnAModelThatNeverAnswersBacksOffFurther(t *testing.T) {
	f := newFixture(t)
	model := f.entry("m1", "a")
	now := testNow()
	var durations []int64
	for i := 0; i < 4; i++ {
		at := now.Add(time.Duration(i) * 100 * time.Millisecond)
		f.svc.OnFailure(model, Failure{Kind: KindRateLimit, Message: "slow down"}, FailureOptions{Now: at})
		durations = append(durations, f.svc.stats.snapshot(model.Ref, at).CooldownUntil-at.UnixMilli())
	}
	want := []int64{90_000, 300_000, 1_800_000, 7_200_000}
	for index := range want {
		if durations[index] != want[index] {
			t.Fatalf("durations = %v, want %v", durations, want)
		}
	}
	f.svc.OnSuccess(model, Measurement{LatencyMs: floatPtr(100)}, nil, now.Add(time.Second))
	f.svc.OnFailure(model, Failure{Kind: KindRateLimit, Message: "slow down"}, FailureOptions{Now: now.Add(2 * time.Second)})
	// The earlier, longer cooldown is cleared by nothing here, so check the
	// level did not rise.
	if got := f.svc.stats.snapshot(model.Ref, now.Add(2*time.Second)).CooldownLevel; got != 0 {
		t.Fatalf("cooldown level = %d, want 0", got)
	}
}

func TestAboveWhatATaskNeedsMoreIntelligenceCountsOnlyAsTheStyleWeighsIt(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	frontier := f.entry("frontier", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.95)
		e.PriceIn = floatPtr(5)
		e.PriceOut = floatPtr(20)
	})
	decent := f.entry("decent", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.6)
		e.PriceIn = floatPtr(0.1)
		e.PriceOut = floatPtr(0.4)
	})
	weights := presetByID("balanced").Weights
	score := func(entry *Entry, need *float64) float64 {
		return f.svc.score(entry, scoreOptions{Weights: weights, Need: need, Sample: false, Random: fixedRandom, Now: now}).Score
	}
	if !(score(frontier, nil) > score(decent, nil)) {
		t.Fatalf("without a prompt to judge, the smarter model leads")
	}
	easy := 0.1
	easyNeed := task.NeededIntelligence(&easy)
	if !(score(decent, easyNeed) > score(frontier, easyNeed)) {
		t.Fatalf("an easy prompt goes to the good-enough cheaper model")
	}
	hard := 0.9
	hardNeed := task.NeededIntelligence(&hard)
	if !(score(frontier, hardNeed) > score(decent, hardNeed)) {
		t.Fatalf("a hard one to the smarter model")
	}
	half := 0.5
	if got := task.EnoughIntelligence(0.7, &half, 1); got != 0.7 {
		t.Fatalf("a style that weighs only intelligence keeps the full scale: %v", got)
	}
	if task.NeededIntelligence(nil) != nil {
		t.Fatalf("neededIntelligence(null) is not nil")
	}
}

func TestAQualityProfileDoesNotPickATinyModelJustBecauseItIsFastAndReliable(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	big := f.entry("big", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.85)
		e.IntelligenceSource = "manual"
	})
	tiny := f.entry("tiny", "a", func(e *Entry) {
		e.Intelligence = floatPtr(0.25)
		e.IntelligenceSource = "manual"
	})
	for i := 0; i < 40; i++ {
		f.svc.OnSuccess(tiny, Measurement{LatencyMs: floatPtr(150), TTFTMs: floatPtr(50), OutputTokens: 200, GenerationMs: 100}, nil, now)
	}
	for i := 0; i < 5; i++ {
		f.svc.OnSuccess(big, Measurement{LatencyMs: floatPtr(4000), TTFTMs: floatPtr(900), OutputTokens: 200, GenerationMs: 3100}, nil, now)
	}
	order := func(profile string) []int64 {
		plan := mustPlan(t, f.svc, PlanOptions{
			UserID:         "u",
			Key:            testKey(profile),
			Request:        planRequest("How do vaccines train the immune system?"),
			RequestedModel: "auto",
			InputTokens:    10,
			Random:         fixedRandom,
			Now:            now,
		})
		var refs []int64
		for _, item := range plan.Rank(RankOptions{Sample: false, Now: now}) {
			if item.Rejected == "" && (item.Entry.Ref == big.Ref || item.Entry.Ref == tiny.Ref) {
				refs = append(refs, item.Entry.Ref)
			}
		}
		return refs
	}
	if got := order("balanced"); len(got) == 0 || got[0] != big.Ref {
		t.Fatalf("balanced order = %v, want big first", got)
	}
	if got := order("smart"); len(got) == 0 || got[0] != big.Ref {
		t.Fatalf("smart order = %v, want big first", got)
	}
	if got := order("fast"); len(got) == 0 || got[0] != tiny.Ref {
		t.Fatalf("speed-first profiles may still prefer the small model: %v", got)
	}
}

func TestRoutingEntriesInAKeysAllowedListLimitWhichAutosItMayAskFor(t *testing.T) {
	f := newFixture(t)
	key := func(patterns []string, profile string) RouterKey {
		encoded := mustJSON(patterns)
		return RouterKey{ID: "k", UserID: "u", Mode: "both", AllowedModelsJSON: encoded, Profile: profile}
	}
	if !f.svc.RouteAllowedByKey("auto:coding", key(nil, "")) {
		t.Fatalf("an empty list allows everything")
	}
	if !f.svc.RouteAllowedByKey("groq/llama", key([]string{"groq/*"}, "")) {
		t.Fatalf("model patterns alone leave every auto open")
	}
	if !f.svc.RouteAllowedByKey("auto:fast", key([]string{"groq/*"}, "")) {
		t.Fatalf("model patterns alone leave every auto open")
	}
	if !f.svc.RouteAllowedByKey("auto:fast", key([]string{"auto:fast"}, "")) {
		t.Fatalf("auto:fast should match auto:fast")
	}
	if f.svc.RouteAllowedByKey("auto:smart", key([]string{"auto:fast"}, "")) {
		t.Fatalf("auto:smart matched auto:fast")
	}
	if !f.svc.RouteAllowedByKey("auto", key([]string{"auto:fast"}, "fast")) {
		t.Fatalf("plain auto counts as auto:<key profile>")
	}
	if !f.svc.RouteAllowedByKey("auto:default", key([]string{"auto"}, "")) {
		t.Fatalf("auto:default is plain auto")
	}
	if !f.svc.RouteAllowedByKey("auto-free:smart", key([]string{"auto*"}, "")) {
		t.Fatalf("auto-free:smart matched auto*")
	}
	if f.svc.RouteAllowedByKey("groq/llama", key([]string{"auto"}, "")) {
		t.Fatalf("routing only must not allow models named directly")
	}
	if !f.svc.RouteAllowedByKey("groq/llama", key([]string{"auto", "groq/*"}, "")) {
		t.Fatalf("routing and model patterns allow both")
	}
	if !f.svc.AllowedByKey(&Entry{ID: "openai/gpt"}, key([]string{"auto:fast"}, ""), time.Now()) {
		t.Fatalf("routing entries do not narrow the models routing picks from")
	}
	if f.svc.AllowedByKey(&Entry{ID: "openai/gpt"}, key([]string{"auto:fast", "groq/*"}, ""), time.Now()) {
		t.Fatalf("model patterns narrow the models routing picks from")
	}
}
