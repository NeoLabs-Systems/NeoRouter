package adapters

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The canonical chat format is the OpenAI Chat Completions wire format. The
// adapters receive a canonical request and answer with a canonical completion
// or stream, so the helpers that read and build those bodies live here. Every
// name is chat-prefixed: the media adapters share this package.
//
// Bodies are kept as insertion-ordered objects (transport.Obj) rather than Go
// structs, because a canonical body carries every field the caller sent —
// logprobs, seed, logit_bias, prompt_cache_key — and a provider's answer carries
// fields NeoRouter does not read. Dropping or reordering them would change what
// clients see.

// chatObj reads a decoded JSON value as an ordered object, or nil when it is not
// one.
func chatObj(value any) *transport.Obj {
	switch v := value.(type) {
	case *transport.Obj:
		return v
	case map[string]any:
		return transport.FromMap(v)
	}
	return nil
}

// chatList reads a decoded JSON value as an array, or nil.
func chatList(value any) []any {
	items, _ := value.([]any)
	return items
}

// chatString is String(x) for the scalars a provider sends.
func chatString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// chatGet reads one member of an object, nil when it is absent.
func chatGet(obj *transport.Obj, name string) any {
	if obj == nil {
		return nil
	}
	value, _ := obj.Get(name)
	return value
}

// chatField is a typed string read of one object member ("" when absent or not
// a string).
func chatField(obj *transport.Obj, name string) string {
	if obj == nil {
		return ""
	}
	value, _ := obj.Get(name)
	text, _ := value.(string)
	return text
}

// chatNumber reads a member as a finite number.
func chatNumber(obj *transport.Obj, name string) (float64, bool) {
	if obj == nil {
		return 0, false
	}
	value, _ := obj.Get(name)
	return chatToNumber(value)
}

// chatToNumber converts a value to a number with a finiteness check.
func chatToNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, true
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// chatTrue is a definition override that asserts a capability.
func chatTrue() *bool { value := true; return &value }

// chatTruthy is truthiness of a decoded JSON value: only null, false, zero and
// "" are falsy.
func chatTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	}
	return true
}

// chatContentParts is canonical.contentParts: a message's content as parts.
func chatContentParts(content any) []*transport.Obj {
	if text, ok := content.(string); ok {
		part := transport.NewObj().Set("type", "text").Set("text", text)
		return []*transport.Obj{part}
	}
	var parts []*transport.Obj
	for _, item := range chatList(content) {
		if part := chatObj(item); part != nil {
			parts = append(parts, part)
		}
	}
	return parts
}

// chatTextOf is canonical.textOf: the text of a message's content, whatever its
// shape.
func chatTextOf(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	var builder strings.Builder
	for _, part := range chatContentParts(content) {
		if chatField(part, "type") == "text" {
			builder.WriteString(chatField(part, "text"))
		}
	}
	return builder.String()
}

// chatImageURL reads the URL of an image part, which is a string or an object.
func chatImageURL(part *transport.Obj) string {
	value, _ := part.Get("image_url")
	if text, ok := value.(string); ok {
		return text
	}
	return chatField(chatObj(value), "url")
}

// chatMessages is the canonical request's message list.
func chatMessages(request *transport.Obj) []*transport.Obj {
	var messages []*transport.Obj
	for _, item := range chatList(chatGet(request, "messages")) {
		if message := chatObj(item); message != nil {
			messages = append(messages, message)
		}
	}
	return messages
}

// chatCompletionID is a canonical completion id, "chatcmpl-" and 12 random
// bytes, as canonical.completionId makes it.
func chatCompletionID() string { return "chatcmpl-" + chatRandomHex(12) }

// chatCallID is a Gemini function-call id ("call_").
func chatCallID() string { return "call_" + chatRandomHex(8) }

// chatRandomHex is crypto.randomBytes(n).toString('hex').
func chatRandomHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		// A failure of the system random source is not survivable for ids; fall
		// back to a time-derived value rather than an empty one.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

// chatNowSeconds is Date.now()/1000 as the canonical `created` field carries it.
func chatNowSeconds() int64 { return time.Now().Unix() }

// chatNormalizeUsage is canonical.normalizeUsage: every provider's usage shape
// read into the OpenAI one, with the cached tokens clamped into the prompt.
func chatNormalizeUsage(usage any) *transport.Obj {
	source := chatObj(usage)
	if source == nil {
		return nil
	}
	cached, cachedKnown := chatNumberOrNil(chatNested(source, "prompt_tokens_details", "cached_tokens"))
	if !cachedKnown {
		cached, cachedKnown = chatNumberOrNil(chatNested(source, "input_tokens_details", "cached_tokens"))
	}
	if !cachedKnown {
		cached, cachedKnown = chatNumberOrNil(chatGet(source, "cache_read_input_tokens"))
	}
	if !cachedKnown {
		cached = 0
	}
	creation, creationKnown := chatNumberOrNil(chatGet(source, "cache_creation_input_tokens"))
	if !creationKnown {
		creation = 0
	}
	input, inputKnown := chatNumberOrNil(chatGet(source, "prompt_tokens"))
	if !inputKnown {
		raw, _ := chatNumberOrNil(chatGet(source, "input_tokens"))
		read, _ := chatNumberOrNil(chatGet(source, "cache_read_input_tokens"))
		input = raw + read + creation
	}
	output, outputKnown := chatNumberOrNil(chatGet(source, "completion_tokens"))
	if !outputKnown {
		output, _ = chatNumberOrNil(chatGet(source, "output_tokens"))
	}
	total, totalKnown := chatNumberOrNil(chatGet(source, "total_tokens"))
	if !totalKnown {
		total = input + output
	}
	details := chatObj(chatGet(source, "prompt_tokens_details"))
	if details == nil {
		details = transport.NewObj()
	}
	details.Set("cached_tokens", math.Max(0, math.Min(input, cached)))
	out := transport.NewObj().
		Set("prompt_tokens", chatCount(input)).
		Set("completion_tokens", chatCount(output)).
		Set("total_tokens", chatCount(total)).
		Set("prompt_tokens_details", details).
		Set("cache_creation_input_tokens", chatCount(creation))
	if completion, ok := source.Get("completion_tokens_details"); ok && completion != nil {
		out.Set("completion_tokens_details", completion)
	}
	return out
}

// chatNested reads obj[name][field].
func chatNested(obj *transport.Obj, name, field string) any {
	if obj == nil {
		return nil
	}
	return chatGet(chatObj(chatGet(obj, name)), field)
}

// chatNumberOrNil reads a value as a number, reporting whether it was one.
func chatNumberOrNil(value any) (float64, bool) {
	if value == nil {
		return 0, false
	}
	return chatToNumber(value)
}

// chatCount renders a token count: a whole number stays whole.
func chatCount(value float64) any {
	if value == math.Trunc(value) && math.Abs(value) < 1e15 {
		return int64(value)
	}
	return value
}

// chatBuildResponse is canonical.buildResponse: a whole completion.
func chatBuildResponse(id, model string, message *transport.Obj, finishReason string, usage *transport.Obj) *transport.Obj {
	if id == "" {
		id = chatCompletionID()
	}
	if finishReason == "" {
		finishReason = "stop"
	}
	choice := transport.NewObj().Set("index", 0).Set("message", message).Set("finish_reason", finishReason)
	body := transport.NewObj().
		Set("id", id).
		Set("object", "chat.completion").
		Set("created", chatNowSeconds()).
		Set("model", model).
		Set("choices", []any{choice})
	if usage != nil {
		body.Set("usage", usage)
	}
	return body
}

// chatBuildChunk is canonical.buildChunk: one streamed piece.
func chatBuildChunk(id, model string, created int64, delta *transport.Obj, finishReason any, usage *transport.Obj) *transport.Obj {
	if delta == nil {
		delta = transport.NewObj()
	}
	choice := transport.NewObj().Set("index", 0).Set("delta", delta).Set("finish_reason", finishReason)
	chunk := transport.NewObj().
		Set("id", id).
		Set("object", "chat.completion.chunk").
		Set("created", created).
		Set("model", model).
		Set("choices", []any{choice})
	if usage != nil {
		chunk.Set("usage", usage)
	}
	return chunk
}

// chatStreamAccumulator is canonical.StreamAccumulator: it folds a stream of
// canonical chunks back into one completion, which the dialects that only stream
// upstream need for a plain request.
type chatStreamAccumulator struct {
	content   strings.Builder
	reasoning strings.Builder
	toolCalls []*transport.Obj
	finish    any
	usage     *transport.Obj
}

// push folds one canonical chunk in.
func (s *chatStreamAccumulator) push(chunk *transport.Obj) {
	if chunk == nil {
		return
	}
	if usage := chatObj(chatGet(chunk, "usage")); usage != nil {
		s.usage = chatNormalizeUsage(usage)
	}
	for _, raw := range chatList(chatGet(chunk, "choices")) {
		choice := chatObj(raw)
		delta := chatObj(chatGet(choice, "delta"))
		if delta != nil {
			if text, ok := chatGet(delta, "content").(string); ok {
				s.content.WriteString(text)
			}
			for _, name := range []string{"reasoning_content", "reasoning"} {
				if text, ok := chatGet(delta, name).(string); ok {
					s.reasoning.WriteString(text)
				}
			}
			for _, rawCall := range chatList(chatGet(delta, "tool_calls")) {
				call := chatObj(rawCall)
				index, _ := chatNumber(call, "index")
				target := s.toolCallAt(int(index))
				if id := chatField(call, "id"); id != "" {
					target.Set("id", id)
				}
				fn := chatObj(chatGet(call, "function"))
				if name := chatField(fn, "name"); name != "" {
					targetFunction := chatObj(chatGet(target, "function"))
					targetFunction.Set("name", chatField(targetFunction, "name")+name)
				}
				if arguments := chatField(fn, "arguments"); arguments != "" {
					targetFunction := chatObj(chatGet(target, "function"))
					targetFunction.Set("arguments", chatField(targetFunction, "arguments")+arguments)
				}
			}
		}
		if reason, ok := choice.Get("finish_reason"); ok && chatTruthy(reason) {
			s.finish = reason
		}
	}
}

// toolCallAt returns the tool call of one index, creating it when the stream has
// not named it yet.
func (s *chatStreamAccumulator) toolCallAt(index int) *transport.Obj {
	if index < 0 {
		index = 0
	}
	for len(s.toolCalls) <= index {
		s.toolCalls = append(s.toolCalls, transport.NewObj().
			Set("id", "").
			Set("type", "function").
			Set("function", transport.NewObj().Set("name", "").Set("arguments", "")))
	}
	return s.toolCalls[index]
}

// hasAnswer reports whether the stream produced something a client can read.
func (s *chatStreamAccumulator) hasAnswer() bool {
	return strings.TrimSpace(s.content.String()) != "" || len(s.toolCalls) > 0
}

// message is the accumulated answer as a canonical assistant message.
func (s *chatStreamAccumulator) message() *transport.Obj {
	content := any(s.content.String())
	if content == "" && len(s.toolCalls) > 0 {
		content = nil
	}
	message := transport.NewObj().Set("role", "assistant").Set("content", content)
	if len(s.toolCalls) > 0 {
		calls := make([]any, 0, len(s.toolCalls))
		for _, call := range s.toolCalls {
			if call != nil {
				calls = append(calls, call)
			}
		}
		message.Set("tool_calls", calls)
	}
	if s.reasoning.Len() > 0 {
		message.Set("reasoning_content", s.reasoning.String())
	}
	return message
}

// finishReason is the completion's finish reason, "stop" when the stream named
// none.
func (s *chatStreamAccumulator) finishReason() string {
	if text, ok := s.finish.(string); ok && text != "" {
		return text
	}
	return "stop"
}

// chatMarshal renders a decoded body as the bytes an adapter returns.
func chatMarshal(body any) ([]byte, error) { return transport.Marshal(body) }

// chatFinishOr is `finishReason || fallback`.
func chatFinishOr(value any, fallback string) any {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

// chatClassifyOrdered classifies a provider failure whose body is an
// insertion-ordered object.
func chatClassifyOrdered(status int, body *transport.Obj) error {
	return providers.ClassifyHTTP(status, chatPlainValue(body), nil)
}
