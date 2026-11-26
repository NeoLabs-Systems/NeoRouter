package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Anthropic's dialect: the outbound translator (canonical → Messages) and the
// inbound reading of its answers, ported from test/unit/translators.test.js.

func anthropicAdapter(t *testing.T, baseURL string) *anthropic {
	t.Helper()
	return newAnthropic(chatDefinition(t, "anthropic"), chatDeps{baseURL: baseURL, secret: "sk-ant"}.deps())
}

func TestAnthropicRequestMapsBackToTheUpstreamFormat(t *testing.T) {
	canonical, err := transport.ParseOrdered([]byte(`{
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},
			{"role":"assistant","content":null,"tool_calls":[{"id":"t1","type":"function","function":{"name":"get","arguments":"{\"a\":1}"}}]},
			{"role":"tool","tool_call_id":"t1","content":"ok"}
		],
		"tools":[{"type":"function","function":{"name":"get","parameters":{"type":"object"}}}]
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body := anthropicRequest(chatObj(canonical), anthropicDefaultMaxTokens())
	plain := chatPlainValue(body).(map[string]any)

	system := plain["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v", system)
	}
	systemBlock := system[0].(map[string]any)
	if systemBlock["text"] != "sys" {
		t.Errorf("system text = %v", systemBlock["text"])
	}
	cache, _ := systemBlock["cache_control"].(map[string]any)
	if cache["type"] != "ephemeral" {
		t.Errorf("the stable system block must be a cache breakpoint: %v", systemBlock)
	}

	messages := plain["messages"].([]any)
	user := messages[0].(map[string]any)
	parts := user["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("the user turn = %v", parts)
	}
	image := parts[1].(map[string]any)
	source := image["source"].(map[string]any)
	if image["type"] != "image" || source["media_type"] != "image/png" || source["data"] != "AAAA" {
		t.Errorf("a data URL must become a base64 image block: %v", image)
	}
	assistant := messages[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Errorf("role = %v", assistant["role"])
	}
	toolUse := assistant["content"].([]any)[0].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["name"] != "get" {
		t.Errorf("the tool call = %v", toolUse)
	}
	if input := toolUse["input"].(map[string]any); input["a"] != float64(1) {
		t.Errorf("the arguments were not parsed: %v", input)
	}
	toolResult := messages[2].(map[string]any)
	if toolResult["content"].([]any)[0].(map[string]any)["type"] != "tool_result" {
		t.Errorf("the tool answer = %v", toolResult)
	}
	// Anthropic's default output limit is the JS adapter's clamp: at most 8192.
	if chatNumberAt(t, plain["max_tokens"]) != 8192 {
		t.Errorf("max_tokens = %v", plain["max_tokens"])
	}
	tools := plain["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["name"] != "get" {
		t.Errorf("tool = %v", tool)
	}
	if _, present := tool["input_schema"]; !present {
		t.Errorf("the schema must travel as input_schema: %v", tool)
	}
	if cache, _ := tool["cache_control"].(map[string]any); cache["type"] != "ephemeral" {
		t.Errorf("the last tool must be a cache breakpoint: %v", tool)
	}
}

func TestAnthropicRequestToolChoiceAndThinking(t *testing.T) {
	canonical, err := transport.ParseOrdered([]byte(`{
		"messages":[{"role":"user","content":"hi"}],
		"max_tokens":40000,
		"temperature":2,
		"tools":[{"type":"function","function":{"name":"f"}}],
		"tool_choice":"required",
		"parallel_tool_calls":false,
		"reasoning_effort":"high",
		"response_format":{"type":"json_schema","json_schema":{"schema":{"type":"object"}}}
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	plain := chatPlainValue(anthropicRequest(chatObj(canonical), anthropicDefaultMaxTokens())).(map[string]any)
	choice := plain["tool_choice"].(map[string]any)
	if choice["type"] != "any" || choice["disable_parallel_tool_use"] != true {
		t.Errorf("tool_choice = %v", choice)
	}
	thinking, _ := plain["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || chatNumberAt(t, thinking["budget_tokens"]) != 24576 {
		t.Errorf("thinking = %v", thinking)
	}
	if _, present := plain["temperature"]; present {
		t.Error("a thinking model must not be sent a temperature")
	}
	// The JSON instruction joins the system blocks, which keeps the breakpoint on
	// the last one.
	system := plain["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v", system)
	}
	if _, ok := system[0].(map[string]any)["cache_control"]; !ok {
		t.Errorf("the JSON instruction must still carry the breakpoint: %v", system)
	}
}

func TestAnthropicAnswerReadsBackAsACanonicalCompletion(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{
			"id":"msg_1","type":"message","role":"assistant","model":"claude-x",
			"content":[
				{"type":"thinking","thinking":"let me think"},
				{"type":"text","text":"hello"},
				{"type":"tool_use","id":"tu","name":"f","input":{"a":1}}
			],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":7,"output_tokens":4,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}
		}`)
	})
	adapter := anthropicAdapter(t, upstream.url())
	result, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[{"role":"user","content":"hi"}]}`), providers.Call{ModelID: "claude-x"})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	sent := upstream.last(t)
	if sent.path != "/messages" {
		t.Errorf("path = %q", sent.path)
	}
	if sent.headers.Get("x-api-key") != "sk-ant" {
		t.Errorf("x-api-key = %q", sent.headers.Get("x-api-key"))
	}
	if sent.headers.Get("anthropic-version") != anthropicAPIVersion {
		t.Errorf("anthropic-version = %q", sent.headers.Get("anthropic-version"))
	}
	if sent.body["stream"] != false || sent.body["model"] != "claude-x" {
		t.Errorf("body = %v", sent.body)
	}

	completion := decodedChatResult(t, result.Body)
	if completion["object"] != "chat.completion" || completion["id"] != "msg_1" {
		t.Errorf("completion = %v", completion)
	}
	message := chatPath(t, completion, "choices").([]any)[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "hello" {
		t.Errorf("content = %v", message["content"])
	}
	if message["reasoning_content"] != "let me think" {
		t.Errorf("reasoning = %v", message["reasoning_content"])
	}
	calls := message["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool calls = %v", calls)
	}
	call := calls[0].(map[string]any)
	if call["id"] != "tu" || call["function"].(map[string]any)["arguments"] != `{"a":1}` {
		t.Errorf("tool call = %v", call)
	}
	choice := chatPath(t, completion, "choices").([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	usage := completion["usage"].(map[string]any)
	// Cache reads and writes are prompts too.
	if usage["prompt_tokens"] != float64(10) || usage["completion_tokens"] != float64(4) || usage["total_tokens"] != float64(14) {
		t.Errorf("usage = %v", usage)
	}
	if usage["prompt_tokens_details"].(map[string]any)["cached_tokens"] != float64(2) {
		t.Errorf("cached tokens = %v", usage["prompt_tokens_details"])
	}
	if usage["cache_creation_input_tokens"] != float64(1) {
		t.Errorf("cache creation = %v", usage["cache_creation_input_tokens"])
	}
}

func TestAnthropicAnswerErrorBecomesAFailure(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	})
	adapter := anthropicAdapter(t, upstream.url())
	_, err := adapter.Chat(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "claude-x"})
	if err == nil {
		t.Fatal("a 200 that carries an error must fail")
	}
	if providers.KindOf(err) != providers.KindServer {
		t.Errorf("kind = %q, want server: %v", providers.KindOf(err), err)
	}
}

func TestAnthropicStreamMapsToCanonicalChunks(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatSSE(w, []string{
			`{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":7}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu","name":"f"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
			`{"type":"message_stop"}`,
		}, false)
	})
	adapter := anthropicAdapter(t, upstream.url())
	chunks := chatChunks(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "claude-x"}))
	if len(chunks) != 5 {
		t.Fatalf("chunks = %d: %v", len(chunks), chunks)
	}
	if chunks[0]["id"] != "msg_1" {
		t.Errorf("the message id must come from the upstream: %v", chunks[0]["id"])
	}
	delta := chatPath(t, chunks[1], "choices").([]any)[0].(map[string]any)["delta"].(map[string]any)
	if delta["content"] != "Hi" {
		t.Errorf("content = %v", delta)
	}
	toolDelta := chatPath(t, chunks[2], "choices").([]any)[0].(map[string]any)["delta"].(map[string]any)
	call := toolDelta["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "tu" || call["index"] != float64(0) {
		t.Errorf("tool call = %v", call)
	}
	arguments := chatPath(t, chunks[3], "choices").([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if arguments["function"].(map[string]any)["arguments"] != "{}" {
		t.Errorf("tool arguments = %v", arguments)
	}
	last := chunks[len(chunks)-1]
	if chatPath(t, last, "choices").([]any)[0].(map[string]any)["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v", last)
	}
	usage := last["usage"].(map[string]any)
	want := map[string]any{
		"prompt_tokens": float64(7), "completion_tokens": float64(4), "total_tokens": float64(11),
		"cache_creation_input_tokens": float64(0),
	}
	for name, value := range want {
		if usage[name] != value {
			t.Errorf("usage[%s] = %v, want %v", name, usage[name], value)
		}
	}
	if usage["prompt_tokens_details"].(map[string]any)["cached_tokens"] != float64(0) {
		t.Errorf("cached tokens = %v", usage["prompt_tokens_details"])
	}
}

func TestAnthropicStreamErrorEventIsClassified(t *testing.T) {
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatSSE(w, []string{`{"type":"error","error":{"type":"rate_limit_error","message":"Number of requests exceeded"}}`}, false)
	})
	adapter := anthropicAdapter(t, upstream.url())
	failure := chatStreamFailure(t, adapter.Stream(context.Background(), chatRequest(t, `{"messages":[]}`), providers.Call{ModelID: "claude-x"}))
	if providers.KindOf(failure) != providers.KindRateLimit {
		t.Fatalf("kind = %q, want rate_limit: %v", providers.KindOf(failure), failure)
	}
}

func TestAnthropicListsModelsWithTheirWindows(t *testing.T) {
	var pages int
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		pages++
		if call.query == "limit=1000" {
			chatJSON(w, http.StatusOK, `{"data":[{"id":"claude-a","display_name":"Claude A","max_input_tokens":200000,"max_tokens":8192}],"has_more":true,"last_id":"claude-a"}`)
			return
		}
		if call.query != "limit=1000&after_id=claude-a" {
			t.Errorf("query = %q", call.query)
		}
		chatJSON(w, http.StatusOK, `{"data":[{"id":"claude-b","max_input_tokens":1000}],"has_more":false}`)
	})
	adapter := anthropicAdapter(t, upstream.url())
	models, err := adapter.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if pages != 2 {
		t.Errorf("pages = %d, want the listing to be paged", pages)
	}
	if len(models) != 2 {
		t.Fatalf("models = %v", models)
	}
	if models[0].ID != "claude-a" || models[0].Name != "Claude A" {
		t.Errorf("model = %+v", models[0])
	}
	if models[0].ContextWindow == nil || *models[0].ContextWindow != 200000 {
		t.Errorf("context window = %v, want max_input_tokens", models[0].ContextWindow)
	}
	if models[0].MaxOutput == nil || *models[0].MaxOutput != 8192 {
		t.Errorf("max output = %v, want max_tokens", models[0].MaxOutput)
	}
	if models[0].Tools == nil || !*models[0].Tools || models[0].Vision == nil || !*models[0].Vision {
		t.Errorf("Anthropic models take tools and images: %+v", models[0])
	}
}

func TestAnthropicDefaults(t *testing.T) {
	// base_adapter.js defines neither flag, and the media handler reads an
	// absent one as true; only Gemini turns timestamps off.
	adapter := anthropicAdapter(t, "http://127.0.0.1:1")
	if !adapter.TranscribeTimestamps() || !adapter.TranscribeTranslate() {
		t.Error("the defaults must stay true")
	}
	if !adapter.Supports(providers.CapChat) || adapter.Supports(providers.CapEmbed) {
		t.Error("Anthropic answers chat and nothing else")
	}
}

func TestAnthropicChatRequestIsForwardedAsJSON(t *testing.T) {
	// The canonical request's own fields (stop, top_p) travel as Anthropic names.
	upstream := newChatProvider(t, func(w http.ResponseWriter, r *http.Request, call chatCall) {
		chatJSON(w, http.StatusOK, `{"id":"msg_2","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	adapter := anthropicAdapter(t, upstream.url())
	request := chatRequest(t, `{"messages":[{"role":"user","content":"hi"}],"stop":["a","b"],"top_p":0.5,"temperature":0.9}`)
	if _, err := adapter.Chat(context.Background(), request, providers.Call{ModelID: "claude-x"}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	body := upstream.last(t).body
	sequences, _ := body["stop_sequences"].([]any)
	if len(sequences) != 2 || sequences[0] != "a" {
		t.Errorf("stop_sequences = %v", body["stop_sequences"])
	}
	if body["top_p"] != 0.5 || body["temperature"] != 0.9 {
		t.Errorf("sampling was not forwarded: %v", body)
	}
	encoded, _ := json.Marshal(body["messages"])
	if string(encoded) != `[{"content":[{"text":"hi","type":"text"}],"role":"user"}]` && string(encoded) != `[{"role":"user","content":[{"type":"text","text":"hi"}]}]` {
		t.Errorf("messages = %s", encoded)
	}
}
