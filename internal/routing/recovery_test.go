package routing

import (
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// The routing assertions for recovery. The provider-error classification
// (classifyHttpError), model-kind detection (detectKind) and metadata
// sameSize/reconcileWithSkills cases belong to the providers and catalog
// domains.

func TestAConnectionThatOnlyFailsRestsLongerEachTimeUntilOneSuccess(t *testing.T) {
	f := newFixture(t)
	now := time.UnixMilli(1_000_000)
	for i := 0; i < FailuresToRest-1; i++ {
		f.svc.health.onFailure("a", now)
	}
	if got := f.svc.health.restingUntil("a", now); got != 0 {
		t.Fatalf("restingUntil = %d, want none", got)
	}
	f.svc.health.onFailure("a", now)
	if got := f.svc.health.restingUntil("a", now); got != now.UnixMilli()+FirstRestMs {
		t.Fatalf("restingUntil = %d, want %d", got, now.UnixMilli()+FirstRestMs)
	}
	later := time.UnixMilli(now.UnixMilli() + FirstRestMs + 1)
	if got := f.svc.health.restingUntil("a", later); got != 0 {
		t.Fatalf("then it gets another try: %d", got)
	}
	for i := 0; i < 3; i++ {
		f.svc.health.onFailure("a", later)
	}
	if got := f.svc.health.restingUntil("a", later); got != later.UnixMilli()+2*FirstRestMs {
		t.Fatalf("a few failures send it back, for longer: %d", got)
	}
	f.svc.health.onSuccess("a")
	if got := f.svc.health.restingUntil("a", later); got != 0 {
		t.Fatalf("a success ends the rest: %d", got)
	}
}

func TestShortOfCapacityARequestIsToldSoonerUnlessItsKeyHasHighPriority(t *testing.T) {
	f := newFixture(t)
	key := RouterKey{UserID: "u1", Priority: "normal"}
	if got := f.svc.QueueWaitMs(key, 45_000); got != 45_000 {
		t.Fatalf("queue wait = %d, want 45000", got)
	}
	for i := 0; i < CapacityMinSamples; i++ {
		f.svc.RecordAnswered("u1", i%2 == 0)
	}
	if !f.svc.ShortOfCapacity("u1") {
		t.Fatalf("a user whose requests mostly fail is short of capacity")
	}
	if got := f.svc.QueueWaitMs(key, 45_000); got != ShortWaitMs {
		t.Fatalf("queue wait = %d, want %d", got, ShortWaitMs)
	}
	high := key
	high.Priority = "high"
	if got := f.svc.QueueWaitMs(high, 45_000); got != 45_000 {
		t.Fatalf("a high-priority key still waits in full: %d", got)
	}
	for i := 0; i < 200; i++ {
		f.svc.RecordAnswered("u1", true)
	}
	if f.svc.ShortOfCapacity("u1") {
		t.Fatalf("and it recovers")
	}
}

func TestTheSameConversationAgainSoonCountsAgainstThatAnswerOnce(t *testing.T) {
	tracker := task.NewRetryTracker()
	messages := []task.Message{{Role: "user", Content: "hi"}}
	id := task.Fingerprint("key", messages)
	if id == task.Fingerprint("key", []task.Message{{Role: "user", Content: "other"}}) {
		t.Fatalf("different conversations share a fingerprint")
	}
	if got := tracker.RepeatedAnswer(id, time.UnixMilli(1000)); got != nil {
		t.Fatalf("nothing remembered yet: %v", got)
	}
	tracker.RememberAnswer(id, task.Answer{Ref: 5, Task: "coding"}, time.UnixMilli(1000))
	if got := tracker.RepeatedAnswer(id, time.UnixMilli(2000)); got == nil || got.Ref != 5 {
		t.Fatalf("the answer this request repeats: %v", got)
	}
	if got := tracker.RepeatedAnswer(id, time.UnixMilli(2000)); got != nil {
		t.Fatalf("counted once per answer: %v", got)
	}
	tracker.RememberAnswer(id, task.Answer{Ref: 5, Task: "coding"}, time.UnixMilli(1000))
	if got := tracker.RepeatedAnswer(id, time.UnixMilli(1000+4*60*1000)); got != nil {
		t.Fatalf("much later it is simply a new question: %v", got)
	}
}

func TestAScoreNobodyMeasuredCannotOutrankAMeasuredOne(t *testing.T) {
	guess := TrustedIntelligence(&Entry{Intelligence: floatPtr(0.5), IntelligenceConfidence: floatPtr(0), IntelligenceSource: "default"})
	measured := TrustedIntelligence(&Entry{Intelligence: floatPtr(0.29), IntelligenceConfidence: floatPtr(0.9), IntelligenceSource: "artificial_analysis"})
	if !(guess < measured) {
		t.Fatalf("guess %v vs measured %v", guess, measured)
	}
	if got := TrustedIntelligence(&Entry{Intelligence: floatPtr(0.9), IntelligenceConfidence: floatPtr(0), IntelligenceSource: "manual"}); got != 0.9 {
		t.Fatalf("set by hand stands: %v", got)
	}
}
