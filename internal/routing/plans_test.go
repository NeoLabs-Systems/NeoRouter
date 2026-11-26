package routing

import (
	"testing"
	"time"
)

// Plan allowance windows.

func TestTheTightestWindowDecidesAndRequestsSinceTheProbeCountDown(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	entry := &Entry{AccountID: "acc-plan", IsPlan: true}
	limitRequests, remainingRequests := 10.0, 2.0
	weekLimit, weekRemaining := 100.0, 90.0
	resetSoon := now.Add(time.Hour).UnixMilli()
	resetWeek := now.Add(24 * time.Hour).UnixMilli()
	f.quota.setState("a:acc-plan", planMetricRequests, QuotaWindow{Limit: &limitRequests, Remaining: &remainingRequests, ResetAt: &resetSoon}, "probe", now)
	f.quota.setState("a:acc-plan", planMetricWeek, QuotaWindow{Limit: &weekLimit, Remaining: &weekRemaining, ResetAt: &resetWeek}, "probe", now)

	room := f.svc.planRoomOf(entry, now)
	if room == nil {
		t.Fatalf("no room")
	}
	approx(t, room.Share, 0.2)
	if got := f.svc.planFactor(entry, now); !(got > 1) {
		t.Fatalf("well ahead of schedule, the plan is preferred: %v", got)
	}
	f.svc.planOnSent(entry, now)
	if got := f.svc.planFactor(entry, now); got != 0.5 {
		t.Fatalf("running low, held back: %v", got)
	}
	f.svc.planOnSent(entry, now)
	if got := f.svc.planRejection(entry, now); got != "plan_used_up" {
		t.Fatalf("rejection = %q, want plan_used_up", got)
	}
	// "Plan, then pay per use" turns into a normally billed model instead.
	overflow := &Entry{AccountID: "acc-plan", IsPlan: true, PlanOverflow: true}
	if got := f.svc.planEffective(overflow, now); got.IsPlan {
		t.Fatalf("an exhausted overflow plan is still a plan")
	}
	// After the reset time the window is full again.
	if room := f.svc.planRoomOf(entry, now.Add(2*time.Hour)); room == nil || room.Share != 0.9 {
		t.Fatalf("after the reset the tightest window is the week at 0.9: %v", room)
	}
}

func TestTheAllowanceCostsMoreTheFasterItGoesAndNothingWhenItWouldExpireUnused(t *testing.T) {
	f := newFixture(t)
	now := testNow()
	entry := &Entry{AccountID: "acc-paced", IsPlan: true}
	if pace := f.svc.planPace(entry, now); pace != nil {
		t.Fatalf("pace = %v, want unknown", *pace)
	}
	approx(t, f.svc.planAllowanceCost(entry, now), 0.08)
	// 44 % of a 5-hour window left with 3 hours to go: used faster than it
	// renews.
	limit := 100.0
	remaining := 44.0
	resetAt := now.Add(3 * time.Hour).UnixMilli()
	f.quota.setState("a:acc-paced", planMetric5h, QuotaWindow{Limit: &limit, Remaining: &remaining, ResetAt: &resetAt}, "headers", now)
	pace := f.svc.planPace(entry, now)
	if pace == nil {
		t.Fatalf("pace is known")
	}
	approx(t, *pace, 0.44/0.6)
	behind := f.svc.planAllowanceCost(entry, now)
	if !(behind > 0.08 && behind < 0.2) {
		t.Fatalf("behind = %v", behind)
	}
	if got := f.svc.planFactor(entry, now); got != 1 {
		t.Fatalf("factor = %v, want 1", got)
	}
	approx(t, f.svc.costOf(entry, now), 1-behind)
	// 90 % left with 20 minutes to go: most of it would be lost.
	remaining = 90
	resetAt = now.Add(20 * time.Minute).UnixMilli()
	f.quota.setState("a:acc-paced", planMetric5h, QuotaWindow{Limit: &limit, Remaining: &remaining, ResetAt: &resetAt}, "headers", now)
	approx(t, f.svc.planAllowanceCost(entry, now), 0)
	if got := f.svc.planFactor(entry, now); !(got > 1) {
		t.Fatalf("factor = %v, want above 1", got)
	}
	// The tighter window decides: a weekly allowance far behind holds the plan
	// back.
	weekRemaining := 10.0
	weekReset := now.Add(6 * 24 * time.Hour).UnixMilli()
	f.quota.setState("a:acc-paced", planMetricWeek, QuotaWindow{Limit: &limit, Remaining: &weekRemaining, ResetAt: &weekReset}, "headers", now)
	if got := f.svc.planAllowanceCost(entry, now); !(got > behind) {
		t.Fatalf("weekly allowance far behind: %v", got)
	}
	notPlan := &Entry{AccountID: "acc-paced", IsPlan: false}
	approx(t, f.svc.planAllowanceCost(notPlan, now), 0)
}
