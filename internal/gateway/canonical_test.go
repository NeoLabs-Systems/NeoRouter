package gateway

// Tests of the canonical chat format.

import (
	"testing"
)

func TestNormalizeRequestKeepsTheFieldsThatMatter(t *testing.T) {
	body := parseBody(t, `{
		"model": "auto",
		"messages": [{"role": "user", "content": "hi"}],
		"stream": true,
		"tools": [{"type": "function", "function": {"name": "f"}}],
		"max_completion_tokens": 40,
		"max_tokens": 90,
		"temperature": 0.2,
		"top_p": null,
		"seed": 7,
		"n": 2,
		"response_format": {"type": "json_object"},
		"tool_choice": "auto",
		"unknown_field": "dropped"
	}`)
	request := NormalizeRequest(body)
	keys := request.Keys()
	want := []string{"messages", "stream", "tools", "max_tokens", "temperature", "seed", "n", "response_format", "tool_choice"}
	if len(keys) != len(want) {
		t.Fatalf("keys = %v", keys)
	}
	for index := range want {
		if keys[index] != want[index] {
			t.Fatalf("key %d = %q, want %q (%v)", index, keys[index], want[index], keys)
		}
	}
	if maxTokens, _ := getNumber(request, "max_tokens"); maxTokens != 40 {
		t.Errorf("max_tokens = %v", maxTokens)
	}
	if stream := getBool(request, "stream"); !stream {
		t.Error("stream was dropped")
	}
	if _, ok := request.Get("top_p"); ok {
		t.Error("a null field was copied")
	}
	if _, ok := request.Get("model"); ok {
		t.Error("model is not part of the canonical request")
	}
}

func TestNormalizeRequestDefaults(t *testing.T) {
	request := NormalizeRequest(parseBody(t, `{}`))
	messages := getList(request, "messages")
	if messages == nil || len(messages) != 0 {
		t.Errorf("messages = %v", getValue(request, "messages"))
	}
	if stream := getBool(request, "stream"); stream {
		t.Error("stream defaults to true")
	}
	if _, ok := request.Get("tools"); ok {
		t.Error("empty tools were copied")
	}
}

func TestRequirements(t *testing.T) {
	vision := Requirements(parseBody(t, `{"messages": [{"role": "user", "content": [
		{"type": "text", "text": "what is this"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,AA"}}]}]}`))
	if !vision.Vision {
		t.Error("vision not detected")
	}
	plain := Requirements(parseBody(t, `{"messages": [{"role": "user", "content": "hi"}]}`))
	if plain.Vision || plain.Tools || plain.JSON || plain.Stream {
		t.Errorf("plain needs = %+v", plain)
	}
	json := Requirements(parseBody(t, `{"messages": [], "response_format": {"type": "json_object"}, "tools": [{"type": "function"}], "stream": true}`))
	if !json.JSON || !json.Tools || !json.Stream {
		t.Errorf("needs = %+v", json)
	}
}

func TestEstimateInputTokens(t *testing.T) {
	request := parseBody(t, `{"messages": [{"role": "user", "content": "aaaa"}, {"role": "user", "content": "bbbb"}]}`)
	estimate := EstimateInputTokens(request)
	// 8 characters are 2 tokens, plus four tokens per message.
	if estimate != 2+2*4 {
		t.Errorf("estimate = %d", estimate)
	}
	withImage := parseBody(t, `{"messages": [{"role": "user", "content": [{"type": "image_url", "image_url": {"url": "x"}}]}]}`)
	if estimate := EstimateInputTokens(withImage); estimate < 850 {
		t.Errorf("image estimate = %d", estimate)
	}
}

func TestNormalizeUsage(t *testing.T) {
	openai := NormalizeUsage(parseBody(t, `{"prompt_tokens": 10, "completion_tokens": 4, "total_tokens": 14, "prompt_tokens_details": {"cached_tokens": 3}}`))
	if prompt, _ := getNumber(openai, "prompt_tokens"); prompt != 10 {
		t.Errorf("prompt_tokens = %v", prompt)
	}
	if cached, _ := getNumber(getObj(openai, "prompt_tokens_details"), "cached_tokens"); cached != 3 {
		t.Errorf("cached_tokens = %v", cached)
	}
	anthropic := NormalizeUsage(parseBody(t, `{"input_tokens": 5, "output_tokens": 2, "cache_read_input_tokens": 2, "cache_creation_input_tokens": 1}`))
	if prompt, _ := getNumber(anthropic, "prompt_tokens"); prompt != 8 {
		t.Errorf("prompt_tokens = %v", prompt)
	}
	if total, _ := getNumber(anthropic, "total_tokens"); total != 10 {
		t.Errorf("total_tokens = %v", total)
	}
	if creation, _ := getNumber(anthropic, "cache_creation_input_tokens"); creation != 1 {
		t.Errorf("cache_creation_input_tokens = %v", creation)
	}
	if cached, _ := getNumber(getObj(anthropic, "prompt_tokens_details"), "cached_tokens"); cached != 2 {
		t.Errorf("cached_tokens = %v", cached)
	}
	if NormalizeUsage(nil) != nil {
		t.Error("nil usage is not nil")
	}
	oversized := NormalizeUsage(parseBody(t, `{"prompt_tokens": 4, "completion_tokens": 1, "prompt_tokens_details": {"cached_tokens": 99}}`))
	if cached, _ := getNumber(getObj(oversized, "prompt_tokens_details"), "cached_tokens"); cached != 4 {
		t.Errorf("clamped cached_tokens = %v", cached)
	}
}

func TestReportedCost(t *testing.T) {
	cost := ReportedCost(parseBody(t, `{"cost": 0.25}`))
	if cost == nil || *cost != 0.25 {
		t.Errorf("cost = %v", cost)
	}
	total := ReportedCost(parseBody(t, `{"total_cost": 1.5}`))
	if total == nil || *total != 1.5 {
		t.Errorf("total = %v", total)
	}
	if ReportedCost(parseBody(t, `{"prompt_tokens": 1}`)) != nil {
		t.Error("a usage block without a cost reported one")
	}
}

func TestAccumulatorFoldsChunks(t *testing.T) {
	accumulator := &Accumulator{}
	accumulator.Push(parseBody(t, `{"id": "c1", "model": "m", "choices": [{"delta": {"role": "assistant", "content": ""}}]}`))
	accumulator.Push(parseBody(t, `{"id": "c1", "model": "m", "choices": [{"delta": {"content": "hi ", "reasoning_content": "think"}}]}`))
	accumulator.Push(parseBody(t, `{"id": "c1", "model": "m", "choices": [{"delta": {"tool_calls": [{"index": 0, "id": "call_1", "function": {"name": "look", "arguments": "{\"q\""}}]}}]}`))
	accumulator.Push(parseBody(t, `{"id": "c1", "model": "m", "choices": [{"delta": {"tool_calls": [{"index": 0, "function": {"arguments": ":1}"}}]}, "finish_reason": "tool_calls"}]}`))
	accumulator.Push(parseBody(t, `{"id": "c1", "model": "m", "choices": [], "usage": {"prompt_tokens": 3, "completion_tokens": 2}}`))
	if !accumulator.HasAnswer() {
		t.Error("no answer")
	}
	if accumulator.Content != "hi " || accumulator.Reasoning != "think" {
		t.Errorf("content = %q reasoning = %q", accumulator.Content, accumulator.Reasoning)
	}
	if accumulator.FinishReason != "tool_calls" {
		t.Errorf("finish = %q", accumulator.FinishReason)
	}
	if accumulator.ID != "c1" || accumulator.Model != "m" {
		t.Errorf("id = %q model = %q", accumulator.ID, accumulator.Model)
	}
	if accumulator.Usage == nil {
		t.Fatal("no usage")
	}
	message := accumulator.ToMessage()
	if role := getString(message, "role"); role != "assistant" {
		t.Errorf("role = %q", role)
	}
	calls := getList(message, "tool_calls")
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d", len(calls))
	}
	if args := getString(getObj(asObj(calls[0]), "function"), "arguments"); args != `{"q":1}` {
		t.Errorf("arguments = %q", args)
	}
	if reasoning := getString(message, "reasoning_content"); reasoning != "think" {
		t.Errorf("reasoning_content = %q", reasoning)
	}
}

func TestAccumulatorWithoutAnswer(t *testing.T) {
	accumulator := &Accumulator{}
	accumulator.Push(parseBody(t, `{"choices": [{"delta": {"reasoning_content": "thinking"}}]}`))
	if accumulator.HasAnswer() {
		t.Error("reasoning alone counted as an answer")
	}
	message := accumulator.ToMessage()
	if content, ok := message.Get("content"); !ok || content != "" {
		t.Errorf("content = %v", content)
	}
}

func TestInvalidToolCalls(t *testing.T) {
	if !InvalidToolCalls([]any{parseBody(t, `{"function": {"arguments": "{}"}}`)}) {
		t.Error("a call without a name is valid")
	}
	if InvalidToolCalls([]any{parseBody(t, `{"function": {"name": "f", "arguments": ""}}`)}) {
		t.Error("empty arguments are invalid")
	}
	if !InvalidToolCalls([]any{parseBody(t, `{"function": {"name": "f", "arguments": "{oops"}}`)}) {
		t.Error("bad JSON arguments are valid")
	}
	if InvalidToolCalls([]any{parseBody(t, `{"function": {"name": "f", "arguments": "{\"a\":1}"}}`)}) {
		t.Error("valid arguments are invalid")
	}
}

func TestBuildResponseAndChunk(t *testing.T) {
	response := BuildResponse(ResponseOptions{Model: "m", Message: newObjOf("role", "assistant", "content", "hi")})
	keys := response.Keys()
	want := []string{"id", "object", "created", "model", "choices"}
	for index := range want {
		if keys[index] != want[index] {
			t.Fatalf("keys = %v", keys)
		}
	}
	if got := getString(response, "object"); got != "chat.completion" {
		t.Errorf("object = %q", got)
	}
	choice := asObj(firstItem(getList(response, "choices")))
	if reason := getString(choice, "finish_reason"); reason != "stop" {
		t.Errorf("finish_reason = %q", reason)
	}
	chunk := BuildChunk("c1", "m", 1, nil, nil, nil)
	if got := getString(chunk, "object"); got != "chat.completion.chunk" {
		t.Errorf("object = %q", got)
	}
	chunkKeys := chunk.Keys()
	if chunkKeys[len(chunkKeys)-1] != "choices" {
		t.Errorf("chunk keys = %v", chunkKeys)
	}
	if _, ok := chunk.Get("usage"); ok {
		t.Error("a chunk without usage carries one")
	}
	withUsage := BuildChunk("c1", "m", 1, nil, "stop", newObjOf("prompt_tokens", float64(1)))
	if _, ok := withUsage.Get("usage"); !ok {
		t.Error("usage was dropped")
	}
}
