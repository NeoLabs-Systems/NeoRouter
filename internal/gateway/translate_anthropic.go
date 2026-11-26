package gateway

// The Anthropic Messages dialect, both ways.
// toCanonical/responseFromCanonical/streamFromCanonical translate what a client
// speaks into the canonical chat format; fromCanonical/responseToCanonical/
// streamToCanonical translate the canonical format into what an Anthropic
// upstream speaks (the adapters use those).

import (
	"encoding/json"
	"iter"
	"math"
)

// stopToAnthropic and stopFromAnthropic map finish reasons both ways.
var stopToAnthropic = map[string]string{
	"stop": "end_turn", "length": "max_tokens", "tool_calls": "tool_use",
	"function_call": "tool_use", "content_filter": "refusal",
}

var stopFromAnthropic = map[string]string{
	"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length",
	"tool_use": "tool_calls", "pause_turn": "stop", "refusal": "content_filter",
}

// AnthropicTranslator is the Anthropic Messages dialect.
type AnthropicTranslator struct{}

// MessageID is messageId: "msg_" and 24 hex characters.
func MessageID() string { return "msg_" + randomHex(12) }

// effortFromBudget is effortFromBudget: a thinking budget as a reasoning effort.
func effortFromBudget(budget float64, ok bool) (string, bool) {
	if !ok {
		return "", false
	}
	switch {
	case budget < 4096:
		return "low", true
	case budget < 16384:
		return "medium", true
	default:
		return "high", true
	}
}

// blocksOf is blocksOf: a string is one text block, an array stays, anything
// else is no content.
func blocksOf(content any) []any {
	if text, ok := content.(string); ok {
		return []any{newObjOf("type", "text", "text", text)}
	}
	return asList(content)
}

// imageBlockToPart is imageBlockToPart: an Anthropic image block as a canonical
// image part.
func imageBlockToPart(block *Obj) *Obj {
	source := getObj(block, "source")
	url := getString(source, "url")
	if getString(source, "type") == "base64" {
		url = "data:" + getString(source, "media_type") + ";base64," + getString(source, "data")
	}
	return newObjOf("type", "image_url", "image_url", newObjOf("url", url))
}

// toolResultText is toolResultText: the text of a tool result's content.
func toolResultText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	var out string
	for _, item := range blocksOf(content) {
		block := asObj(item)
		if getString(block, "type") == "text" {
			out += getString(block, "text")
		}
	}
	return out
}

// ToCanonical is toCanonical: an Anthropic Messages request as a canonical one.
func (AnthropicTranslator) ToCanonical(body *Obj) (*Obj, error) {
	messages := make([]any, 0, 4)
	systemText := getString(body, "system")
	systemBlocks := blocksOf(getValue(body, "system"))
	if _, isString := getValue(body, "system").(string); !isString {
		parts := make([]string, 0, len(systemBlocks))
		for _, item := range systemBlocks {
			parts = append(parts, getString(asObj(item), "text"))
		}
		systemText = joinLines(parts)
	}
	if systemText != "" {
		var content any = systemText
		if _, isArray := getValue(body, "system").([]any); isArray {
			blocks := make([]any, 0, len(systemBlocks))
			for _, item := range systemBlocks {
				block := asObj(item)
				if getString(block, "type") != "text" {
					continue
				}
				blocks = append(blocks, clone(block))
			}
			content = blocks
		}
		messages = append(messages, newObjOf("role", "system", "content", content))
	}
	for _, item := range getList(body, "messages") {
		message := asObj(item)
		blocks := blocksOf(getValue(message, "content"))
		if getString(message, "role") == "assistant" {
			var text string
			toolCalls := make([]any, 0, 2)
			for _, rawBlock := range blocks {
				block := asObj(rawBlock)
				switch getString(block, "type") {
				case "text":
					text += getString(block, "text")
				case "tool_use":
					toolCalls = append(toolCalls, newObjOf(
						"id", getString(block, "id"),
						"type", "function",
						"function", newObjOf(
							"name", getString(block, "name"),
							"arguments", marshalObject(getValue(block, "input")),
						),
					))
				}
			}
			out := newObjOf("role", "assistant")
			if text != "" || len(toolCalls) == 0 {
				out.Set("content", text)
			} else {
				out.Set("content", nil)
			}
			if len(toolCalls) > 0 {
				out.Set("tool_calls", toolCalls)
			}
			messages = append(messages, out)
			continue
		}
		for _, rawBlock := range blocks {
			block := asObj(rawBlock)
			if getString(block, "type") != "tool_result" {
				continue
			}
			text := toolResultText(getValue(block, "content"))
			if getBool(block, "is_error") {
				text = "Error: " + text
			}
			messages = append(messages, newObjOf("role", "tool", "tool_call_id", getString(block, "tool_use_id"), "content", text))
		}
		parts := make([]any, 0, len(blocks))
		onlyText := true
		for _, rawBlock := range blocks {
			block := asObj(rawBlock)
			var part *Obj
			switch getString(block, "type") {
			case "tool_result":
				continue
			case "image":
				part = imageBlockToPart(block)
			case "text":
				part = newObjOf("type", "text", "text", getString(block, "text"))
			case "document":
				if getString(getObj(block, "source"), "type") == "text" {
					part = newObjOf("type", "text", "text", getString(getObj(block, "source"), "data"))
				}
			}
			if part == nil {
				continue
			}
			if getString(part, "type") != "text" {
				onlyText = false
			}
			parts = append(parts, part)
		}
		if len(parts) > 0 {
			var content any
			if onlyText {
				texts := make([]string, 0, len(parts))
				for _, part := range parts {
					texts = append(texts, getString(asObj(part), "text"))
				}
				content = joinStrings(texts)
			} else {
				content = parts
			}
			messages = append(messages, newObjOf("role", "user", "content", content))
		}
	}
	request := newObjOf("messages", messages, "stream", getBool(body, "stream"))
	if maxTokens, ok := getNumber(body, "max_tokens"); ok {
		request.Set("max_tokens", maxTokens)
	}
	// temperature/top_p are passed through even when null is what the client
	// sent.
	if value, ok := body.Get("temperature"); ok && value != nil {
		request.Set("temperature", value)
	}
	if value, ok := body.Get("top_p"); ok && value != nil {
		request.Set("top_p", value)
	}
	if stops := getList(body, "stop_sequences"); len(stops) > 0 {
		request.Set("stop", stops)
	}
	if tools := getList(body, "tools"); len(tools) > 0 {
		mapped := make([]any, 0, len(tools))
		for _, rawTool := range tools {
			tool := asObj(rawTool)
			kind := getString(tool, "type")
			if tool == nil || (getValue(tool, "input_schema") == nil && kind != "" && kind != "custom") {
				continue
			}
			entry := newObjOf("type", "function")
			if cacheControl, ok := get(tool, "cache_control"); ok {
				entry.Set("cache_control", cacheControl)
			}
			parameters := getValue(tool, "input_schema")
			if parameters == nil {
				parameters = newObjOf("type", "object", "properties", newObj())
			}
			entry.Set("function", newObjOf(
				"name", getString(tool, "name"),
				"description", getString(tool, "description"),
				"parameters", parameters,
			))
			mapped = append(mapped, entry)
		}
		if len(mapped) > 0 {
			request.Set("tools", mapped)
		}
	}
	choice := getObj(body, "tool_choice")
	switch getString(choice, "type") {
	case "any":
		request.Set("tool_choice", "required")
	case "tool":
		request.Set("tool_choice", newObjOf("type", "function", "function", newObjOf("name", getString(choice, "name"))))
	case "none":
		request.Set("tool_choice", "none")
	case "auto":
		request.Set("tool_choice", "auto")
	}
	if getBool(choice, "disable_parallel_tool_use") {
		request.Set("parallel_tool_calls", false)
	}
	if getString(getObj(body, "thinking"), "type") == "enabled" {
		if effort, ok := effortFromBudget(getNumber(getObj(body, "thinking"), "budget_tokens")); ok {
			request.Set("reasoning_effort", effort)
		}
	}
	if userID, ok := get(getObj(body, "metadata"), "user_id"); ok && truthy(userID) {
		request.Set("user", jsString(userID))
	}
	return request, nil
}

// AnthropicUsage is anthropicUsage: a canonical usage block as Anthropic's.
func AnthropicUsage(usage *Obj) *Obj {
	cached, _ := getNumber(getObj(usage, "prompt_tokens_details"), "cached_tokens")
	creation, _ := getNumber(usage, "cache_creation_input_tokens")
	prompt, _ := getNumber(usage, "prompt_tokens")
	completion, _ := getNumber(usage, "completion_tokens")
	return newObjOf(
		"input_tokens", math.Max(0, prompt-cached-creation),
		"output_tokens", completion,
		"cache_read_input_tokens", cached,
		"cache_creation_input_tokens", creation,
	)
}

// ResponseFromCanonical is responseFromCanonical: a canonical completion as an
// Anthropic message.
func (AnthropicTranslator) ResponseFromCanonical(completion *Obj, model string) *Obj {
	choice := asObj(firstItem(getList(completion, "choices")))
	message := getObj(choice, "message")
	content := make([]any, 0, 3)
	reasoning := getString(message, "reasoning_content")
	if reasoning == "" {
		reasoning = getString(message, "reasoning")
	}
	if reasoning != "" {
		content = append(content, newObjOf("type", "thinking", "thinking", reasoning, "signature", ""))
	}
	if text := TextOf(getValue(message, "content")); text != "" {
		content = append(content, newObjOf("type", "text", "text", text))
	}
	for _, rawCall := range getList(message, "tool_calls") {
		call := asObj(rawCall)
		var input any = newObj()
		if args := getString(getObj(call, "function"), "arguments"); args != "" {
			if parsed, err := parseOrdered([]byte(args)); err == nil {
				input = parsed
			}
		}
		id := getString(call, "id")
		if id == "" {
			id = "toolu_" + randomHex(8)
		}
		content = append(content, newObjOf("type", "tool_use", "id", id, "name", getString(getObj(call, "function"), "name"), "input", input))
	}
	usage := NormalizeUsage(getValue(completion, "usage"))
	if usage == nil {
		usage = newObjOf("prompt_tokens", 0, "completion_tokens", 0)
	}
	stopReason := stopToAnthropic[getString(choice, "finish_reason")]
	if stopReason == "" {
		stopReason = "end_turn"
	}
	return newObjOf(
		"id", MessageID(),
		"type", "message",
		"role", "assistant",
		"model", model,
		"content", content,
		"stop_reason", stopReason,
		"stop_sequence", nil,
		"usage", AnthropicUsage(usage),
	)
}

// StreamFromCanonical is streamFromCanonical: canonical chunks as the Anthropic
// event sequence.
func (AnthropicTranslator) StreamFromCanonical(chunks iter.Seq2[*Obj, error], model string, yield func(Event) bool) error {
	id := MessageID()
	started := false
	index := -1
	blockType := ""
	toolBlocks := map[int]int{}
	finish := ""
	var usage *Obj

	start := func() Event {
		return Event{Event: "message_start", Data: newObjOf(
			"type", "message_start",
			"message", newObjOf(
				"id", id, "type", "message", "role", "assistant", "model", model,
				"content", []any{}, "stop_reason", nil, "stop_sequence", nil,
				"usage", newObjOf("input_tokens", 0, "output_tokens", 0),
			),
		)}
	}
	closeBlock := func() bool {
		if blockType == "" {
			return true
		}
		blockType = ""
		return yield(Event{Event: "content_block_stop", Data: newObjOf("type", "content_block_stop", "index", float64(index))})
	}
	openBlock := func(kind string, block *Obj) bool {
		if !closeBlock() {
			return false
		}
		index++
		blockType = kind
		return yield(Event{Event: "content_block_start", Data: newObjOf("type", "content_block_start", "index", float64(index), "content_block", block)})
	}
	delta := func(payload *Obj) bool {
		return yield(Event{Event: "content_block_delta", Data: newObjOf("type", "content_block_delta", "index", float64(index), "delta", payload)})
	}

	for chunk, err := range chunks {
		if err != nil {
			return err
		}
		if !started {
			started = true
			if !yield(start()) {
				return nil
			}
		}
		if raw := getValue(chunk, "usage"); truthy(raw) {
			usage = NormalizeUsage(raw)
		}
		for _, item := range getList(chunk, "choices") {
			choice := asObj(item)
			deltaObj := getObj(choice, "delta")
			reasoning := getString(deltaObj, "reasoning_content")
			if reasoning == "" {
				reasoning = getString(deltaObj, "reasoning")
			}
			if reasoning != "" {
				if blockType != "thinking" {
					if !openBlock("thinking", newObjOf("type", "thinking", "thinking", "")) {
						return nil
					}
				}
				if !delta(newObjOf("type", "thinking_delta", "thinking", reasoning)) {
					return nil
				}
			}
			if content := getString(deltaObj, "content"); content != "" {
				if blockType != "text" {
					if !openBlock("text", newObjOf("type", "text", "text", "")) {
						return nil
					}
				}
				if !delta(newObjOf("type", "text_delta", "text", content)) {
					return nil
				}
			}
			for _, rawCall := range getList(deltaObj, "tool_calls") {
				call := asObj(rawCall)
				key := 0
				if position, ok := getNumber(call, "index"); ok {
					key = int(position)
				}
				if _, seen := toolBlocks[key]; !seen {
					id := getString(call, "id")
					if id == "" {
						id = "toolu_" + randomHex(8)
					}
					if !openBlock("tool", newObjOf("type", "tool_use", "id", id, "name", getString(getObj(call, "function"), "name"), "input", newObj())) {
						return nil
					}
					toolBlocks[key] = index
				}
				if args := getString(getObj(call, "function"), "arguments"); args != "" {
					if !yield(Event{Event: "content_block_delta", Data: newObjOf(
						"type", "content_block_delta", "index", float64(toolBlocks[key]),
						"delta", newObjOf("type", "input_json_delta", "partial_json", args),
					)}) {
						return nil
					}
				}
			}
			if reason := getString(choice, "finish_reason"); reason != "" {
				finish = reason
			}
		}
	}
	if !started {
		if !yield(start()) {
			return nil
		}
	}
	if !closeBlock() {
		return nil
	}
	stopReason := stopToAnthropic[finish]
	if stopReason == "" {
		stopReason = "end_turn"
	}
	if !yield(Event{Event: "message_delta", Data: newObjOf(
		"type", "message_delta",
		"delta", newObjOf("stop_reason", stopReason, "stop_sequence", nil),
		"usage", AnthropicUsage(usage),
	)}) {
		return nil
	}
	yield(Event{Event: "message_stop", Data: newObjOf("type", "message_stop")})
	return nil
}

// partToBlock is partToBlock: a canonical content part as an Anthropic block.
func partToBlock(part *Obj) *Obj {
	switch getString(part, "type") {
	case "text":
		return newObjOf("type", "text", "text", getString(part, "text"))
	case "image_url":
		url := getString(part, "image_url")
		if nested := getObj(part, "image_url"); nested != nil {
			url = getString(nested, "url")
		}
		if media, data, ok := splitDataURL(url); ok {
			return newObjOf("type", "image", "source", newObjOf("type", "base64", "media_type", media, "data", data))
		}
		return newObjOf("type", "image", "source", newObjOf("type", "url", "url", url))
	}
	return nil
}

// effortBudgets are the thinking budgets fromCanonical maps an effort to.
var effortBudgets = map[string]float64{"minimal": 1024, "low": 2048, "medium": 8192, "high": 24576}

// FromCanonical is fromCanonical: a canonical request as an Anthropic upstream
// body. It is what the Anthropic adapter sends.
func (AnthropicTranslator) FromCanonical(request *Obj, defaultMaxTokens int) *Obj {
	if defaultMaxTokens == 0 {
		defaultMaxTokens = 4096
	}
	system := make([]any, 0, 2)
	messages := make([]any, 0, len(getList(request, "messages")))
	push := func(role string, blocks []any) {
		if len(messages) > 0 {
			last := asObj(messages[len(messages)-1])
			if getString(last, "role") == role {
				last.Set("content", append(getList(last, "content"), blocks...))
				return
			}
		}
		messages = append(messages, newObjOf("role", role, "content", blocks))
	}
	for _, item := range getList(request, "messages") {
		message := asObj(item)
		role := getString(message, "role")
		switch role {
		case "system", "developer":
			cacheControl, hasCache := get(message, "cache_control")
			for _, rawPart := range ContentParts(getValue(message, "content")) {
				part := asObj(rawPart)
				if getString(part, "type") != "text" {
					continue
				}
				block := clone(part)
				if hasCache {
					block.Set("cache_control", cacheControl)
				}
				system = append(system, block)
			}
		case "tool":
			push("user", []any{newObjOf(
				"type", "tool_result",
				"tool_use_id", getString(message, "tool_call_id"),
				"content", TextOf(getValue(message, "content")),
			)})
		case "assistant":
			blocks := make([]any, 0, 2)
			if text := TextOf(getValue(message, "content")); text != "" {
				blocks = append(blocks, newObjOf("type", "text", "text", text))
			}
			for _, rawCall := range getList(message, "tool_calls") {
				call := asObj(rawCall)
				var input any = newObj()
				if args := getString(getObj(call, "function"), "arguments"); args != "" {
					if parsed, err := parseOrdered([]byte(args)); err == nil {
						input = parsed
					}
				}
				blocks = append(blocks, newObjOf("type", "tool_use", "id", getString(call, "id"), "name", getString(getObj(call, "function"), "name"), "input", input))
			}
			if len(blocks) > 0 {
				push("assistant", blocks)
			}
		default:
			blocks := make([]any, 0, 2)
			for _, rawPart := range ContentParts(getValue(message, "content")) {
				if block := partToBlock(asObj(rawPart)); block != nil {
					blocks = append(blocks, block)
				}
			}
			if len(blocks) > 0 {
				push("user", blocks)
			}
		}
	}
	maxTokens := float64(defaultMaxTokens)
	if value, ok := getNumber(request, "max_tokens"); ok && value != 0 {
		maxTokens = value
	}
	body := newObjOf("messages", messages, "max_tokens", maxTokens)
	if len(system) > 0 {
		body.Set("system", system)
	}
	if value, ok := getNumber(request, "temperature"); ok {
		body.Set("temperature", math.Min(1, value))
	}
	if value, ok := getNumber(request, "top_p"); ok {
		body.Set("top_p", value)
	}
	if stop, ok := get(request, "stop"); ok {
		if list, isList := stop.([]any); isList {
			body.Set("stop_sequences", list)
		} else {
			body.Set("stop_sequences", []any{stop})
		}
	}
	if tools := getList(request, "tools"); len(tools) > 0 {
		mapped := make([]any, 0, len(tools))
		for _, rawTool := range tools {
			tool := asObj(rawTool)
			function := getObj(tool, "function")
			parameters := getValue(function, "parameters")
			if parameters == nil {
				parameters = newObjOf("type", "object", "properties", newObj())
			}
			mapped = append(mapped, newObjOf(
				"name", getString(function, "name"),
				"description", getString(function, "description"),
				"input_schema", parameters,
			))
		}
		body.Set("tools", mapped)
	}
	if choice, ok := get(request, "tool_choice"); ok {
		switch value := choice.(type) {
		case string:
			switch value {
			case "required":
				body.Set("tool_choice", newObjOf("type", "any"))
			case "none":
				body.Set("tool_choice", newObjOf("type", "none"))
			case "auto":
				body.Set("tool_choice", newObjOf("type", "auto"))
			}
		case *Obj:
			if name := getString(getObj(value, "function"), "name"); name != "" {
				body.Set("tool_choice", newObjOf("type", "tool", "name", name))
			}
		}
	}
	if parallel, ok := get(request, "parallel_tool_calls"); ok && parallel == false && body.Has("tools") {
		current := getObj(body, "tool_choice")
		if current == nil {
			current = newObjOf("type", "auto")
		} else {
			current = clone(current)
		}
		current.Set("disable_parallel_tool_use", true)
		body.Set("tool_choice", current)
	}
	if effort := getString(request, "reasoning_effort"); effort != "" {
		if budget, known := effortBudgets[effort]; known && maxTokens > budget {
			body.Set("thinking", newObjOf("type", "enabled", "budget_tokens", budget))
			body.Delete("temperature")
			body.Delete("top_p")
		}
	}
	format := getObj(request, "response_format")
	if kind := getString(format, "type"); kind == "json_schema" || kind == "json_object" {
		note := "Respond only with valid JSON."
		if schema := getValue(getObj(format, "json_schema"), "schema"); schema != nil {
			note = "Respond only with JSON matching this schema: " + marshal(schema)
		}
		system = append(getList(body, "system"), newObjOf("type", "text", "text", note))
		body.Set("system", system)
	}
	// Stable prefix breakpoints are upstream cache hints, never a local answer cache.
	blocks := getList(body, "system")
	if len(blocks) > 0 && !anyHasKey(blocks, "cache_control") {
		asObj(blocks[len(blocks)-1]).Set("cache_control", newObjOf("type", "ephemeral"))
	}
	bodyTools := getList(body, "tools")
	if len(bodyTools) > 0 {
		requestTools := getList(request, "tools")
		for index, rawTool := range bodyTools {
			if index >= len(requestTools) {
				break
			}
			if cacheControl, ok := get(asObj(requestTools[index]), "cache_control"); ok {
				asObj(rawTool).Set("cache_control", cacheControl)
			}
		}
		if !anyHasKey(bodyTools, "cache_control") {
			asObj(bodyTools[len(bodyTools)-1]).Set("cache_control", newObjOf("type", "ephemeral"))
		}
	}
	return body
}

// ResponseToCanonical is responseToCanonical: an Anthropic upstream message as a
// canonical completion.
func (AnthropicTranslator) ResponseToCanonical(message *Obj, model string) *Obj {
	var text, reasoning string
	toolCalls := make([]any, 0, 2)
	for _, item := range getList(message, "content") {
		block := asObj(item)
		switch getString(block, "type") {
		case "text":
			text += getString(block, "text")
		case "thinking":
			reasoning += getString(block, "thinking")
		case "tool_use":
			toolCalls = append(toolCalls, newObjOf(
				"id", getString(block, "id"),
				"type", "function",
				"function", newObjOf("name", getString(block, "name"), "arguments", marshalObject(getValue(block, "input"))),
			))
		}
	}
	out := newObjOf("role", "assistant")
	if text != "" || len(toolCalls) == 0 {
		out.Set("content", text)
	} else {
		out.Set("content", nil)
	}
	if len(toolCalls) > 0 {
		out.Set("tool_calls", toolCalls)
	}
	if reasoning != "" {
		out.Set("reasoning_content", reasoning)
	}
	usage := getObj(message, "usage")
	inputTokens, _ := getNumber(usage, "input_tokens")
	cacheRead, _ := getNumber(usage, "cache_read_input_tokens")
	cacheWrite, _ := getNumber(usage, "cache_creation_input_tokens")
	outputTokens, _ := getNumber(usage, "output_tokens")
	input := inputTokens + cacheRead + cacheWrite
	finish := stopFromAnthropic[getString(message, "stop_reason")]
	if finish == "" {
		finish = "stop"
	}
	id := getString(message, "id")
	if id == "" {
		id = CompletionID()
	}
	upstreamModel := getString(message, "model")
	if upstreamModel == "" {
		upstreamModel = model
	}
	return BuildResponse(ResponseOptions{
		ID:           id,
		Model:        upstreamModel,
		Message:      out,
		FinishReason: finish,
		Usage: newObjOf(
			"prompt_tokens", input,
			"prompt_tokens_details", newObjOf("cached_tokens", cacheRead),
			"cache_creation_input_tokens", cacheWrite,
			"completion_tokens", outputTokens,
			"total_tokens", input+outputTokens,
		),
	})
}

// UpstreamEvent is one server-sent event an adapter read from an upstream.
type UpstreamEvent struct {
	Event string
	Data  string
}

// StreamToCanonical is streamToCanonical: Anthropic stream events as canonical
// chunks. onError receives a provider error event; the stream then ends.
func (AnthropicTranslator) StreamToCanonical(events func(yield func(UpstreamEvent) bool), model string, onError func(payload *Obj)) ChunkStream {
	return func(yield func(*Obj, error) bool) {
		id := CompletionID()
		created := nowSeconds()
		var input, cached, creation, output float64
		finish := ""
		toolIndex := map[int]int{}
		chunk := func(delta *Obj, finishReason any, usage *Obj) *Obj {
			out := newObjOf(
				"id", id, "object", "chat.completion.chunk", "created", created, "model", model,
				"choices", []any{newObjOf("index", 0, "delta", delta, "finish_reason", finishReason)},
			)
			if usage != nil {
				out.Set("usage", usage)
			}
			return out
		}
		stopped := false
		ended := false
		events(func(event UpstreamEvent) bool {
			payload, err := parseOrdered([]byte(event.Data))
			if err != nil {
				return true
			}
			body, _ := payload.(*Obj)
			if body == nil {
				return true
			}
			kind := getString(body, "type")
			if kind == "" {
				kind = event.Event
			}
			switch kind {
			case "error":
				if onError != nil {
					onError(body)
				}
				stopped = true
				return false
			case "message_start":
				message := getObj(body, "message")
				if value := getString(message, "id"); value != "" {
					id = value
				}
				usage := getObj(message, "usage")
				cached, _ = getNumber(usage, "cache_read_input_tokens")
				creation, _ = getNumber(usage, "cache_creation_input_tokens")
				base, _ := getNumber(usage, "input_tokens")
				input = base + cached + creation
				if !yield(chunk(newObjOf("role", "assistant", "content", ""), nil, nil), nil) {
					stopped = true
					return false
				}
			case "content_block_start":
				block := getObj(body, "content_block")
				if getString(block, "type") != "tool_use" {
					return true
				}
				index, _ := getNumber(body, "index")
				position := len(toolIndex)
				toolIndex[int(index)] = position
				if !yield(chunk(newObjOf("tool_calls", []any{newObjOf(
					"index", position,
					"id", getString(block, "id"),
					"type", "function",
					"function", newObjOf("name", getString(block, "name"), "arguments", ""),
				)}), nil, nil), nil) {
					stopped = true
					return false
				}
			case "content_block_delta":
				delta := getObj(body, "delta")
				index, _ := getNumber(body, "index")
				switch getString(delta, "type") {
				case "text_delta":
					if !yield(chunk(newObjOf("content", getString(delta, "text")), nil, nil), nil) {
						stopped = true
						return false
					}
				case "thinking_delta":
					if !yield(chunk(newObjOf("reasoning_content", getString(delta, "thinking")), nil, nil), nil) {
						stopped = true
						return false
					}
				case "input_json_delta":
					position, known := toolIndex[int(index)]
					if !known {
						return true
					}
					if !yield(chunk(newObjOf("tool_calls", []any{newObjOf(
						"index", position,
						"function", newObjOf("arguments", getString(delta, "partial_json")),
					)}), nil, nil), nil) {
						stopped = true
						return false
					}
				}
			case "message_delta":
				stop := getString(getObj(body, "delta"), "stop_reason")
				if mapped := stopFromAnthropic[stop]; mapped != "" {
					finish = mapped
				}
				usage := getObj(body, "usage")
				if value, ok := getNumber(usage, "output_tokens"); ok {
					output = value
				}
				if value, ok := getNumber(usage, "cache_read_input_tokens"); ok {
					cached = value
				}
				if value, ok := getNumber(usage, "cache_creation_input_tokens"); ok {
					creation = value
				}
				if value, ok := getNumber(usage, "input_tokens"); ok {
					input = value + cached + creation
				}
			case "message_stop":
				ended = true
				return false
			}
			return true
		})
		if stopped {
			return
		}
		_ = ended
		if finish == "" {
			finish = "stop"
		}
		yield(chunk(newObj(), finish, newObjOf(
			"prompt_tokens", input,
			"prompt_tokens_details", newObjOf("cached_tokens", cached),
			"cache_creation_input_tokens", creation,
			"completion_tokens", output,
			"total_tokens", input+output,
		)), nil)
	}
}

// marshalObject encodes a value as JSON, with a default for a missing one.
func marshalObject(value any) string {
	if value == nil {
		return "{}"
	}
	return marshal(value)
}

// firstItem is list[0] or nil.
func firstItem(list []any) any {
	if len(list) == 0 {
		return nil
	}
	return list[0]
}

// anyHasKey reports whether any object in the list has the key.
func anyHasKey(list []any, key string) bool {
	for _, item := range list {
		if object := asObj(item); object != nil {
			if _, ok := object.Get(key); ok {
				return true
			}
		}
	}
	return false
}

// jsString is the text form of a value: a string as it is, a number or boolean
// written out.
func jsString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return trimFloat(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	}
	return marshal(value)
}

// trimFloat renders a JSON number in its shortest form.
func trimFloat(value float64) string {
	if value == math.Trunc(value) && math.Abs(value) < 1e21 {
		return marshalNumber(int64(value))
	}
	return marshal(value)
}

// marshalNumber renders an int64 as JSON.
func marshalNumber(value int64) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "0"
	}
	return string(data)
}

// splitDataURL splits "data:<media>;base64,<data>" into its parts.
func splitDataURL(url string) (media, data string, ok bool) {
	const prefix = "data:"
	if len(url) < len(prefix) || url[:len(prefix)] != prefix {
		return "", "", false
	}
	rest := url[len(prefix):]
	for i := 0; i+7 <= len(rest); i++ {
		if rest[i:i+7] == ";base64" {
			return rest[:i], rest[i+8:], true
		}
	}
	return "", "", false
}

// joinLines is blocks.map(text).join('\n') for the system prompt.
func joinLines(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, part := range parts[1:] {
		out += "\n" + part
	}
	return out
}

// joinStrings is array.join(”) for text parts.
func joinStrings(parts []string) string {
	out := ""
	for _, part := range parts {
		out += part
	}
	return out
}
