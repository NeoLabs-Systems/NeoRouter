package routing

import (
	"testing"
	"time"
)

// What the app resets or a person says is seen at once, not after a restart.
func TestClearCooldownReachesTheLiveRecord(t *testing.T) {
	f := newFixture(t)
	entry := f.entry("m1", "a")
	now := testNow()
	f.svc.OnFailure(entry, Failure{Kind: Kind("rate_limit"), Status: 429}, FailureOptions{Now: now})
	if f.svc.ModelStats(entry.Ref, now).CooldownUntil <= now.UnixMilli() {
		t.Fatal("no cooldown to clear")
	}
	f.svc.ClearCooldown(entry.Ref, now)
	stats := f.svc.ModelStats(entry.Ref, now.Add(time.Millisecond))
	if stats.CooldownUntil != 0 || stats.BreakerOpenUntil != 0 || stats.Penalty != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}
