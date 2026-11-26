package gateway

// The /v1 endpoints against fake adapters and a real routing service.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// freeKey is a free-only key on the fixture's mock provider.
func freeKey(t *testing.T, f *fixture) string {
	t.Helper()
	secret, _ := f.key(freeKeyMode)
	return secret
}

func TestMissingKeyIsRejected(t *testing.T) {
	f := newFixture(t, nil)
	response := f.do(http.MethodGet, "/v1/models", "", nil, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	if code := errorCode(t, response); code != "INVALID_API_KEY" {
		t.Errorf("code = %q", code)
	}
	body := decode(t, response)
	if message := getString(getObj(body, "error"), "message"); message != "A valid NeoRouter key (nrt_…) is required." {
		t.Errorf("message = %q", message)
	}
	if kind := getString(getObj(body, "error"), "type"); kind != "invalid_request_error" {
		t.Errorf("type = %q", kind)
	}
}

func TestRouterKeyListsAutoProfilesAndCatalogModels(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha", "ok-beta")
	secret := freeKey(t, f)
	response := f.do(http.MethodGet, "/v1/models", secret, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := decode(t, response)
	if object := getString(body, "object"); object != "list" {
		t.Errorf("object = %q", object)
	}
	ids := map[string]*Obj{}
	for _, item := range getList(body, "data") {
		entry := asObj(item)
		ids[getString(entry, "id")] = entry
	}
	for _, id := range []string{"auto", "auto:balanced", "auto:fast", "auto:smart", "mock/ok-alpha", "mock/ok-beta"} {
		if ids[id] == nil {
			t.Errorf("missing %q in %v", id, keysOf(ids))
		}
	}
	model := ids["mock/ok-alpha"]
	if owned := getString(model, "owned_by"); owned != "mock" {
		t.Errorf("owned_by = %q", owned)
	}
	if price := getString(getObj(model, "pricing"), "prompt"); price != "0" {
		t.Errorf("pricing.prompt = %q", price)
	}
	neorouter := getObj(model, "neorouter")
	if kind := getString(neorouter, "kind"); kind != "chat" {
		t.Errorf("kind = %q", kind)
	}
	if free := getBool(neorouter, "free"); !free {
		t.Error("free model is not marked free")
	}
	hidden := getObj(getObj(body, "neorouter"), "not_allowed_for_key")
	if hidden == nil {
		t.Fatal("no not_allowed_for_key")
	}
	auto := ids["auto"]
	if profile := getString(getObj(auto, "neorouter"), "profile"); profile != "balanced" {
		t.Errorf("auto profile = %q", profile)
	}
	if keyDefault := getBool(getObj(auto, "neorouter"), "keyDefault"); !keyDefault {
		t.Error("auto is not the key default")
	}
}

// keysOf is the ids of the list, for a failure message.
func keysOf(models map[string]*Obj) []string {
	out := make([]string, 0, len(models))
	for id := range models {
		out = append(out, id)
	}
	return out
}

func TestExplicitModelNonStreaming(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "mock/ok-alpha",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := decode(t, response)
	if object := getString(body, "object"); object != "chat.completion" {
		t.Errorf("object = %q", object)
	}
	message := getObj(asObj(firstItem(getList(body, "choices"))), "message")
	if content := getString(message, "content"); content != "hello from ok-alpha" {
		t.Errorf("content = %q", content)
	}
	if model := response.Header().Get("X-NeoRouter-Model"); model != "mock/ok-alpha" {
		t.Errorf("X-NeoRouter-Model = %q", model)
	}
	if trail := response.Header().Get("X-NeoRouter-Trail"); trail != "mock/ok-alpha" {
		t.Errorf("X-NeoRouter-Trail = %q", trail)
	}
	if completion, _ := getNumber(getObj(body, "usage"), "completion_tokens"); completion != 6 {
		t.Errorf("completion_tokens = %v", completion)
	}
	stats := getObj(body, "neorouter")
	if model := getString(stats, "model"); model != "mock/ok-alpha" {
		t.Errorf("neorouter.model = %q", model)
	}
	trail := getList(stats, "trail")
	if len(trail) != 1 || trail[0] != "mock/ok-alpha" {
		t.Errorf("neorouter.trail = %v", trail)
	}
	if attempts, _ := getNumber(stats, "attempts"); attempts != 1 {
		t.Errorf("neorouter.attempts = %v", attempts)
	}
	if requestID := getString(stats, "request_id"); requestID == "" {
		t.Error("no request id")
	}
	// A row is recorded, and it holds metadata only.
	var count int
	if err := f.db.R.QueryRow(`SELECT COUNT(*) FROM requests WHERE status = 'success'`).Scan(&count); err != nil {
		t.Fatalf("count requests: %v", err)
	}
	if count != 1 {
		t.Errorf("request rows = %d", count)
	}
	var trailJSON string
	if err := f.db.R.QueryRow(`SELECT trail_json FROM requests ORDER BY created_at DESC LIMIT 1`).Scan(&trailJSON); err != nil {
		t.Fatalf("trail: %v", err)
	}
	if !strings.Contains(trailJSON, `"outcome":"success"`) {
		t.Errorf("trail_json = %q", trailJSON)
	}
	if !strings.Contains(trailJSON, `"ref":`) {
		t.Errorf("trail item has no ref: %q", trailJSON)
	}
}

func TestFailingModelReportsTheTrail(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "fail-delta")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "mock/fail-delta",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := decode(t, response)
	errorBody := getObj(body, "error")
	if code := getString(errorBody, "code"); code != "ALL_ATTEMPTS_FAILED" {
		t.Errorf("code = %q", code)
	}
	if kind := getString(errorBody, "type"); kind != "server_error" {
		t.Errorf("type = %q", kind)
	}
	trail := getList(getObj(errorBody, "details"), "trail")
	if len(trail) == 0 {
		t.Fatal("no trail")
	}
	if outcome := getString(asObj(trail[0]), "outcome"); outcome != "server" {
		t.Errorf("outcome = %q", outcome)
	}
}

func TestStreamingReplaysSSEChunksAndEndsWithDone(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-beta")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "mock/ok-beta",
		"stream", true,
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Errorf("content-type = %q", contentType)
	}
	text := response.Body.String()
	if !strings.Contains(text, `"content":"hello "`) {
		t.Errorf("no first chunk in %q", text)
	}
	if !strings.Contains(text, "data: [DONE]") {
		t.Errorf("no [DONE] in %q", text)
	}
	if !strings.Contains(text, "\n: neorouter {") {
		t.Errorf("no stats comment in %q", text)
	}
}

func TestAnswersCarryLimitsAndTheStreamCarriesStats(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha", "ok-beta")
	rpm := int64(30)
	secret, _ := f.key(func(input *routerkeys.Input) {
		freeKeyMode(input)
		input.RPMLimit = routerkeys.Field[int64]{Set: true, Value: &rpm}
	})
	response := f.chat(secret, newObjOf(
		"model", "mock/ok-alpha",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := decode(t, response)
	limits := getObj(getObj(body, "neorouter"), "limits")
	if perMinute, _ := getNumber(getObj(limits, "requests"), "perMinute"); perMinute != 30 {
		t.Errorf("perMinute = %v", perMinute)
	}
	if limit := response.Header().Get("x-ratelimit-limit-requests"); limit != "30" {
		t.Errorf("x-ratelimit-limit-requests = %q", limit)
	}
	if remaining := response.Header().Get("x-ratelimit-remaining-requests"); remaining != "29" {
		t.Errorf("x-ratelimit-remaining-requests = %q", remaining)
	}
	streamed := f.chat(secret, newObjOf(
		"model", "mock/ok-beta",
		"stream", true,
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if !strings.Contains(streamed.Body.String(), "\n: neorouter {\"request_id\"") {
		t.Errorf("stream has no stats line: %q", streamed.Body.String())
	}
	limitsResponse := f.do(http.MethodGet, "/v1/limits", secret, nil, nil)
	if limitsResponse.Code != http.StatusOK {
		t.Fatalf("limits status = %d", limitsResponse.Code)
	}
	limitsBody := decode(t, limitsResponse)
	if object := getString(limitsBody, "object"); object != "neorouter.limits" {
		t.Errorf("object = %q", object)
	}
	requests := getObj(limitsBody, "requests")
	if remaining, _ := getNumber(requests, "remaining"); remaining != 28 {
		t.Errorf("remaining = %v", remaining)
	}
	if interval, _ := getNumber(requests, "intervalMs"); interval != 2000 {
		t.Errorf("intervalMs = %v", interval)
	}
	if _, ok := getObj(limitsBody, "concurrent").Get("available"); !ok {
		t.Error("concurrent.available is missing")
	}
}

func TestStreamWithoutOutputFallsBack(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "empty-zeta")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "mock/empty-zeta",
		"stream", true,
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	trail := getList(getObj(getObj(decode(t, response), "error"), "details"), "trail")
	if outcome := getString(asObj(firstItem(trail)), "outcome"); outcome != "empty" {
		t.Errorf("outcome = %q", outcome)
	}
}

func TestReasoningWithoutAnAnswerCountsAsEmpty(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier")
	f.model("mock", "think-only")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "mock/think-only",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	trail := getList(getObj(getObj(decode(t, response), "error"), "details"), "trail")
	if outcome := getString(asObj(firstItem(trail)), "outcome"); outcome != "empty" {
		t.Errorf("outcome = %q", outcome)
	}
}

func TestReasoningThatExhaustsMaxTokensIsLearned(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "trunc-thinker")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "mock/trunc-thinker",
		"max_tokens", float64(50),
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	trail := getList(getObj(getObj(decode(t, response), "error"), "details"), "trail")
	if outcome := getString(asObj(firstItem(trail)), "outcome"); outcome != "truncated" {
		t.Errorf("outcome = %q", outcome)
	}
	var minOutput int64
	if err := f.db.R.QueryRow(`SELECT min_output_tokens FROM model_stats WHERE model_ref = ?`, f.catalog.entries[0].Ref).Scan(&minOutput); err != nil {
		t.Fatalf("min_output_tokens: %v", err)
	}
	if minOutput != 100 {
		t.Errorf("min_output_tokens = %d", minOutput)
	}
}

func TestFreeReasoningModelGetsTheRoomItNeeds(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "room-thinker")
	secret := freeKey(t, f)
	request := newObjOf(
		"model", "mock/room-thinker",
		"max_tokens", float64(50),
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	)
	first := f.chat(secret, request)
	trail := getList(getObj(getObj(decode(t, first), "error"), "details"), "trail")
	if outcome := getString(asObj(firstItem(trail)), "outcome"); outcome != "truncated" {
		t.Fatalf("first outcome = %q", outcome)
	}
	var failures, penalty float64
	if err := f.db.R.QueryRow(`SELECT failures, penalty FROM model_stats WHERE model_ref = ?`, f.catalog.entries[0].Ref).Scan(&failures, &penalty); err != nil {
		t.Fatalf("model stats: %v", err)
	}
	if failures != 0 || penalty != 0 {
		t.Errorf("failures = %v, penalty = %v", failures, penalty)
	}
	second := f.chat(secret, request)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d: %s", second.Code, second.Body.String())
	}
	body := decode(t, second)
	message := getObj(asObj(firstItem(getList(body, "choices"))), "message")
	if content := getString(message, "content"); content != "hello with 100" {
		t.Errorf("content = %q", content)
	}
	if raised, ok := getNumber(getObj(body, "neorouter"), "max_tokens_raised_to"); !ok || raised != 100 {
		t.Errorf("max_tokens_raised_to = %v", getValue(getObj(body, "neorouter"), "max_tokens_raised_to"))
	}
}

func TestAnthropicMessagesPlainAndStreaming(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-gamma")
	secret := freeKey(t, f)
	plain := f.do(http.MethodPost, "/v1/messages", secret, newObjOf(
		"model", "mock/ok-gamma",
		"max_tokens", float64(50),
		"system", "be brief",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	), map[string]string{"anthropic-version": "2023-06-01"})
	if plain.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", plain.Code, plain.Body.String())
	}
	body := decode(t, plain)
	if kind := getString(body, "type"); kind != "message" {
		t.Errorf("type = %q", kind)
	}
	if text := getString(asObj(firstItem(getList(body, "content"))), "text"); text != "hello from ok-gamma" {
		t.Errorf("text = %q", text)
	}
	if stop := getString(body, "stop_reason"); stop != "end_turn" {
		t.Errorf("stop_reason = %q", stop)
	}
	streamed := f.do(http.MethodPost, "/v1/messages", secret, newObjOf(
		"model", "mock/ok-gamma",
		"max_tokens", float64(50),
		"stream", true,
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	), nil)
	text := streamed.Body.String()
	for _, want := range []string{"event: message_start", `"type":"text_delta","text":"hello "`, "event: message_stop"} {
		if !strings.Contains(text, want) {
			t.Errorf("stream is missing %q: %q", want, text)
		}
	}
}

func TestAnthropicToolUseRoundtrip(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "tools-eta")
	secret := freeKey(t, f)
	response := f.do(http.MethodPost, "/v1/messages", secret, newObjOf(
		"model", "mock/tools-eta",
		"max_tokens", float64(50),
		"tools", []any{newObjOf("name", "lookup", "description", "Look up", "input_schema", newObjOf("type", "object", "properties", newObjOf("q", newObjOf("type", "string"))))},
		"messages", []any{newObjOf("role", "user", "content", "search")},
	), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := decode(t, response)
	var toolUse *Obj
	for _, item := range getList(body, "content") {
		if getString(asObj(item), "type") == "tool_use" {
			toolUse = asObj(item)
		}
	}
	if toolUse == nil {
		t.Fatalf("no tool_use block: %s", marshal(body))
	}
	input := getObj(toolUse, "input")
	if q := getString(input, "q"); q != "x" {
		t.Errorf("input.q = %q", q)
	}
	if stop := getString(body, "stop_reason"); stop != "tool_use" {
		t.Errorf("stop_reason = %q", stop)
	}
}

func TestRequiredToolCallThatNeverComesFallsBack(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "mock/ok-alpha",
		"tool_choice", "required",
		"tools", []any{newObjOf("type", "function", "function", newObjOf("name", "lookup", "parameters", newObjOf("type", "object", "properties", newObj())))},
		"messages", []any{newObjOf("role", "user", "content", "use the tool")},
	))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	trail := getList(getObj(getObj(decode(t, response), "error"), "details"), "trail")
	if outcome := getString(asObj(firstItem(trail)), "outcome"); outcome != "invalid_output" {
		t.Errorf("outcome = %q", outcome)
	}
}

func TestResponsesAPIPlainAndStreaming(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	plain := f.do(http.MethodPost, "/v1/responses", secret, newObjOf(
		"model", "mock/ok-alpha",
		"instructions", "be brief",
		"input", "hi",
	), nil)
	if plain.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", plain.Code, plain.Body.String())
	}
	body := decode(t, plain)
	if object := getString(body, "object"); object != "response" {
		t.Errorf("object = %q", object)
	}
	if text := getString(body, "output_text"); text != "hello from ok-alpha" {
		t.Errorf("output_text = %q", text)
	}
	streamed := f.do(http.MethodPost, "/v1/responses", secret, newObjOf(
		"model", "mock/ok-alpha",
		"stream", true,
		"input", []any{newObjOf("role", "user", "content", []any{newObjOf("type", "input_text", "text", "hi")})},
	), nil)
	text := streamed.Body.String()
	for _, want := range []string{"event: response.created", "event: response.output_text.delta", "event: response.completed"} {
		if !strings.Contains(text, want) {
			t.Errorf("stream is missing %q", want)
		}
	}
}

func TestFreeOnlyKeyNeverRoutesToPaidModels(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("paid", "paid", "ok-paid")
	for index := range f.catalog.entries {
		f.catalog.entries[index].IsFree = false
		f.catalog.entries[index].BillingMode = "paid"
	}
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "auto",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	errorBody := getObj(decode(t, response), "error")
	if code := getString(errorBody, "code"); code != "NO_MODEL_AVAILABLE" {
		t.Errorf("code = %q", code)
	}
	rejected := getObj(getObj(errorBody, "details"), "rejected")
	if count, _ := getNumber(rejected, "not_free"); count <= 0 {
		t.Errorf("rejected = %s", marshal(rejected))
	}
}

func TestExplicitPaidModelOnAFreeOnlyKeyIs403(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("paid", "paid", "ok-paid")
	for index := range f.catalog.entries {
		f.catalog.entries[index].IsFree = false
		f.catalog.entries[index].BillingMode = "paid"
	}
	secret := freeKey(t, f)
	response := f.chat(secret, newObjOf(
		"model", "paid/ok-paid",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	errorBody := getObj(decode(t, response), "error")
	if code := getString(errorBody, "code"); code != "MODEL_NOT_ALLOWED" {
		t.Errorf("code = %q", code)
	}
	if message := getString(errorBody, "message"); message != `The model "paid/ok-paid" is not free and this key only allows free models.` {
		t.Errorf("message = %q", message)
	}
}

func TestMalformedJSONAndOversizedContext(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	bad := f.do(http.MethodPost, "/v1/chat/completions", secret, `{"model": "auto", "messages": [`, nil)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", bad.Code)
	}
	if code := errorCode(t, bad); code != "INVALID_JSON" {
		t.Errorf("code = %q", code)
	}
	if message := getString(getObj(decode(t, bad), "error"), "message"); message != "The request body is not valid JSON." {
		t.Errorf("message = %q", message)
	}
	huge := f.chat(secret, newObjOf(
		"model", "auto",
		"messages", []any{newObjOf("role", "user", "content", strings.Repeat("word ", 200000))},
	))
	if huge.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", huge.Code, huge.Body.String())
	}
	if code := errorCode(t, huge); code != "CONTEXT_TOO_LARGE" {
		t.Errorf("code = %q", code)
	}
}

func TestModelNotFound(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	response := f.do(http.MethodGet, "/v1/models/nope", secret, nil, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
	body := decode(t, response)
	if code := getString(getObj(body, "error"), "code"); code != "MODEL_NOT_FOUND" {
		t.Errorf("code = %q", code)
	}
	if message := getString(getObj(body, "error"), "message"); message != "Model nope not found." {
		t.Errorf("message = %q", message)
	}
	found := f.do(http.MethodGet, "/v1/models/mock/ok-alpha", secret, nil, nil)
	if found.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", found.Code, found.Body.String())
	}
	model := decode(t, found)
	if id := getString(model, "id"); id != "mock/ok-alpha" {
		t.Errorf("id = %q", id)
	}
}

func TestAnthropicModelListShape(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	response := f.do(http.MethodGet, "/v1/models", secret, nil, map[string]string{"anthropic-version": "2023-06-01"})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := decode(t, response)
	if more := getBool(body, "has_more"); more {
		t.Error("has_more is true")
	}
	data := getList(body, "data")
	if len(data) == 0 {
		t.Fatal("no models")
	}
	first := asObj(data[0])
	if kind := getString(first, "type"); kind != "model" {
		t.Errorf("type = %q", kind)
	}
	if id := getString(body, "first_id"); id != getString(first, "id") {
		t.Errorf("first_id = %q", id)
	}
	if last := getString(body, "last_id"); last != getString(asObj(data[len(data)-1]), "id") {
		t.Errorf("last_id = %q", last)
	}
	if _, ok := first.Get("created_at"); !ok {
		t.Error("no created_at")
	}
}

func TestFeedbackRecordsWhatAPersonSaid(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	answered := f.chat(secret, newObjOf(
		"model", "mock/ok-alpha",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	requestID := getString(getObj(decode(t, answered), "neorouter"), "request_id")
	if requestID == "" {
		t.Fatal("no request id")
	}
	good := f.do(http.MethodPost, "/v1/feedback", secret, newObjOf("request_id", requestID, "rating", "good"), nil)
	if good.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", good.Code, good.Body.String())
	}
	body := decode(t, good)
	if ok := getBool(body, "ok"); !ok {
		t.Error("ok is false")
	}
	if model := getString(body, "model"); model != "mock/ok-alpha" {
		t.Errorf("model = %q", model)
	}
	if len(f.feedback) != 1 || !f.feedback[0].good {
		t.Errorf("feedback = %+v", f.feedback)
	}
	bad := f.do(http.MethodPost, "/v1/feedback", secret, newObjOf("request_id", requestID, "rating", -1), nil)
	if bad.Code != http.StatusOK {
		t.Fatalf("status = %d", bad.Code)
	}
	if len(f.feedback) != 2 || f.feedback[1].good {
		t.Errorf("feedback = %+v", f.feedback)
	}
	invalid := f.do(http.MethodPost, "/v1/feedback", secret, newObjOf("request_id", requestID, "rating", "meh"), nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", invalid.Code)
	}
	if code := errorCode(t, invalid); code != "INVALID_RATING" {
		t.Errorf("code = %q", code)
	}
	unknown := f.do(http.MethodPost, "/v1/feedback", secret, newObjOf("request_id", "nope", "rating", "good"), nil)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("status = %d", unknown.Code)
	}
	if code := errorCode(t, unknown); code != "REQUEST_NOT_FOUND" {
		t.Errorf("code = %q", code)
	}
}

func TestCountTokensEstimates(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	response := f.do(http.MethodPost, "/v1/messages/count_tokens", secret, newObjOf(
		"model", "mock/ok-alpha",
		"messages", []any{newObjOf("role", "user", "content", "hello there")},
	), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := decode(t, response)
	tokens, ok := getNumber(body, "input_tokens")
	if !ok || tokens <= 0 {
		t.Errorf("input_tokens = %v", getValue(body, "input_tokens"))
	}
}

func TestRequestRowsHoldNoPromptText(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	f.chat(secret, newObjOf(
		"model", "mock/ok-alpha",
		"messages", []any{newObjOf("role", "user", "content", "SECRET_PROMPT_TEXT")},
	))
	rows, err := f.db.R.Query(`SELECT * FROM requests`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	for rows.Next() {
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for index := range values {
			targets[index] = &values[index]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		for index, value := range values {
			if text, ok := value.(string); ok && strings.Contains(text, "SECRET_PROMPT_TEXT") {
				t.Errorf("column %s holds the prompt", columns[index])
			}
		}
	}
}

func TestUsageRowRecordsTheOutcome(t *testing.T) {
	f := newFixture(t, nil)
	f.mockProvider("mock", "free_tier", "ok-alpha")
	secret := freeKey(t, f)
	f.chat(secret, newObjOf(
		"model", "mock/ok-alpha",
		"messages", []any{newObjOf("role", "user", "content", "hi")},
	))
	var apiFormat, status, modelLabel string
	var inputTokens, outputTokens int64
	var stream bool
	if err := f.db.R.QueryRow(`SELECT api_format, status, model_label, input_tokens, output_tokens, stream FROM requests LIMIT 1`).
		Scan(&apiFormat, &status, &modelLabel, &inputTokens, &outputTokens, &stream); err != nil {
		t.Fatalf("usage row: %v", err)
	}
	if apiFormat != dialectOpenAIChat || status != "success" || modelLabel != "mock/ok-alpha" {
		t.Errorf("row = %q %q %q", apiFormat, status, modelLabel)
	}
	if inputTokens <= 0 || outputTokens != 6 || stream {
		t.Errorf("tokens = %d/%d stream=%v", inputTokens, outputTokens, stream)
	}
}
