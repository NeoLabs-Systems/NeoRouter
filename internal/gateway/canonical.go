package gateway

// The canonical chat format. It is the OpenAI Chat Completions wire format, so
// most upstreams need no translation and the Anthropic/Responses translators
// only map to one shape.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode/utf16"
)

// passthroughFields are the request fields every dialect sends upstream as they
// arrived.
var passthroughFields = []string{
	"prompt_cache_key",
	"temperature", "top_p", "stop", "seed", "presence_penalty", "frequency_penalty", "n", "logit_bias",
	"logprobs", "top_logprobs", "user", "parallel_tool_calls", "reasoning_effort", "response_format", "tool_choice",
}

// imagePartTypes are the content parts that make a request need a vision model.
var imagePartTypes = map[string]bool{"image_url": true, "input_image": true, "image": true}

// CompletionID is completionId: "chatcmpl-" and 24 hex characters.
func CompletionID() string { return "chatcmpl-" + randomHex(12) }

// randomHex is a random lowercase hex string of n bytes.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(buf)
}

// NormalizeRequest is normalizeRequest: the OpenAI body reduced to the fields
// the router and the adapters act on, in the canonical order.
func NormalizeRequest(body *Obj) *Obj {
	request := newObj()
	messages := getList(body, "messages")
	if messages == nil {
		messages = []any{}
	}
	request.Set("messages", messages)
	request.Set("stream", getBool(body, "stream"))
	if tools := getList(body, "tools"); len(tools) > 0 {
		request.Set("tools", tools)
	}
	if maxTokens, ok := getNumber(body, "max_completion_tokens"); ok {
		request.Set("max_tokens", maxTokens)
	} else if maxTokens, ok := getNumber(body, "max_tokens"); ok {
		request.Set("max_tokens", maxTokens)
	}
	for _, field := range passthroughFields {
		if value, ok := get(body, field); ok {
			request.Set(field, value)
		}
	}
	return request
}

// ContentParts is contentParts: a string becomes one text part, an array stays
// as it is, anything else is no content.
func ContentParts(content any) []any {
	if text, ok := content.(string); ok {
		part := newObj()
		part.Set("type", "text")
		part.Set("text", text)
		return []any{part}
	}
	return asList(content)
}

// TextOf is textOf: the text parts of a content value, joined.
func TextOf(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	var out strings.Builder
	for _, item := range ContentParts(content) {
		part := asObj(item)
		if getString(part, "type") != "text" {
			continue
		}
		out.WriteString(getString(part, "text"))
	}
	return out.String()
}

// Needs is what a request needs from a model; routing uses it as hard filters.
type Needs struct {
	Tools  bool
	Vision bool
	JSON   bool
	Stream bool
}

// Requirements is requirements: the needs of a canonical request.
func Requirements(request *Obj) Needs {
	needs := Needs{
		Tools:  len(getList(request, "tools")) > 0,
		Stream: getBool(request, "stream"),
	}
	for _, item := range getList(request, "messages") {
		message := asObj(item)
		for _, part := range ContentParts(getValue(message, "content")) {
			if imagePartTypes[getString(asObj(part), "type")] {
				needs.Vision = true
			}
		}
		if needs.Vision {
			break
		}
	}
	if format := getString(getObj(request, "response_format"), "type"); format == "json_object" || format == "json_schema" {
		needs.JSON = true
	}
	return needs
}

// getValue reads a key including an explicit null.
func getValue(object *Obj, key string) any {
	if object == nil {
		return nil
	}
	value, _ := object.Get(key)
	return value
}

// EstimateInputTokens is estimateInputTokens: a rough token estimate (about
// four characters per token, images counted flat). Routing and budget
// reservations use it; billing uses the provider's own usage block.
func EstimateInputTokens(request *Obj) int {
	messages := getList(request, "messages")
	chars := 0
	images := 0
	for _, item := range messages {
		message := asObj(item)
		for _, part := range ContentParts(getValue(message, "content")) {
			partObj := asObj(part)
			if getString(partObj, "type") == "text" {
				chars += utf16Len(getString(partObj, "text"))
				continue
			}
			images++
		}
		if calls, ok := get(message, "tool_calls"); ok {
			chars += utf16Len(marshal(calls))
		}
	}
	if tools, ok := get(request, "tools"); ok {
		chars += utf16Len(marshal(tools))
	}
	return (chars+3)/4 + images*850 + len(messages)*4
}

// utf16Len is the length in UTF-16 code units.
func utf16Len(text string) int {
	for _, r := range text {
		if r > 0xFFFF {
			return len(utf16.Encode([]rune(text)))
		}
	}
	return len(text)
}

// NormalizeUsage is normalizeUsage: every provider's usage block reduced to the
// OpenAI one, with the cached-token count folded into the details.
func NormalizeUsage(usage any) *Obj {
	block := asObj(usage)
	if block == nil {
		return nil
	}
	// cached = prompt_tokens_details?.cached_tokens ?? input_tokens_details?.cached_tokens
	//   ?? cache_read_input_tokens ?? 0
	cached := 0.0
	if value, ok := getNumber(getObj(block, "prompt_tokens_details"), "cached_tokens"); ok {
		cached = value
	} else if value, ok := getNumber(getObj(block, "input_tokens_details"), "cached_tokens"); ok {
		cached = value
	} else if value, ok := getNumber(block, "cache_read_input_tokens"); ok {
		cached = value
	}
	creation := 0.0
	if value, ok := getNumber(block, "cache_creation_input_tokens"); ok {
		creation = value
	}
	input := 0.0
	if value, ok := getNumber(block, "prompt_tokens"); ok {
		input = value
	} else {
		base, _ := getNumber(block, "input_tokens")
		cacheRead, _ := getNumber(block, "cache_read_input_tokens")
		input = base + cacheRead + creation
	}
	output := 0.0
	if value, ok := getNumber(block, "completion_tokens"); ok {
		output = value
	} else if value, ok := getNumber(block, "output_tokens"); ok {
		output = value
	}
	total := input + output
	if value, ok := getNumber(block, "total_tokens"); ok {
		total = value
	}
	details := newObj()
	if existing := getObj(block, "prompt_tokens_details"); existing != nil {
		details = existing.Clone()
	}
	details.Set("cached_tokens", math.Max(0, math.Min(input, cached)))
	out := newObj()
	out.Set("prompt_tokens", input)
	out.Set("completion_tokens", output)
	out.Set("total_tokens", total)
	out.Set("prompt_tokens_details", details)
	out.Set("cache_creation_input_tokens", creation)
	if completionDetails := getValue(block, "completion_tokens_details"); truthy(completionDetails) {
		out.Set("completion_tokens_details", completionDetails)
	}
	return out
}

// ReportedCost is reportedCost: the charge a provider reports in its usage
// block (OpenRouter, Kilo, …) in USD, nil when it reports none.
func ReportedCost(usage *Obj) *float64 {
	if usage == nil {
		return nil
	}
	value := getValue(usage, "cost")
	if value == nil {
		value = getValue(usage, "total_cost")
	}
	cost, ok := number(value)
	if !ok {
		return nil
	}
	return &cost
}

// ResponseOptions are buildResponse's arguments.
type ResponseOptions struct {
	ID           string
	Model        string
	Message      *Obj
	FinishReason string
	Usage        *Obj
	Created      int64
}

// BuildResponse is buildResponse: one canonical chat completion.
func BuildResponse(opts ResponseOptions) *Obj {
	id := opts.ID
	if id == "" {
		id = CompletionID()
	}
	finish := opts.FinishReason
	if finish == "" {
		finish = "stop"
	}
	created := opts.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	choice := newObj()
	choice.Set("index", 0)
	choice.Set("message", opts.Message)
	choice.Set("finish_reason", finish)
	out := newObj()
	out.Set("id", id)
	out.Set("object", "chat.completion")
	out.Set("created", created)
	out.Set("model", opts.Model)
	out.Set("choices", []any{choice})
	if opts.Usage != nil {
		out.Set("usage", opts.Usage)
	}
	return out
}

// BuildChunk is buildChunk: one canonical stream chunk.
func BuildChunk(id, model string, created int64, delta *Obj, finishReason any, usage *Obj) *Obj {
	if delta == nil {
		delta = newObj()
	}
	choice := newObj()
	choice.Set("index", 0)
	choice.Set("delta", delta)
	choice.Set("finish_reason", finishReason)
	chunk := newObj()
	chunk.Set("id", id)
	chunk.Set("object", "chat.completion.chunk")
	chunk.Set("created", created)
	chunk.Set("model", model)
	chunk.Set("choices", []any{choice})
	if usage != nil {
		chunk.Set("usage", usage)
	}
	return chunk
}

// Accumulator folds a stream of canonical chunks into a full response, to
// validate streams (empty answers, truncated tool JSON) and to record usage.
type Accumulator struct {
	Content      string
	Reasoning    string
	ToolCalls    []*Obj
	FinishReason string
	Usage        *Obj
	ReportedCost *float64
	Chunks       int
	ID           string
	Model        string
}

// Push folds one chunk in, as StreamAccumulator.push does.
func (a *Accumulator) Push(chunk *Obj) {
	if a == nil || chunk == nil {
		return
	}
	a.Chunks++
	if a.ID == "" {
		a.ID = getString(chunk, "id")
	}
	if a.Model == "" {
		a.Model = getString(chunk, "model")
	}
	if usage := getValue(chunk, "usage"); truthy(usage) {
		a.Usage = NormalizeUsage(usage)
		a.ReportedCost = ReportedCost(asObj(usage))
	}
	for _, item := range getList(chunk, "choices") {
		choice := asObj(item)
		delta := getObj(choice, "delta")
		if content, ok := get(delta, "content"); ok {
			if text, isText := content.(string); isText {
				a.Content += text
			}
		}
		if reasoning := getString(delta, "reasoning_content"); reasoning != "" {
			a.Reasoning += reasoning
		}
		if reasoning, ok := get(delta, "reasoning"); ok {
			if text, isText := reasoning.(string); isText {
				a.Reasoning += text
			}
		}
		for _, rawCall := range getList(delta, "tool_calls") {
			call := asObj(rawCall)
			index := len(a.ToolCalls)
			if position, ok := getNumber(call, "index"); ok {
				index = int(position)
			}
			target := a.toolCallAt(index)
			if id := getString(call, "id"); id != "" {
				target.Set("id", id)
			}
			function := getObj(call, "function")
			if name := getString(function, "name"); name != "" {
				targetFunction := getObj(target, "function")
				targetFunction.Set("name", getString(targetFunction, "name")+name)
			}
			if args := getString(function, "arguments"); args != "" {
				targetFunction := getObj(target, "function")
				targetFunction.Set("arguments", getString(targetFunction, "arguments")+args)
			}
		}
		if finish := getString(choice, "finish_reason"); finish != "" {
			a.FinishReason = finish
		}
	}
}

// toolCallAt returns the accumulated tool call at index, creating it.
func (a *Accumulator) toolCallAt(index int) *Obj {
	for len(a.ToolCalls) <= index {
		call := newObj()
		call.Set("id", "")
		call.Set("type", "function")
		function := newObj()
		function.Set("name", "")
		function.Set("arguments", "")
		call.Set("function", function)
		a.ToolCalls = append(a.ToolCalls, call)
	}
	return a.ToolCalls[index]
}

// HasAnswer is hasAnswer: a real answer or a tool call arrived.
func (a *Accumulator) HasAnswer() bool {
	return strings.TrimSpace(a.Content) != "" || len(a.ToolCalls) > 0
}

// ToMessage is toMessage: the accumulated answer as a canonical message.
func (a *Accumulator) ToMessage() *Obj {
	message := newObj()
	message.Set("role", "assistant")
	if a.Content != "" || len(a.ToolCalls) == 0 {
		message.Set("content", a.Content)
	} else {
		message.Set("content", nil)
	}
	if len(a.ToolCalls) > 0 {
		calls := make([]any, 0, len(a.ToolCalls))
		for _, call := range a.ToolCalls {
			if call != nil {
				calls = append(calls, call)
			}
		}
		message.Set("tool_calls", calls)
	}
	if a.Reasoning != "" {
		message.Set("reasoning_content", a.Reasoning)
	}
	return message
}

// EstimateOutputTokens is estimateOutputTokens: the tokens an accumulated
// stream produced, when the provider reported no usage block.
func EstimateOutputTokens(a *Accumulator) int {
	chars := utf16Len(a.Content) + utf16Len(a.Reasoning)
	if len(a.ToolCalls) > 0 {
		calls := make([]any, 0, len(a.ToolCalls))
		for _, call := range a.ToolCalls {
			calls = append(calls, call)
		}
		chars += utf16Len(marshal(calls))
	}
	return (chars + 3) / 4
}

// InvalidToolCalls is invalidToolCalls: a call without a name, or with
// arguments that are not valid JSON, is malformed.
func InvalidToolCalls(toolCalls []any) bool {
	for _, item := range toolCalls {
		call := asObj(item)
		if getString(getObj(call, "function"), "name") == "" {
			return true
		}
		args := getString(getObj(call, "function"), "arguments")
		if args == "" {
			continue
		}
		if !json.Valid([]byte(args)) {
			return true
		}
	}
	return false
}

// invalidAccumulatedCalls is invalidToolCalls over the accumulator's calls.
func invalidAccumulatedCalls(a *Accumulator) bool {
	calls := make([]any, 0, len(a.ToolCalls))
	for _, call := range a.ToolCalls {
		calls = append(calls, call)
	}
	return InvalidToolCalls(calls)
}
