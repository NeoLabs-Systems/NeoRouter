package gateway

// Port of test/api/hedging.test.js: a hanging free model is hedged, and an
// explicit model only hedges to the same model elsewhere.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHangingFreeModelIsHedged(t *testing.T) {
	f := newFixture(t, map[string]string{"NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS": "200"})
	f.mockProvider("mock", "free_tier", "hang-one", "ok-two")
	secret := freeKey(t, f)
	started := time.Now()
	type result struct {
		recorder *httptest.ResponseRecorder
		model    string
	}
	done := make(chan result, 6)
	for index := 0; index < 6; index++ {
		go func() {
			response := f.chat(secret, newObjOf(
				"model", "auto",
				"messages", []any{newObjOf("role", "user", "content", "hi")},
			))
			done <- result{recorder: response, model: response.Header().Get("X-NeoRouter-Model")}
		}()
	}
	for index := 0; index < 6; index++ {
		outcome := <-done
		if outcome.recorder.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", outcome.recorder.Code, outcome.recorder.Body.String())
		}
		if outcome.model != "mock/ok-two" {
			t.Errorf("model = %q", outcome.model)
		}
	}
	if elapsed := time.Since(started); elapsed > 4500*time.Millisecond {
		t.Errorf("waited %v for the hanging model", elapsed)
	}
	var hedged int
	if err := f.db.R.QueryRow(`SELECT COUNT(*) FROM requests WHERE trail_json LIKE '%hedge_lost%'`).Scan(&hedged); err != nil {
		t.Fatalf("count hedges: %v", err)
	}
	if hedged == 0 {
		t.Error("no request hedged")
	}
}

func TestExplicitModelHedgesOnlyToTheSameModel(t *testing.T) {
	f := newFixture(t, map[string]string{"NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS": "200"})
	f.mockProvider("mock", "free_tier", "hang-one", "ok-two")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "mock/hang-one",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if model := response.Header().Get("X-NeoRouter-Model"); model != "mock/hang-one" {
		t.Errorf("model = %q", model)
	}
}

func TestHedgedStreamCommitsOnlyTheFastAnswer(t *testing.T) {
	f := newFixture(t, map[string]string{"NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS": "200"})
	f.mockProvider("mock", "free_tier", "hang-one", "ok-two")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "auto",
		"stream", true,
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	text := response.Body.String()
	if strings.Contains(text, `"model":"mock/hang-one"`) {
		t.Errorf("a chunk of the hanging model reached the client: %q", text)
	}
	if !strings.Contains(text, `"content":"hello "`) {
		t.Errorf("no answer in %q", text)
	}
}
