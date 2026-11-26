package routing

import (
	"testing"
	"time"
)

// The sentences are the exact explanations people read.

func TestExplainNoModelSentences(t *testing.T) {
	cases := []struct {
		rejected   map[string]int
		capability string
		want       string
	}{
		{nil, "chat", "There is no model in your catalog yet."},
		{nil, "embedding", "There is no embedding model in your catalog yet."},
		{
			map[string]int{"too_slow": 2, "rate_limited": 3, "needs_action": 1},
			"chat",
			"No model can take this request right now: 3 are used up, rate-limited or cooling down for now; 2 are left out by the routing style; 1 need something only you can do at the provider (accepting terms, a consent, a verification or a plan).",
		},
		{
			map[string]int{"quota_rpm": 2, "not_allowed_for_key": 1, "weird_reason": 4},
			"chat",
			"No model can take this request right now: 4 are switched off or unavailable; 2 are used up, rate-limited or cooling down for now; 1 are not allowed for this key.",
		},
	}
	for _, test := range cases {
		if got := ExplainNoModel(test.rejected, test.capability); got != test.want {
			t.Errorf("ExplainNoModel(%v, %q)\n got: %s\nwant: %s", test.rejected, test.capability, got, test.want)
		}
	}
}

func TestRejectionSummaryGroupsReasons(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	model := f.entry("m1", "a")
	reason := "rate_limited"
	f.svc.stats.recordFailure(model.Ref, FailureRecord{CooldownUntil: now.Add(5 * time.Hour).UnixMilli(), CooldownReason: &reason}, now)
	plan := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0.5 }, Now: now,
	})
	summary := plan.RejectionSummary(now, nil)
	if summary["rate_limited"] != 1 {
		t.Fatalf("summary = %v", summary)
	}
	// A filter marks what the caller cannot use as unsupported.
	summary = plan.RejectionSummary(now, func(*Entry) bool { return false })
	if summary["rate_limited"] != 1 {
		t.Fatalf("a rejected model keeps its own reason: %v", summary)
	}
	other := f.entry("m2", "a")
	plan2 := mustPlan(t, f.svc, PlanOptions{
		UserID: "u", Key: testKey("balanced"), Request: planRequest("hi"), RequestedModel: "auto",
		InputTokens: 10, Random: func() float64 { return 0.5 }, Now: now,
	})
	summary = plan2.RejectionSummary(now, func(entry *Entry) bool { return entry.Ref != other.Ref })
	if summary["unsupported"] != 1 {
		t.Fatalf("summary = %v", summary)
	}
}
