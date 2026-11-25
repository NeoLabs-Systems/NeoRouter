package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The OpenAI-compatible dialect: the canonical body goes out unchanged apart
// from the model, the stream flag and the definition's quirks, and the provider's
// answer comes back untouched.

// openAIFake answers the shapes an OpenAI-compatible provider does.
func openAIFake(t *testing.T, options ...func(w http.ResponseWriter, r *http.Request, call chatCall) bool) *chatProvider {
	t.Helper()
	return newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		for _, option := range options {
			if option(w, r, call) {
				return
			}
		}
		switch {
		case r.URL.Path == "/v1/models":
			chatJSON(w, http.StatusOK, `{"object":"list","data":[{"id":"ok-model","context_length":32768,"pricing":{"prompt":"0.000001","completion":"0.000002"}}]}`)
		case r.URL.Path == "/v1/chat/completions":
			if call.body["stream"] == true {
				chatSSE(w, []string{
					`{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"ok-model","choices":[{"index":0,"delta":{"role":"assistant","content":"hi "},"finish_reason":null}]}`,
					`{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"ok-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
					`{"id":"cmpl-1","object":"chat.completion.chunk","created":1,"model":"ok-model","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}`,
				}, true)
				return
			}
			w.Header().Set("x-ratelimit-limit-requests", "1000")
			chatJSON(w, http.StatusOK, `{"id":"cmpl-1","object":"chat.completion","created":1,"model":"ok-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello from ok-model"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}`)
		default:
			chatJSON(w, http.StatusNotFound, `{"error":{"message":"no such path"}}`)
		}
	})
}

// customOpenAI is the definition a user's own OpenAI-compatible endpoint uses.
func customOpenAI(t *testing.T, baseURL string, options map[string]any) *openAICompatible {
	t.Helper()
	def := chatDefinition(t, "custom_openai")
	return newOpenAICompatible(def, chatDeps{baseURL: baseURL + "/v1", secret: "k", options: options}.deps())
}

func TestOpenAICompatibleChatForwardsTheCanonicalBody(t *testing.T) {
	upstream := openAIFake(t)
	adapter := customOpenAI(t, upstream.url(), nil)
	request := chatRequest(t, `{"model":"ignored","messages":[{"role":"user","content":"hi"}],"temperature":0.2,"seed":7,"logprobs":true,"stop":["x"]}`)

	result, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "ok-model"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	sent := upstream.last(t)
	if sent.body["model"] != "ok-model" {
		t.Errorf("model = %v, want the call's model", sent.body["model"])
	}
	if sent.body["stream"] != false {
		t.Errorf("stream = %v, want false", sent.body["stream"])
	}
	// Every field the caller sent is forwarded, including the ones a Go struct
	// would have dropped.
	for _, field := range []string{"temperature", "seed", "logprobs", "stop"} {
		if _, ok := sent.body[field]; !ok {
			t.Errorf("%s was not forwarded: %v", field, sent.body)
		}
	}
	if _, ok := sent.body["stream_options"]; ok {
		t.Errorf("a plain chat must not ask for stream usage: %v", sent.body)
	}
	// The provider's own answer is returned unchanged, key order included.
	if string(result.Body) != `{"id":"cmpl-1","object":"chat.completion","created":1,"model":"ok-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello from ok-model"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}` {
		t.Errorf("the completion was rewritten: %s", result.Body)
	}
	if result.Headers.Get("x-ratelimit-limit-requests") != "1000" {
		t.Errorf("the response headers were dropped: %v", result.Headers)
	}
	decoded := decodedChatResult(t, result.Body)
	if chatPath(t, decoded, "choices") == nil {
		t.Error("the completion has no choices")
	}
}

func TestOpenAICompatibleStreamYieldsTheProviderChunks(t *testing.T) {
	upstream := openAIFake(t)
	adapter := customOpenAI(t, upstream.url(), nil)
	request := chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`)

	var headers http.Header
	chunks := chatChunks(t, adapter.Stream(context.Background(), request, providers.Call{
		ModelID:   "ok-model",
		OnHeaders: func(received http.Header) { headers = received },
	}))
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want the provider's 3", len(chunks))
	}
	if headers.Get("Content-Type") != "text/event-stream" {
		t.Errorf("the stream headers were not reported: %v", headers)
	}
	sent := upstream.last(t)
	if sent.body["stream"] != true {
		t.Errorf("stream = %v, want true", sent.body["stream"])
	}
	options, _ := sent.body["stream_options"].(map[string]any)
	if options["include_usage"] != true {
		t.Errorf("stream usage was not requested: %v", sent.body)
	}
	if sent.headers.Get("Accept") != "text/event-stream" {
		t.Errorf("Accept = %q", sent.headers.Get("Accept"))
	}
}

func TestOpenAICompatibleStreamFallsBackToJSONAnswer(t *testing.T) {
	// Some upstreams ignore stream=true and answer with one JSON body.
	upstream := openAIFake(t, func(w http.ResponseWriter, r *http.Request, call chatCall) bool {
		if r.URL.Path != "/v1/chat/completions" {
			return false
		}
		chatJSON(w, http.StatusOK, `{"id":"cmpl-9","object":"chat.completion","created":3,"model":"ok-model","choices":[{"index":0,"message":{"role":"assistant","content":"one shot","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
		return true
	})
	adapter := customOpenAI(t, upstream.url(), nil)
	chunks := chatChunks(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "ok-model"}))
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if chunks[0]["object"] != "chat.completion.chunk" {
		t.Errorf("object = %v", chunks[0]["object"])
	}
	delta := chatPath(t, chunks[0], "choices").([]any)[0].(map[string]any)["delta"].(map[string]any)
	if delta["content"] != "one shot" {
		t.Errorf("content = %v", delta["content"])
	}
	calls, _ := delta["tool_calls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["index"] != float64(0) {
		t.Errorf("tool calls were not indexed: %v", delta["tool_calls"])
	}
	last := chatPath(t, chunks[1], "choices").([]any)[0].(map[string]any)
	if last["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v", last["finish_reason"])
	}
	if chatPath(t, chunks[1], "usage", "total_tokens") != float64(3) {
		t.Errorf("usage was not carried over: %v", chunks[1]["usage"])
	}
}

func TestOpenAICompatibleStreamRaisesAnEmbeddedError(t *testing.T) {
	upstream := openAIFake(t, func(w http.ResponseWriter, r *http.Request, call chatCall) bool {
		if r.URL.Path != "/v1/chat/completions" {
			return false
		}
		chatSSE(w, []string{`{"error":{"message":"upstream exploded","code":500}}`}, false)
		return true
	})
	adapter := customOpenAI(t, upstream.url(), nil)
	failure := chatStreamFailure(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "ok-model"}))
	if failure == nil {
		t.Fatal("an error in the first chunk must fail the stream")
	}
	if providers.KindOf(failure) != providers.KindServer {
		t.Errorf("kind = %q, want server: %v", providers.KindOf(failure), failure)
	}
}

func TestOpenAICompatibleNonJSONAnswerIsInvalidOutput(t *testing.T) {
	upstream := openAIFake(t, func(w http.ResponseWriter, r *http.Request, call chatCall) bool {
		if r.URL.Path != "/v1/chat/completions" {
			return false
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not a provider</html>"))
		return true
	})
	adapter := customOpenAI(t, upstream.url(), nil)
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "ok-model"})
	if err == nil {
		t.Fatal("an HTML answer must not be a completion")
	}
	if providers.KindOf(err) != providers.KindInvalidOutput {
		t.Errorf("kind = %q, want invalid_output", providers.KindOf(err))
	}
	if err.Error() != "The provider returned a non-JSON response." {
		t.Errorf("message = %q", err.Error())
	}
}

func TestOpenAICompatibleChatRaisesAnEmbeddedError(t *testing.T) {
	upstream := openAIFake(t, func(w http.ResponseWriter, r *http.Request, call chatCall) bool {
		if r.URL.Path != "/v1/chat/completions" {
			return false
		}
		chatJSON(w, http.StatusOK, `{"error":{"message":"Rate limit reached: limit: 30 requests per minute","code":429}}`)
		return true
	})
	adapter := customOpenAI(t, upstream.url(), nil)
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "ok-model"})
	if providers.KindOf(err) != providers.KindRateLimit {
		t.Fatalf("kind = %q, want rate_limit: %v", providers.KindOf(err), err)
	}
}

func TestOpenAICompatibleQuirks(t *testing.T) {
	upstream := openAIFake(t)
	def := chatDefinition(t, "custom_openai")
	// A provider that only takes max_completion_tokens, refuses parallel tool
	// calls and needs a bigger output limit than the caller asked for.
	def.Quirks.MaxTokensField = "max_completion_tokens"
	def.Quirks.SingleToolCall = chatTrue()
	def.Quirks.MinMaxTokens = 100
	def.Quirks.DropFields = []string{"logprobs"}
	def.Quirks.StripSchemaKeys = []string{"$schema"}
	adapter := newOpenAICompatible(def, chatDeps{baseURL: upstream.url() + "/v1", secret: "k"}.deps())

	request := chatRequest(t, `{"messages":[],"max_tokens":5,"logprobs":true,"tools":[{"type":"function","function":{"name":"f","parameters":{"$schema":"x","type":"object"}}}]}`)
	if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "ok-model"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	body := upstream.last(t).body
	if _, present := body["max_tokens"]; present {
		t.Errorf("max_tokens should have been renamed: %v", body)
	}
	// The rename runs before the minimum, so the renamed field keeps the
	// caller's value — the order google_gemini's quirks share.
	if body["max_completion_tokens"] != float64(5) {
		t.Errorf("max_completion_tokens = %v, want the caller's 5", body["max_completion_tokens"])
	}
	if body["parallel_tool_calls"] != false {
		t.Errorf("parallel_tool_calls = %v, want false", body["parallel_tool_calls"])
	}
	if _, present := body["logprobs"]; present {
		t.Errorf("logprobs should have been dropped: %v", body)
	}
	tools, _ := body["tools"].([]any)
	parameters, _ := tools[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if _, present := parameters["$schema"]; present {
		t.Errorf("the schema key should have been stripped: %v", parameters)
	}

	// A definition that only raises the output limit keeps max_tokens.
	raised := chatDefinition(t, "custom_openai")
	raised.Quirks.MinMaxTokens = 100
	adapter = newOpenAICompatible(raised, chatDeps{baseURL: upstream.url() + "/v1", secret: "k"}.deps())
	if _, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[],"max_tokens":5}`), providers.Call{ModelID: "ok-model"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := upstream.last(t).body["max_tokens"]; got != float64(100) {
		t.Errorf("max_tokens = %v, want the definition's minimum", got)
	}
}

func TestOpenAICompatibleSessionHeader(t *testing.T) {
	upstream := openAIFake(t)
	// OpenCode refuses a request without its session header.
	adapter := newOpenAICompatible(chatDefinition(t, "opencode"), chatDeps{baseURL: upstream.url() + "/v1", secret: "k"}.deps())
	request := chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`)

	if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "ok-model"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	derived := upstream.last(t).headers.Get("x-opencode-session")
	if !strings.HasPrefix(derived, "nr-") || len(derived) != 35 {
		t.Errorf("derived session = %q, want an nr- id", derived)
	}

	if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "ok-model", SessionID: "client-1"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := upstream.last(t).headers.Get("x-opencode-session"); got != "client-1" {
		t.Errorf("session header = %q, want the caller's own id", got)
	}
}

func TestOpenAICompatibleListsModelsAndAppliesTheMarketingRules(t *testing.T) {
	// The listing OpenCode serves, reduced to what the adapter reads.
	upstream := openAIFake(t, func(w http.ResponseWriter, r *http.Request, call chatCall) bool {
		if r.URL.Path != "/v1/models" {
			return false
		}
		chatJSON(w, http.StatusOK, `{"object":"list","data":[
			{"id":"claude-haiku-5-5","name":"Claude Haiku 5.5","type":"chat","limit":{"context":1000000,"output":128000},"cost":{"text":{"input":{"price":0.1},"output":{"price":0.5}}},"capabilities":{"tools":{"supported":true}}},
			{"id":"glm-5.3-flash","name":"GLM-5.3-Flash","type":"chat","limit":{"context":1000000,"output":131072},"cost":{"text":{"input":{"price":0.15},"output":{"price":0.5}}}},
			{"id":"big-pickle","name":"Big Pickle","type":"chat","cost":{"text":{"input":{"price":0},"output":{"price":0}}}}
		]}`)
		return true
	})
	adapter := newOpenAICompatible(chatDefinition(t, "opencode"), chatDeps{baseURL: upstream.url() + "/v1", secret: "k"}.deps())

	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	var ids []string
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	// opencode's definition excludes the models it prices at zero.
	if strings.Join(ids, ",") != "claude-haiku-5-5,glm-5.3-flash" {
		t.Fatalf("models = %v", ids)
	}
	haiku := models[0]
	if haiku.ContextWindow == nil || *haiku.ContextWindow != 1_000_000 {
		t.Errorf("contextWindow = %v", haiku.ContextWindow)
	}
	if haiku.MaxOutput == nil || *haiku.MaxOutput != 128_000 {
		t.Errorf("maxOutput = %v", haiku.MaxOutput)
	}
	if haiku.PriceIn == nil || *haiku.PriceIn != 0.1 || haiku.PriceKnown != true || haiku.FreeHint {
		t.Errorf("price = %v known=%v free=%v", haiku.PriceIn, haiku.PriceKnown, haiku.FreeHint)
	}
}

func TestOpenAICompatibleOptInModelsAndStaticListings(t *testing.T) {
	upstream := openAIFake(t, func(w http.ResponseWriter, r *http.Request, call chatCall) bool {
		if r.URL.Path != "/v1/models" {
			return false
		}
		chatJSON(w, http.StatusOK, `{"data":[{"id":"iceman-contributor-free"},{"id":"glm-5.3-flash"}]}`)
		return true
	})
	adapter := newOpenAICompatible(chatDefinition(t, "opencode_go"), chatDeps{baseURL: upstream.url() + "/v1", secret: "k"}.deps())
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	optedIn := map[string]bool{}
	for _, model := range models {
		if model.Raw["optIn"] == true {
			optedIn[model.ID] = true
		}
	}
	if !optedIn["iceman-contributor-free"] {
		t.Errorf("a contributor model should start switched off: %v", models)
	}
	if optedIn["glm-5.3-flash"] {
		t.Errorf("a normal model must not be opted in")
	}

	// A definition that publishes its own list needs no endpoint.
	static := chatDefinition(t, "custom_openai")
	static.Models.Static = []any{map[string]any{"id": "listed-model", "name": "Listed"}}
	queried := false
	fake := openAIFake(t, func(w http.ResponseWriter, r *http.Request, call chatCall) bool {
		queried = true
		return false
	})
	adapter = newOpenAICompatible(static, chatDeps{baseURL: fake.url() + "/v1", secret: "k"}.deps())
	listed, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "listed-model" {
		t.Fatalf("models = %v", listed)
	}
	if queried {
		t.Error("a static listing must not call the provider")
	}
}

func TestOpenAICompatibleManualModelsSurviveAMissingListing(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusNotFound, `{"error":{"message":"no such path"}}`)
	})
	adapter := customOpenAI(t, upstream.url(), map[string]any{"manualModels": []any{"my-model"}})
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("a manual model must survive a missing listing: %v", err)
	}
	if len(models) != 1 || models[0].ID != "my-model" {
		t.Fatalf("models = %v", models)
	}
	// Without a manual model the listing failure is the caller's failure.
	adapter = customOpenAI(t, upstream.url(), nil)
	if _, err := adapter.ListModels(context.Background()); err == nil {
		t.Fatal("a provider that cannot list models must report it")
	}
}

func TestOpenAICompatibleChatProbeValidation(t *testing.T) {
	// The key check uses the cheap model the definition names, and reads a
	// refusal by what it means.
	cases := []struct {
		name     string
		status   int
		body     string
		want     providers.ValidationStatus
		wantKind string
	}{
		{"ok", http.StatusOK, `{"choices":[{"message":{"content":"pong"}}]}`, providers.ValidationHealthy, ""},
		{"refused", http.StatusUnauthorized, `{"error":{"message":"Missing API key."}}`, providers.ValidationInvalid, ""},
		{"out of credit", http.StatusPaymentRequired, `{"error":{"message":"Insufficient account funds"}}`, providers.ValidationHealthy, ""},
		{"used-up trial", http.StatusTooManyRequests, `{"error":{"message":"You are using a Trial key, which is limited to 1000 API calls / month."}}`, providers.ValidationHealthy, ""},
		{"model not open", http.StatusNotFound, `{"error":{"message":"This Go model requires Global regions."}}`, providers.ValidationHealthy, ""},
		{"upstream broke", http.StatusInternalServerError, `{"error":{"message":"upstream exploded"}}`, providers.ValidationUnknown, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var probed string
			upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
				if r.URL.Path == "/v1/models" {
					chatJSON(w, http.StatusOK, `{"data":[{"id":"expensive"},{"id":"glm-5.3-flash"},{"id":"glm-5.3-flash-free"}]}`)
					return
				}
				probed, _ = call.body["model"].(string)
				chatJSON(w, testCase.status, testCase.body)
			})
			adapter := newOpenAICompatible(chatDefinition(t, "opencode"), chatDeps{baseURL: upstream.url() + "/v1", secret: "k"}.deps())
			verdict, err := adapter.Validate(context.Background())
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if verdict.Status != testCase.want {
				t.Fatalf("status = %q, want %q (%v)", verdict.Status, testCase.want, verdict.Message)
			}
			if probed != "glm-5.3-flash" {
				t.Errorf("probed model = %q, want the definition's cheap model", probed)
			}
		})
	}
}

func TestOpenAICompatibleDefaultValidation(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"data":[]}`)
	})
	adapter := customOpenAI(t, upstream.url(), nil)
	verdict, err := adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if verdict.Status != providers.ValidationHealthy || verdict.Message != nil {
		t.Fatalf("verdict = %+v", verdict)
	}
	if upstream.last(t).path != "/v1/models" {
		t.Errorf("path = %q", upstream.last(t).path)
	}

	// An HTML page is not a provider, but it is not a refused key either.
	html := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		_, _ = w.Write([]byte("<html><body>login</body></html>"))
	})
	adapter = customOpenAI(t, html.url(), nil)
	verdict, err = adapter.Validate(context.Background())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if verdict.Status != providers.ValidationUnknown {
		t.Fatalf("status = %q, want unknown", verdict.Status)
	}
	if verdict.Message == nil || *verdict.Message != "The provider returned an HTML page instead of JSON." {
		t.Errorf("message = %v", verdict.Message)
	}
}

func TestOpenAICompatibleFetchQuota(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"data":{"limit":10,"limit_remaining":7}}`)
	})
	adapter := newOpenAICompatible(chatDefinition(t, "openrouter"), chatDeps{baseURL: upstream.url() + "/v1", secret: "k"}.deps())
	windows, err := adapter.FetchQuota(context.Background())
	if err != nil {
		t.Fatalf("quota: %v", err)
	}
	if len(windows) != 1 || windows[0].Metric != "credits_usd" || *windows[0].Remaining != 7 {
		t.Fatalf("windows = %+v", windows)
	}
	// A provider with no probe reports nothing.
	plain := customOpenAI(t, upstream.url(), nil)
	if windows, err := plain.FetchQuota(context.Background()); err != nil || windows != nil {
		t.Fatalf("windows = %+v, err = %v", windows, err)
	}
}

func TestOpenAICompatibleCapabilities(t *testing.T) {
	// A custom endpoint serves every media endpoint its user configures; a
	// definition without media serves chat only.
	custom := chatDefinition(t, "custom_openai")
	custom.Media = providers.MediaSpec{Embed: "/embeddings", Speech: "/audio/speech"}
	adapter := newOpenAICompatible(custom, chatDeps{baseURL: "http://127.0.0.1:1/v1", secret: "k"}.deps())
	if !adapter.Supports(providers.CapChat) {
		t.Error("chat must always be supported")
	}
	// The media methods are installed by the media adapter files; until they are,
	// the set does not claim them.
	if adapter.Supports(providers.CapEmbed) {
		t.Error("an adapter without an embed method must not claim embed")
	}
	plain := newOpenAICompatible(chatDefinition(t, "custom_openai"), chatDeps{baseURL: "http://127.0.0.1:1/v1", secret: "k"}.deps())
	if plain.Supports(providers.CapSpeech) {
		t.Error("a definition without media must not claim speech")
	}
}

func TestOpenAICompatibleAuthFromTheAccountOptions(t *testing.T) {
	upstream := openAIFake(t)
	// A custom endpoint may want the key in a header of its own.
	adapter := customOpenAI(t, upstream.url(), map[string]any{"authHeader": "x-api-key", "headers": map[string]any{"x-extra": "1"}})
	if _, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "ok-model"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	sent := upstream.last(t)
	if sent.headers.Get("x-api-key") != "k" {
		t.Errorf("x-api-key = %q", sent.headers.Get("x-api-key"))
	}
	if sent.headers.Get("Authorization") != "" {
		t.Errorf("Authorization should not be sent: %q", sent.headers.Get("Authorization"))
	}
	if sent.headers.Get("x-extra") != "1" {
		t.Errorf("the account's own header was dropped")
	}
}

func TestOpenAICompatiblePrivateAccountStaysPublic(t *testing.T) {
	// An account that is not the instance admin's may not reach a private host.
	upstream := openAIFake(t)
	adapter := newOpenAICompatible(chatDefinition(t, "custom_openai"), chatDeps{
		baseURL: upstream.url() + "/v1", secret: "k", publicOnly: true, userID: "user-1",
	}.deps())
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "ok-model", Timeout: 2 * time.Second})
	if providers.KindOf(err) != providers.KindBadRequest {
		t.Fatalf("kind = %q, want bad_request: %v", providers.KindOf(err), err)
	}
	if len(upstream.all()) != 0 {
		t.Error("the call must not reach the private host")
	}
}

func TestOpenAICompatibleURLTemplating(t *testing.T) {
	// Cloudflare's base URL carries the account id.
	def := chatDefinition(t, "cloudflare")
	adapter := newOpenAICompatible(def, chatDeps{secret: "k", options: map[string]any{"accountId": "abc 123"}}.deps())
	want := "https://api.cloudflare.com/client/v4/accounts/abc%20123/ai/v1"
	if adapter.baseURL != want {
		t.Fatalf("baseURL = %q, want %q", adapter.baseURL, want)
	}
	if adapter.url("/chat/completions") != want+"/chat/completions" {
		t.Errorf("url = %q", adapter.url("/chat/completions"))
	}
	if adapter.url("https://example.test/x") != "https://example.test/x" {
		t.Errorf("an absolute path must be used as it is")
	}
}

func TestOpenAICompatibleRejectsANonObjectRequest(t *testing.T) {
	adapter := customOpenAI(t, "http://127.0.0.1:1", nil)
	if _, err := adapter.Chat(context.Background(), json.RawMessage(`[1,2]`), providers.Call{ModelID: "m"}); providers.KindOf(err) != providers.KindBadRequest {
		t.Fatalf("kind = %q, want bad_request: %v", providers.KindOf(err), err)
	}
	failure := chatStreamFailure(t, adapter.Stream(context.Background(), json.RawMessage(`not json`), providers.Call{ModelID: "m"}))
	if providers.KindOf(failure) != providers.KindBadRequest {
		t.Fatalf("kind = %q, want bad_request: %v", providers.KindOf(failure), failure)
	}
}
