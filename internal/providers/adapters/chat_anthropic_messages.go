package adapters

import (
	"encoding/json"
	"io"
	"iter"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// The outbound half of capabilities/chat/translators/anthropic_messages.js: a
// canonical (OpenAI Chat) request rendered as an Anthropic Messages request, and
// an Anthropic answer or event stream read back into canonical ones. The inbound
// half (an Anthropic client request turned canonical) belongs to the gateway,
// which serves /v1/messages.
//
// Anthropic's dialect differs enough from the canonical one that every field
// here is deliberate: cache breakpoints, thinking budgets, image sources and the
// tool_result turn are all things the JS translator decides.

// anthropicAPIVersion is the Messages API version the adapter sends.
const anthropicAPIVersion = "2023-06-01"

// anthropicStopTo is canonical → Anthropic finish reasons.
var anthropicStopTo = map[string]string{
	"stop": "end_turn", "length": "max_tokens", "tool_calls": "tool_use",
	"function_call": "tool_use", "content_filter": "refusal",
}

// anthropicStopFrom is Anthropic → canonical finish reasons.
var anthropicStopFrom = map[string]string{
	"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length",
	"tool_use": "tool_calls", "pause_turn": "stop", "refusal": "content_filter",
}

// anthropicThinkingBudgets is the output-token budget a reasoning effort asks
// Anthropic for.
var anthropicThinkingBudgets = map[string]float64{"minimal": 1024, "low": 2048, "medium": 8192, "high": 24576}

// anthropicRequest is one canonical request as an Anthropic Messages body.
func anthropicRequest(request *transport.Obj, defaultMaxTokens float64) *transport.Obj {
	var system []any
	var messages []any
	push := func(role string, blocks []any) {
		if len(blocks) == 0 {
			return
		}
		if len(messages) > 0 {
			last := chatObj(messages[len(messages)-1])
			if chatField(last, "role") == role {
				content := chatList(chatGet(last, "content"))
				last.Set("content", append(content, blocks...))
				return
			}
		}
		messages = append(messages, transport.NewObj().Set("role", role).Set("content", blocks))
	}
	for _, message := range chatMessages(request) {
		switch chatField(message, "role") {
		case "system", "developer":
			for _, part := range chatContentParts(chatGet(message, "content")) {
				if chatField(part, "type") != "text" {
					continue
				}
				block := part.Clone()
				if cache, ok := message.Get("cache_control"); ok && cache != nil {
					block.Set("cache_control", cache)
				}
				system = append(system, block)
			}
		case "tool":
			push("user", []any{transport.NewObj().
				Set("type", "tool_result").
				Set("tool_use_id", chatField(message, "tool_call_id")).
				Set("content", chatTextOf(chatGet(message, "content")))})
		case "assistant":
			var blocks []any
			if text := chatTextOf(chatGet(message, "content")); text != "" {
				blocks = append(blocks, transport.NewObj().Set("type", "text").Set("text", text))
			}
			for _, raw := range chatList(chatGet(message, "tool_calls")) {
				call := chatObj(raw)
				fn := chatObj(chatGet(call, "function"))
				blocks = append(blocks, transport.NewObj().
					Set("type", "tool_use").
					Set("id", chatGet(call, "id")).
					Set("name", chatGet(fn, "name")).
					Set("input", chatToolArguments(chatField(fn, "arguments"))))
			}
			push("assistant", blocks)
		default:
			var blocks []any
			for _, part := range chatContentParts(chatGet(message, "content")) {
				if block := anthropicPart(part); block != nil {
					blocks = append(blocks, block)
				}
			}
			push("user", blocks)
		}
	}
	maxTokens := float64(defaultMaxTokens)
	if value, ok := chatNumber(request, "max_tokens"); ok && value != 0 {
		maxTokens = value
	}
	body := transport.NewObj().Set("messages", messages).Set("max_tokens", chatCount(maxTokens))
	if len(system) > 0 {
		body.Set("system", system)
	}
	if value, ok := chatNumber(request, "temperature"); ok {
		body.Set("temperature", min(1, value))
	}
	if value, ok := chatNumber(request, "top_p"); ok {
		body.Set("top_p", value)
	}
	if stop, ok := request.Get("stop"); ok && stop != nil {
		if items, isList := stop.([]any); isList {
			body.Set("stop_sequences", items)
		} else {
			body.Set("stop_sequences", []any{stop})
		}
	}
	if tools := chatList(chatGet(request, "tools")); len(tools) > 0 {
		converted := make([]any, 0, len(tools))
		for _, raw := range tools {
			tool := chatObj(raw)
			fn := chatObj(chatGet(tool, "function"))
			parameters := chatGet(fn, "parameters")
			if parameters == nil {
				parameters = transport.NewObj().Set("type", "object").Set("properties", transport.NewObj())
			}
			converted = append(converted, transport.NewObj().
				Set("name", chatGet(fn, "name")).
				Set("description", chatDescription(fn)).
				Set("input_schema", parameters))
		}
		body.Set("tools", converted)
	}
	anthropicToolChoice(body, chatGet(request, "tool_choice"))
	if parallel, ok := request.Get("parallel_tool_calls"); ok && parallel == false && body.Has("tools") {
		choice := chatObj(chatGet(body, "tool_choice"))
		if choice == nil {
			choice = transport.NewObj()
		}
		body.Set("tool_choice", choice.Clone().Set("type", chatFinishOr(chatGet(choice, "type"), "auto")).Set("disable_parallel_tool_use", true))
	}
	if effort := chatField(request, "reasoning_effort"); effort != "" {
		if budget, ok := anthropicThinkingBudgets[effort]; ok && maxTokens > budget {
			body.Set("thinking", transport.NewObj().Set("type", "enabled").Set("budget_tokens", chatCount(budget)))
			body.Delete("temperature")
			body.Delete("top_p")
		}
	}
	if format := chatObj(chatGet(request, "response_format")); format != nil {
		kind := chatField(format, "type")
		if kind == "json_schema" || kind == "json_object" {
			note := "Respond only with valid JSON."
			if schema := chatGet(chatObj(chatGet(format, "json_schema")), "schema"); schema != nil {
				if encoded, err := chatMarshal(chatObjWrap(schema)); err == nil {
					note = "Respond only with JSON matching this schema: " + string(encoded)
				}
			}
			systemBlocks := chatList(chatGet(body, "system"))
			body.Set("system", append(systemBlocks, transport.NewObj().Set("type", "text").Set("text", note)))
		}
	}
	// Stable prefix breakpoints are upstream cache hints, never a local answer
	// cache.
	anthropicCacheBreakpoints(body)
	return body
}

// anthropicToolChoice renders the canonical tool_choice as Anthropic's.
func anthropicToolChoice(body *transport.Obj, choice any) {
	switch value := choice.(type) {
	case string:
		switch value {
		case "required":
			body.Set("tool_choice", transport.NewObj().Set("type", "any"))
		case "none":
			body.Set("tool_choice", transport.NewObj().Set("type", "none"))
		case "auto":
			body.Set("tool_choice", transport.NewObj().Set("type", "auto"))
		}
	case *transport.Obj:
		if name := chatField(chatObj(chatGet(value, "function")), "name"); name != "" {
			body.Set("tool_choice", transport.NewObj().Set("type", "tool").Set("name", name))
		}
	case map[string]any:
		anthropicToolChoice(body, chatObj(value))
	}
}

// anthropicCacheBreakpoints marks the last system block and the last tool as
// cache breakpoints when the request names none.
func anthropicCacheBreakpoints(body *transport.Obj) {
	if system := chatList(chatGet(body, "system")); len(system) > 0 {
		marked := false
		for _, block := range system {
			if chatObj(block).Has("cache_control") {
				marked = true
				break
			}
		}
		if !marked {
			last := chatObj(system[len(system)-1])
			last.Set("cache_control", transport.NewObj().Set("type", "ephemeral"))
		}
	}
	if tools := chatList(chatGet(body, "tools")); len(tools) > 0 {
		marked := false
		for _, tool := range tools {
			if chatObj(tool).Has("cache_control") {
				marked = true
				break
			}
		}
		if !marked {
			chatObj(tools[len(tools)-1]).Set("cache_control", transport.NewObj().Set("type", "ephemeral"))
		}
	}
}

// anthropicPart is one canonical content part as an Anthropic block.
func anthropicPart(part *transport.Obj) *transport.Obj {
	switch chatField(part, "type") {
	case "text":
		return transport.NewObj().Set("type", "text").Set("text", chatField(part, "text"))
	case "image_url":
		url := chatImageURL(part)
		if mimeType, data, ok := chatDataURI(url); ok {
			return transport.NewObj().Set("type", "image").Set("source",
				transport.NewObj().Set("type", "base64").Set("media_type", mimeType).Set("data", data))
		}
		return transport.NewObj().Set("type", "image").Set("source",
			transport.NewObj().Set("type", "url").Set("url", url))
	}
	return nil
}

// anthropicResponse reads one Anthropic message as a canonical completion.
func anthropicResponse(message *transport.Obj, modelID string) *transport.Obj {
	var text, reasoning strings.Builder
	var toolCalls []any
	for _, raw := range chatList(chatGet(message, "content")) {
		block := chatObj(raw)
		switch chatField(block, "type") {
		case "text":
			text.WriteString(chatField(block, "text"))
		case "thinking":
			reasoning.WriteString(chatField(block, "thinking"))
		case "tool_use":
			toolCalls = append(toolCalls, transport.NewObj().
				Set("id", chatGet(block, "id")).
				Set("type", "function").
				Set("function", transport.NewObj().
					Set("name", chatGet(block, "name")).
					Set("arguments", chatJSONText(chatGet(block, "input")))))
		}
	}
	var content any = text.String()
	if content == "" && len(toolCalls) > 0 {
		content = nil
	}
	out := transport.NewObj().Set("role", "assistant").Set("content", content)
	if len(toolCalls) > 0 {
		out.Set("tool_calls", toolCalls)
	}
	if reasoning.Len() > 0 {
		out.Set("reasoning_content", reasoning.String())
	}
	usage := chatObj(chatGet(message, "usage"))
	input, _ := chatNumber(usage, "input_tokens")
	read, _ := chatNumber(usage, "cache_read_input_tokens")
	creation, _ := chatNumber(usage, "cache_creation_input_tokens")
	output, _ := chatNumber(usage, "output_tokens")
	id := chatField(message, "id")
	served := chatField(message, "model")
	if served == "" {
		served = modelID
	}
	return chatBuildResponse(id, served, out, anthropicStopFrom[chatField(message, "stop_reason")], transport.NewObj().
		Set("prompt_tokens", chatCount(input+read+creation)).
		Set("prompt_tokens_details", transport.NewObj().Set("cached_tokens", chatCount(read))).
		Set("cache_creation_input_tokens", chatCount(creation)).
		Set("completion_tokens", chatCount(output)).
		Set("total_tokens", chatCount(input+read+creation+output)))
}

// anthropicStream reads Anthropic's event stream as canonical chunks. A
// failure event ends the sequence with the classified error, as the JS adapter
// raised it after the reader returned.
func anthropicStream(body io.Reader, modelID string) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		id := chatCompletionID()
		created := chatNowSeconds()
		input, cached, creation, output := 0.0, 0.0, 0.0, 0.0
		var finish any
		toolIndex := map[float64]int{}
		chunk := func(delta *transport.Obj, finishReason any, usage *transport.Obj) *transport.Obj {
			return chatBuildChunk(id, modelID, created, delta, finishReason, usage)
		}
		for event, streamErr := range providers.SSE(body) {
			if streamErr != nil {
				yield(nil, streamErr)
				return
			}
			decoded, decodeErr := chatDecodeJSON([]byte(event.Data))
			if decodeErr != nil {
				continue
			}
			payload := chatObj(decoded)
			kind := chatField(payload, "type")
			if kind == "" {
				kind = event.Event
			}
			switch kind {
			case "error":
				yield(nil, anthropicStreamFailure(payload))
				return
			case "message_start":
				message := chatObj(chatGet(payload, "message"))
				if name := chatField(message, "id"); name != "" {
					id = name
				}
				usage := chatObj(chatGet(message, "usage"))
				cached, _ = chatNumber(usage, "cache_read_input_tokens")
				creation, _ = chatNumber(usage, "cache_creation_input_tokens")
				raw, _ := chatNumber(usage, "input_tokens")
				input = raw + cached + creation
				if !yield(chatEmit(chunk(transport.NewObj().Set("role", "assistant").Set("content", ""), nil, nil))) {
					return
				}
			case "content_block_start":
				block := chatObj(chatGet(payload, "content_block"))
				if chatField(block, "type") == "tool_use" {
					index, _ := chatNumber(payload, "index")
					position := len(toolIndex)
					toolIndex[index] = position
					call := transport.NewObj().
						Set("index", position).
						Set("id", chatGet(block, "id")).
						Set("type", "function").
						Set("function", transport.NewObj().Set("name", chatGet(block, "name")).Set("arguments", ""))
					if !yield(chatEmit(chunk(transport.NewObj().Set("tool_calls", []any{call}), nil, nil))) {
						return
					}
				}
			case "content_block_delta":
				delta := chatObj(chatGet(payload, "delta"))
				switch chatField(delta, "type") {
				case "text_delta":
					if !yield(chatEmit(chunk(transport.NewObj().Set("content", chatGet(delta, "text")), nil, nil))) {
						return
					}
				case "thinking_delta":
					if !yield(chatEmit(chunk(transport.NewObj().Set("reasoning_content", chatGet(delta, "thinking")), nil, nil))) {
						return
					}
				case "input_json_delta":
					index, _ := chatNumber(payload, "index")
					position, known := toolIndex[index]
					if !known {
						break
					}
					call := transport.NewObj().
						Set("index", position).
						Set("function", transport.NewObj().Set("arguments", chatGet(delta, "partial_json")))
					if !yield(chatEmit(chunk(transport.NewObj().Set("tool_calls", []any{call}), nil, nil))) {
						return
					}
				}
			case "message_delta":
				if stop := chatField(chatObj(chatGet(payload, "delta")), "stop_reason"); stop != "" {
					finish = anthropicStopFrom[stop]
				}
				usage := chatObj(chatGet(payload, "usage"))
				if value, ok := chatNumber(usage, "output_tokens"); ok {
					output = value
				}
				if value, ok := chatNumber(usage, "cache_read_input_tokens"); ok {
					cached = value
				}
				if value, ok := chatNumber(usage, "cache_creation_input_tokens"); ok {
					creation = value
				}
				if value, ok := chatNumber(usage, "input_tokens"); ok {
					input = value + cached + creation
				}
			case "message_stop":
				usage := transport.NewObj().
					Set("prompt_tokens", chatCount(input)).
					Set("prompt_tokens_details", transport.NewObj().Set("cached_tokens", chatCount(cached))).
					Set("cache_creation_input_tokens", chatCount(creation)).
					Set("completion_tokens", chatCount(output)).
					Set("total_tokens", chatCount(input+output))
				yield(chatEmit(chunk(transport.NewObj(), chatFinishOr(finish, "stop"), usage)))
				return
			}
		}
		usage := transport.NewObj().
			Set("prompt_tokens", chatCount(input)).
			Set("prompt_tokens_details", transport.NewObj().Set("cached_tokens", chatCount(cached))).
			Set("cache_creation_input_tokens", chatCount(creation)).
			Set("completion_tokens", chatCount(output)).
			Set("total_tokens", chatCount(input+output))
		yield(chatEmit(chunk(transport.NewObj(), chatFinishOr(finish, "stop"), usage)))
	}
}

// anthropicStreamFailure classifies an error event: Anthropic states the kind
// in the payload and the status only in the words, so the status comes from the
// event type as the JS adapter reads it.
func anthropicStreamFailure(payload *transport.Obj) error {
	status := 500
	switch chatField(chatObj(chatGet(payload, "error")), "type") {
	case "overloaded_error":
		status = 529
	case "rate_limit_error":
		status = 429
	}
	return providers.ClassifyHTTP(status, chatPlainValue(payload), nil)
}

// chatEmit renders one canonical chunk for a stream.
func chatEmit(chunk *transport.Obj) (json.RawMessage, error) {
	encoded, err := chatMarshal(chunk)
	if err != nil {
		return nil, providers.NewError(providers.KindInvalidOutput, "The answer could not be encoded.", providers.ErrorOptions{})
	}
	return encoded, nil
}

// chatToolArguments parses a tool call's arguments as the object Anthropic wants,
// an empty object when they are not JSON.
func chatToolArguments(raw string) *transport.Obj {
	if strings.TrimSpace(raw) == "" {
		return transport.NewObj()
	}
	decoded, err := transport.ParseOrdered([]byte(raw))
	if err != nil {
		return transport.NewObj()
	}
	obj := chatObj(decoded)
	if obj == nil {
		return transport.NewObj()
	}
	return obj
}

// chatDataURI splits a data: URL into its media type and payload.
func chatDataURI(url string) (mimeType, data string, ok bool) {
	if !strings.HasPrefix(url, "data:") {
		return "", "", false
	}
	header, payload, found := strings.Cut(strings.TrimPrefix(url, "data:"), ",")
	if !found || !strings.HasSuffix(header, ";base64") {
		return "", "", false
	}
	return strings.TrimSuffix(header, ";base64"), payload, true
}

// chatJSONText is JSON.stringify of a decoded value.
func chatJSONText(value any) string {
	encoded, err := chatMarshal(chatObjWrap(value))
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// chatObjWrap allows marshaling a value that may be nil, a scalar or an object.
func chatObjWrap(value any) any {
	if value == nil {
		return transport.NewObj()
	}
	return value
}

// chatDescription is a tool's description, empty when it has none.
func chatDescription(fn *transport.Obj) any {
	if value := chatGet(fn, "description"); value != nil {
		return value
	}
	return ""
}
