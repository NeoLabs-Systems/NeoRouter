package gateway

// A working model held back only by a preference still answers, and a model out
// for hours is still tried once.

import (
	"net/http"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// coolDown puts every model out for hours with a reason.
func coolDown(f *fixture, reason string, hours int) {
	for _, entry := range f.catalog.entries {
		f.routing.OnFailure(entry, routing.Failure{
			Kind:    routing.KindRateLimit,
			Message: "Rate limit reached",
			Status:  429,
		}, routing.FailureOptions{Now: time.Now()})
		for index := 0; index < hours*40; index++ {
			f.routing.OnFailure(entry, routing.Failure{Kind: routing.KindRateLimit, Message: "Rate limit reached"}, routing.FailureOptions{Now: time.Now()})
		}
	}
	_ = reason
}

func TestASlowModelThatWorksAnswersWhenNothingElseCan(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-slow")
	// The working model is far slower than the style allows.
	fast := f.catalog.entries[0]
	// The catalog carries what was measured (SELECT_ENTRY joins model_stats).
	fast.Stats = routing.EntryStats{TTFTMs: float64Ptr(9000)}
	for index := 0; index < 5; index++ {
		f.routing.OnSuccess(fast, routing.Measurement{
			LatencyMs:    float64Ptr(12000),
			TTFTMs:       float64Ptr(9000),
			OutputTokens: 100,
			GenerationMs: 3000,
		}, nil, time.Now())
	}
	snappy := routing.Constraints{MaxTTFTMs: intPtr(2000), Adaptive: true, Exploration: 0.08}
	profile, err := f.routing.CreateProfile(f.userID, routing.ProfileInput{
		Name:        stringPtr("Snappy"),
		Weights:     &routing.Weights{Intelligence: 0.3, Speed: 0.4, Reliability: 0.2, Cost: 0.1},
		Constraints: &snappy,
	}, f.userID)
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	profileID := profile.ID
	secret, _ := f.key(func(input *routerkeys.Input) {
		input.Profile = routerkeys.Field[string]{Set: true, Value: &profileID}
	})
	response := f.chat(secret, newObjOf("model", "auto", "messages", []any{newObjOf("role", "user", "content", "hi")}))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var trailJSON string
	if err := f.db.R.QueryRow(`SELECT trail_json FROM requests ORDER BY created_at DESC LIMIT 1`).Scan(&trailJSON); err != nil {
		t.Fatalf("trail: %v", err)
	}
	if !contains(trailJSON, `"lastResort":true`) {
		t.Errorf("trail = %q", trailJSON)
	}
}

func TestAModelThatOnceNeededMoreRoomStillAnswers(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "room-thinker")
	entry := f.catalog.entries[0]
	// The model once needed far more output room than the client allows.
	f.routing.OnFailure(entry, routing.Failure{Kind: routing.KindTruncated, Message: "The model used the whole max_tokens budget before answering."}, routing.FailureOptions{MaxTokens: intPtr(8000), Now: time.Now()})
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "auto",
		"max_tokens", float64(4096),
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if model := response.Header().Get("X-NeoRouter-Model"); model != "mock/room-thinker" {
		t.Errorf("model = %q", model)
	}
}

func TestWithEveryModelCoolingDownARequestStillTriesOne(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-one", "ok-two")
	coolDown(f, "rate_limited", 5)
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf("model", "auto", "messages", []any{newObjOf("role", "user", "content", "hi")}))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if trail := response.Header().Get("X-NeoRouter-Trail"); trail == "" {
		t.Error("no attempt was made")
	}
}

func TestOnlyAFewLongShotsPerRequest(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-one", "ok-two")
	coolDown(f, "not_available", 5)
	secret := freeKey(t, f)
	for _, entry := range f.catalog.entries {
		_ = entry
	}
	response := f.chat(secret, newObjOf("model", "ok-one", "messages", []any{newObjOf("role", "user", "content", "hi")}))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	calls := 0
	for _, adapter := range f.adapters {
		calls += adapter.callCount()
	}
	if calls > 3 {
		t.Errorf("calls = %d", calls)
	}
}

func TestNeedsActionIsNeverTriedAsALongShot(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-one")
	for _, entry := range f.catalog.entries {
		// A consent or a verification: only a person can fix it.
		f.routing.OnFailure(entry, routing.Failure{Kind: routing.KindNotFound, Message: "Accept the terms at the provider", NeedsAction: true}, routing.FailureOptions{Now: time.Now()})
	}
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf("model", "auto", "messages", []any{newObjOf("role", "user", "content", "hi")}))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	message := getString(getObj(decode(t, response), "error"), "message")
	if !contains(message, "only you can do") {
		t.Errorf("message = %q", message)
	}
}

func TestPaymentProblemsAnswerNoModel(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "paid", "ok-paid")
	for index := range f.catalog.entries {
		f.catalog.entries[index].IsFree = false
		f.catalog.entries[index].BillingMode = "paid"
	}
	for _, entry := range f.catalog.entries {
		for index := 0; index < 80; index++ {
			f.routing.OnFailure(entry, routing.Failure{Kind: routing.KindPayment, Message: "Add a payment method", NeedsAction: true}, routing.FailureOptions{Now: time.Now()})
		}
	}
	secret := kindsKey(t, f, true, true, true)
	response := f.chat(secret, newObjOf("model", "auto", "messages", []any{newObjOf("role", "user", "content", "hi")}))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if code := errorCode(t, response); code != "NO_MODEL_AVAILABLE" {
		t.Errorf("code = %q", code)
	}
}

// contains is strings.Contains for a failure message.
func contains(text, fragment string) bool {
	if len(fragment) == 0 {
		return true
	}
	for index := 0; index+len(fragment) <= len(text); index++ {
		if text[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
