package gateway

// The Anthropic and Responses translators, in and out, plain and streaming.

import (
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// chunkList builds a ChunkStream from ordered objects.
func chunkList(chunks ...*Obj) ChunkStream {
	return StreamFromChunks(chunks...)
}

// parseBody is a request body from JSON, keeping key order.
func parseBody(t *testing.T, text string) *Obj {
	t.Helper()
	body, err := parseObject([]byte(text))
	if err != nil {
		t.Fatalf("parse body: %v", err)
	}
	return body
}

func TestAnthropicRequestWithToolsAndResults(t *testing.T) {
	canonical, err := AnthropicTranslator{}.ToCanonical(parseBody(t, `{
		"system": [{"type": "text", "text": "sys"}],
		"max_tokens": 100,
		"tools": [{"name": "get", "input_schema": {"type": "object"}}],
		"tool_choice": {"type": "any"},
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": [{"type": "text", "text": "calling"}, {"type": "tool_use", "id": "t1", "name": "get", "input": {"a": 1}}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "t1", "content": "result"}, {"type": "text", "text": "thanks"}]}
		]
	}`))
	if err != nil {
		t.Fatalf("toCanonical: %v", err)
	}
	messages := getList(canonical, "messages")
	if len(messages) != 5 {
		t.Fatalf("messages: %d", len(messages))
	}
	if role := getString(asObj(messages[0]), "role"); role != "system" {
		t.Errorf("first role = %q", role)
	}
	assistant := asObj(messages[2])
	if args := getString(getObj(asObj(firstItem(getList(assistant, "tool_calls"))), "function"), "arguments"); args != `{"a":1}` {
		t.Errorf("tool call arguments = %q", args)
	}
	tool := asObj(messages[3])
	if getString(tool, "role") != "tool" || getString(tool, "tool_call_id") != "t1" || getString(tool, "content") != "result" {
		t.Errorf("tool message = %s", marshal(tool))
	}
	if content := getString(asObj(messages[4]), "content"); content != "thanks" {
		t.Errorf("last content = %q", content)
	}
	if choice := getString(canonical, "tool_choice"); choice != "required" {
		t.Errorf("tool_choice = %q", choice)
	}
}

func TestCanonicalRequestMapsBackToAnthropic(t *testing.T) {
	request := parseBody(t, `{
		"messages": [
			{"role": "system", "content": "sys"},
			{"role": "user", "content": [{"type": "text", "text": "look"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}}]},
			{"role": "assistant", "content": null, "tool_calls": [{"id": "t1", "type": "function", "function": {"name": "get", "arguments": "{\"a\":1}"}}]},
			{"role": "tool", "tool_call_id": "t1", "content": "ok"}
		],
		"tools": [{"type": "function", "function": {"name": "get", "parameters": {"type": "object"}}}]
	}`)
	body := AnthropicTranslator{}.FromCanonical(request, 0)
	system := getList(body, "system")
	if len(system) != 1 {
		t.Fatalf("system blocks = %d", len(system))
	}
	block := asObj(system[0])
	if getString(block, "text") != "sys" || getString(getObj(block, "cache_control"), "type") != "ephemeral" {
		t.Errorf("system block = %s", marshal(block))
	}
	messages := getList(body, "messages")
	imagePart := asObj(getList(asObj(messages[0]), "content")[1])
	if media := getString(getObj(imagePart, "source"), "media_type"); media != "image/png" {
		t.Errorf("image media type = %q", media)
	}
	if kind := getString(asObj(getList(asObj(messages[1]), "content")[0]), "type"); kind != "tool_use" {
		t.Errorf("assistant block = %q", kind)
	}
	if kind := getString(asObj(getList(asObj(messages[2]), "content")[0]), "type"); kind != "tool_result" {
		t.Errorf("tool block = %q", kind)
	}
	if maxTokens, _ := getNumber(body, "max_tokens"); maxTokens != 4096 {
		t.Errorf("max_tokens = %v", maxTokens)
	}
}

func TestCanonicalStreamBecomesAnthropicEvents(t *testing.T) {
	chunks := chunkList(
		newObjOf("choices", []any{newObjOf("delta", newObjOf("role", "assistant", "content", "Hel"))}),
		newObjOf("choices", []any{newObjOf("delta", newObjOf("content", "lo"))}),
		newObjOf("choices", []any{newObjOf("delta", newObjOf("tool_calls", []any{newObjOf("index", float64(0), "id", "c1", "function", newObjOf("name", "f", "arguments", `{"x"`))}))}),
		newObjOf("choices", []any{newObjOf("delta", newObjOf("tool_calls", []any{newObjOf("index", float64(0), "function", newObjOf("arguments", `:1}`))}))}),
		newObjOf("choices", []any{newObjOf("delta", newObj(), "finish_reason", "tool_calls")}),
		newObjOf("choices", []any{}, "usage", newObjOf("prompt_tokens", float64(5), "completion_tokens", float64(3))),
	)
	events, err := collectEvents(func(yield func(Event) bool) error {
		return AnthropicTranslator{}.StreamFromCanonical(chunks, "m", yield)
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Event)
	}
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if len(types) != len(want) {
		t.Fatalf("events = %v", types)
	}
	for index := range want {
		if types[index] != want[index] {
			t.Fatalf("event %d = %q, want %q (%v)", index, types[index], want[index], types)
		}
	}
	deltaEvent := asObj(events[9].Data)
	if stop := getString(getObj(deltaEvent, "delta"), "stop_reason"); stop != "tool_use" {
		t.Errorf("stop_reason = %q", stop)
	}
	if output, _ := getNumber(getObj(deltaEvent, "usage"), "output_tokens"); output != 3 {
		t.Errorf("output_tokens = %v", output)
	}
}

func TestAnthropicUpstreamStreamBecomesCanonicalChunks(t *testing.T) {
	raw := []UpstreamEvent{
		{Event: "message_start", Data: `{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":7}}}`},
		{Event: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`},
		{Event: "content_block_start", Data: `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu","name":"f"}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`},
		{Event: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`},
		{Event: "message_stop", Data: `{"type":"message_stop"}`},
	}
	stream := AnthropicTranslator{}.StreamToCanonical(func(yield func(UpstreamEvent) bool) {
		for _, event := range raw {
			if !yield(event) {
				return
			}
		}
	}, "claude", nil)
	out, err := CollectChunks(stream)
	if err != nil {
		t.Fatalf("streamToCanonical: %v", err)
	}
	if len(out) != 5 {
		t.Fatalf("chunks = %d", len(out))
	}
	if content := getString(getObj(asObj(firstItem(getList(out[1], "choices"))), "delta"), "content"); content != "Hi" {
		t.Errorf("content = %q", content)
	}
	toolCalls := getList(getObj(asObj(firstItem(getList(out[2], "choices"))), "delta"), "tool_calls")
	if id := getString(asObj(firstItem(toolCalls)), "id"); id != "tu" {
		t.Errorf("tool call id = %q", id)
	}
	last := out[len(out)-1]
	if finish := getString(asObj(firstItem(getList(last, "choices"))), "finish_reason"); finish != "tool_calls" {
		t.Errorf("finish_reason = %q", finish)
	}
	usage := getObj(last, "usage")
	if prompt, _ := getNumber(usage, "prompt_tokens"); prompt != 7 {
		t.Errorf("prompt_tokens = %v", prompt)
	}
	if cached, _ := getNumber(getObj(usage, "prompt_tokens_details"), "cached_tokens"); cached != 0 {
		t.Errorf("cached_tokens = %v", cached)
	}
	if total, _ := getNumber(usage, "total_tokens"); total != 11 {
		t.Errorf("total_tokens = %v", total)
	}
}

func TestResponsesInputItemsMapToCanonical(t *testing.T) {
	canonical, err := ResponsesTranslator{}.ToCanonical(parseBody(t, `{
		"instructions": "sys",
		"input": [
			{"role": "user", "content": [{"type": "input_text", "text": "hi"}]},
			{"type": "function_call", "call_id": "c1", "name": "f", "arguments": "{}"},
			{"type": "function_call_output", "call_id": "c1", "output": "done"}
		],
		"tools": [{"type": "function", "name": "f", "parameters": {"type": "object"}}, {"type": "web_search"}],
		"max_output_tokens": 20,
		"text": {"format": {"type": "json_schema", "name": "x", "schema": {"type": "object"}}}
	}`))
	if err != nil {
		t.Fatalf("toCanonical: %v", err)
	}
	messages := getList(canonical, "messages")
	if len(messages) != 4 {
		t.Fatalf("messages = %d", len(messages))
	}
	if id := getString(asObj(firstItem(getList(asObj(messages[2]), "tool_calls"))), "id"); id != "c1" {
		t.Errorf("call id = %q", id)
	}
	if role := getString(asObj(messages[3]), "role"); role != "tool" {
		t.Errorf("role = %q", role)
	}
	if tools := getList(canonical, "tools"); len(tools) != 1 {
		t.Errorf("tools = %d", len(tools))
	}
	if maxTokens, _ := getNumber(canonical, "max_tokens"); maxTokens != 20 {
		t.Errorf("max_tokens = %v", maxTokens)
	}
	if kind := getString(getObj(canonical, "response_format"), "type"); kind != "json_schema" {
		t.Errorf("response_format = %q", kind)
	}
}

func TestPreviousResponseIDIsRejected(t *testing.T) {
	_, err := ResponsesTranslator{}.ToCanonical(parseBody(t, `{"input": "hi", "previous_response_id": "resp_1"}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	problem := errorOf(err)
	if problem.code != "UNSUPPORTED_PARAMETER" || problem.status != 400 {
		t.Errorf("error = %+v", problem)
	}
	if !strings.Contains(err.Error(), "previous_response_id") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestResponsesStreamEmitsFunctionCallEvents(t *testing.T) {
	chunks := chunkList(
		newObjOf("choices", []any{newObjOf("delta", newObjOf("content", "a"))}),
		newObjOf("choices", []any{newObjOf("delta", newObjOf("tool_calls", []any{newObjOf("index", float64(0), "id", "c1", "function", newObjOf("name", "f", "arguments", "{}"))}))}),
		newObjOf("choices", []any{newObjOf("delta", newObj(), "finish_reason", "tool_calls")}, "usage", newObjOf("prompt_tokens", float64(1), "completion_tokens", float64(2))),
	)
	events, err := collectEvents(func(yield func(Event) bool) error {
		return ResponsesTranslator{}.StreamFromCanonical(chunks, "m", yield)
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Event == "response.function_call_arguments.done" {
			found = true
		}
	}
	if !found {
		t.Error("no function call done event")
	}
	last := asObj(events[len(events)-1].Data)
	if getString(last, "type") != "response.completed" {
		t.Errorf("last event = %q", getString(last, "type"))
	}
	response := getObj(last, "response")
	if output := getList(response, "output"); len(output) != 2 {
		t.Errorf("output = %d", len(output))
	}
	if outputTokens, _ := getNumber(getObj(response, "usage"), "output_tokens"); outputTokens != 2 {
		t.Errorf("output_tokens = %v", outputTokens)
	}
	for index, event := range events {
		sequence, _ := getNumber(asObj(event.Data), "sequence_number")
		if int(sequence) != index {
			t.Fatalf("sequence_number %v at %d", sequence, index)
		}
	}
}

func TestResponsesPlainAnswer(t *testing.T) {
	completion := BuildResponse(ResponseOptions{
		Model:   "m",
		Message: newObjOf("role", "assistant", "content", "hello"),
		Usage:   newObjOf("prompt_tokens", float64(3), "completion_tokens", float64(2), "total_tokens", float64(5)),
	})
	body := ResponsesTranslator{}.ResponseFromCanonical(completion, "mock/ok-alpha")
	if object := getString(body, "object"); object != "response" {
		t.Errorf("object = %q", object)
	}
	if text := getString(body, "output_text"); text != "hello" {
		t.Errorf("output_text = %q", text)
	}
	if input, _ := getNumber(getObj(body, "usage"), "input_tokens"); input != 3 {
		t.Errorf("input_tokens = %v", input)
	}
}

func TestChatTranslatorRelabelsTheModel(t *testing.T) {
	completion := BuildResponse(ResponseOptions{
		Model:   "upstream",
		Message: newObjOf("role", "assistant", "content", "hi"),
	})
	body := ChatTranslator{}.ResponseFromCanonical(completion, "mock/ok-alpha")
	if model := getString(body, "model"); model != "mock/ok-alpha" {
		t.Errorf("model = %q", model)
	}
	if object := getString(body, "object"); object != "chat.completion" {
		t.Errorf("object = %q", object)
	}
}

func TestChatStreamEndsWithDone(t *testing.T) {
	chunks := chunkList(BuildChunk("chunk-1", "upstream", 1, newObjOf("content", "hi"), nil, nil))
	events, err := collectEvents(func(yield func(Event) bool) error {
		return ChatTranslator{}.StreamFromCanonical(chunks, "mock/ok-alpha", yield)
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	if data, ok := events[1].Data.(string); !ok || data != "[DONE]" {
		t.Errorf("last event = %v", events[1].Data)
	}
	if model := getString(asObj(events[0].Data), "model"); model != "mock/ok-alpha" {
		t.Errorf("model = %q", model)
	}
}

func TestAnthropicStreamErrorEndsTheStream(t *testing.T) {
	failure := providers.NewError(providers.KindServer, "The stream stalled.", providers.ErrorOptions{})
	stream := FailingStream(failure, BuildChunk("chunk-1", "m", 1, newObjOf("content", "hi"), nil, nil))
	_, err := collectEvents(func(yield func(Event) bool) error {
		return AnthropicTranslator{}.StreamFromCanonical(stream, "m", yield)
	})
	if err == nil {
		t.Fatal("expected the stream error")
	}
}

// StreamFromChunks is a ChunkStream over a fixed list of chunks.
func StreamFromChunks(chunks ...*Obj) ChunkStream {
	return func(yield func(*Obj, error) bool) {
		for _, chunk := range chunks {
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

// FailingStream is a ChunkStream of the chunks, then the error.
func FailingStream(err error, chunks ...*Obj) ChunkStream {
	return func(yield func(*Obj, error) bool) {
		for _, chunk := range chunks {
			if !yield(chunk, nil) {
				return
			}
		}
		yield(nil, err)
	}
}

// CollectChunks drains a ChunkStream into a list, stopping at the first error.
func CollectChunks(stream ChunkStream) ([]*Obj, error) {
	var out []*Obj
	for chunk, err := range stream {
		if err != nil {
			return out, err
		}
		out = append(out, chunk)
	}
	return out, nil
}

// collectEvents drains a translator's event stream, for tests and for callers
// that answer without a live connection.
func collectEvents(stream func(yield func(Event) bool) error) ([]Event, error) {
	var out []Event
	err := stream(func(event Event) bool {
		out = append(out, event)
		return true
	})
	return out, err
}
