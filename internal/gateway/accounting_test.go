package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/NeoLabs-Systems/neorouter/internal/routing"
)

// A model that streams only reasoning is replaced when the first-answer timeout
// passes, however steadily its chunks arrive.
func TestFirstAnswerDeadlineDoesNotRestartPerChunk(t *testing.T) {
	adapter := &fakeAdapter{stream: func(ctx context.Context, _ *Obj, _ providers.Call, yield func(*Obj) bool) error {
		for ctx.Err() == nil {
			if !yield(BuildChunk("c", "m", 1, newObjOf("reasoning_content", "hmm"), nil, nil)) {
				break
			}
		}
		return nil
	}}
	_, err := runAttempt(context.Background(), &routing.Entry{ModelID: "m"}, adapter,
		newObjOf("stream", true), true, 50*time.Millisecond, "s")
	if classified := providers.AsProviderError(err); classified == nil || classified.Kind != providers.KindTimeout {
		t.Fatalf("err = %v", err)
	}
}

// peerRequest runs one chat request as a pool partner's key.
func peerRequest(t *testing.T, f *fixture, model string) *httptest.ResponseRecorder {
	t.Helper()
	_, public := f.key(nil)
	row, err := f.keys.Row(f.userID, public.ID)
	if err != nil || row == nil {
		t.Fatalf("key row: %v", err)
	}
	link := "link-1"
	row.PeerLinkID = &link
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	f.service.Execute(recorder, request, keyFromRow(row), dialectOpenAIChat,
		newObjOf("model", model, "messages", []any{newObjOf("role", "user", "content", "hi")}), false)
	return recorder
}

func TestPartnerShareIsAnnouncedAndReleased(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-1", "fail-1")
	var served []bool
	f.service.opts.ShareAdmit = func(*Key) (*Share, error) {
		return &Share{
			Headers: map[string]string{"x-ratelimit-limit-requests-day": "300"},
			Release: func(ok bool) { served = append(served, ok) },
		}, nil
	}
	answered := peerRequest(t, f, "mock/ok-1")
	if answered.Code != http.StatusOK || answered.Header().Get("x-ratelimit-limit-requests-day") != "300" {
		t.Fatalf("answer = %d %v %s", answered.Code, answered.Header(), answered.Body)
	}
	if failed := peerRequest(t, f, "mock/fail-1"); failed.Code == http.StatusOK {
		t.Fatalf("failure = %d", failed.Code)
	}
	if len(served) != 2 || !served[0] || served[1] {
		t.Fatalf("released = %v", served)
	}
}

func TestPartnerRefusalSaysWhyAndWhen(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-1")
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		f.service.opts.ShareAdmit = func(*Key) (*Share, error) {
			err := httpserver.New(status, "POOL_REFUSED", "Not now.")
			err.Details = map[string]any{"retryAfterSeconds": int64(300)}
			return nil, err
		}
		refused := peerRequest(t, f, "mock/ok-1")
		if refused.Code != status || refused.Header().Get("Retry-After") != "300" || errorCode(t, refused) != "POOL_REFUSED" {
			t.Fatalf("refusal = %d %q %s", refused.Code, refused.Header().Get("Retry-After"), refused.Body)
		}
	}
}

func TestPeerSafeErrorKeepsTheRetryTimeAndDropsDetails(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusBadRequest} {
		failure := httpserver.New(status, "SOMETHING", "internal: account acc-1",
			map[string]any{"retryAfterSeconds": 7, "rejected": map[string]int{"not_shared": 1}})
		var safe *httpserver.HttpError
		if !errors.As(peerSafeError(failure), &safe) {
			t.Fatal("not an HTTP error")
		}
		details, _ := safe.Details.(map[string]any)
		if safe.RetryAfter != 7 || details["retryAfterSeconds"] != 7 || len(details) != 1 {
			t.Fatalf("status %d: %+v", status, safe)
		}
		if status >= 500 && strings.Contains(safe.Message, "acc-1") {
			t.Fatalf("message = %q", safe.Message)
		}
	}
}

// Requests sent at once must not all pass the budget check before any of them
// holds its cost.
func TestConcurrentRequestsCannotOvershootABudget(t *testing.T) {
	f := newFixture(t, map[string]string{"NEOROUTER_ROUTING_QUEUE_WAIT_MS": "0"})
	id := f.account("paid", "paid")
	release := make(chan struct{})
	var mu sync.Mutex
	upstream := 0
	arrived := make(chan struct{}, 20)
	f.adapters[id].chat = func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error) {
		mu.Lock()
		upstream++
		mu.Unlock()
		arrived <- struct{}{}
		<-release
		return mockChat(call.ModelID)(ctx, request, call)
	}
	f.model("paid", "ok-1", func(entry *routing.Entry) {
		entry.IsFree = false
		entry.BillingMode = "paid"
		entry.PriceIn, entry.PriceOut = float64Ptr(0), float64Ptr(100)
	})
	// About 0.1 USD estimated per call, 0.15 USD to spend.
	secret, _ := f.key(func(input *routerkeys.Input) {
		budget, mode := 0.15, "paid"
		input.BudgetUSD = routerkeys.Field[float64]{Set: true, Value: &budget}
		input.Mode = routerkeys.Field[string]{Set: true, Value: &mode}
	})
	const requests = 20
	done := make(chan int, requests)
	for range requests {
		go func() {
			done <- f.chat(secret, newObjOf("model", "paid/ok-1", "max_tokens", float64(1000),
				"messages", []any{newObjOf("role", "user", "content", "hi")})).Code
		}()
	}
	// Every request but the one holding the budget is refused while it runs.
	<-arrived
	for range requests - 1 {
		select {
		case code := <-done:
			if code == http.StatusOK {
				t.Errorf("a second request was answered")
			}
		case <-arrived:
			close(release)
			t.Fatal("a second request reached the paid upstream")
		}
	}
	close(release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("the holder got %d", code)
	}
	if upstream != 1 {
		t.Fatalf("upstream calls = %d", upstream)
	}
}

func TestAReportedChargeReachesBilling(t *testing.T) {
	f := newFixture(t, nil)
	id := f.account("free", "free_tier")
	usage := newObjOf("prompt_tokens", float64(1), "completion_tokens", float64(1), "cost", 0.5)
	f.adapters[id].chat = func(ctx context.Context, request *Obj, call providers.Call) (*providers.ChatResult, error) {
		body := okResponse(call.ModelID)
		body.Set("usage", usage)
		return &providers.ChatResult{Body: []byte(marshal(body))}, nil
	}
	f.adapters[id].stream = func(ctx context.Context, _ *Obj, _ providers.Call, yield func(*Obj) bool) error {
		yield(BuildChunk("c", "m", 1, newObjOf("content", "hi"), "stop", usage))
		return nil
	}
	f.model("free", "m")
	var charged []float64
	f.service.opts.ObserveCost = func(_ *routing.Entry, cost float64) { charged = append(charged, cost) }
	secret, _ := f.key(nil)
	for _, stream := range []bool{false, true} {
		answer := f.chat(secret, newObjOf("model", "free/m", "stream", stream,
			"messages", []any{newObjOf("role", "user", "content", "hi")}))
		if answer.Code != http.StatusOK {
			t.Fatalf("stream=%v: %d %s", stream, answer.Code, answer.Body)
		}
	}
	if len(charged) != 2 || charged[0] != 0.5 || charged[1] != 0.5 {
		t.Fatalf("charged = %v", charged)
	}
}

// A busy model is worth the configured wait, or a plan slot's longer one, even
// while a user who is short of capacity otherwise waits less.
func TestQueueWindows(t *testing.T) {
	f := newFixture(t, nil)
	f.account("plan", "paid")
	slotWait := 120_000
	f.model("plan", "m", func(entry *routing.Entry) {
		entry.IsFree, entry.IsPlan, entry.IsFlatRate = false, true, true
		entry.OverflowPolicy = "queue"
		entry.SlotWaitMs = &slotWait
	})
	_, public := f.key(nil)
	row, err := f.keys.Row(f.userID, public.ID)
	if err != nil {
		t.Fatal(err)
	}
	key := keyFromRow(row)
	plan, err := f.routing.NewPlan(routing.PlanOptions{UserID: f.userID, Key: key.Routing, RequestedModel: "auto", Capability: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		f.routing.RecordAnswered(f.userID, false)
	}
	now := time.Now()
	waitUntil, busyUntil := f.service.queueWindows(plan, key, now, now.Add(time.Hour))
	if busyUntil.Sub(now) != 2*time.Minute {
		t.Fatalf("busy wait = %v", busyUntil.Sub(now))
	}
	if !waitUntil.Before(busyUntil) {
		t.Fatalf("short of capacity: wait %v, busy %v", waitUntil.Sub(now), busyUntil.Sub(now))
	}
}

func TestResponsesStreamItemHasNoOutput(t *testing.T) {
	events, err := collectEvents(func(yield func(Event) bool) error {
		return ResponsesTranslator{}.StreamFromCanonical(
			StreamFromChunks(BuildChunk("c", "m", 1, newObjOf("content", "hi"), "stop", nil)), "m", yield)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Event != "response.output_item.added" {
			continue
		}
		if text := marshal(event.Data); strings.Contains(text, `"output":`) {
			t.Fatalf("event = %s", text)
		}
		return
	}
	t.Fatal("no output_item.added event")
}
