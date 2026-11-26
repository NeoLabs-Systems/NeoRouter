package gateway

// The OpenAI Responses dialect, both ways.

import (
	"iter"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// ResponsesTranslator is the OpenAI Responses dialect.
type ResponsesTranslator struct{}

// contentToParts is contentToParts: Responses input content as canonical content.
func contentToParts(content any) any {
	if text, ok := content.(string); ok {
		return text
	}
	parts := make([]any, 0, 2)
	allText := true
	for _, item := range asList(content) {
		part := asObj(item)
		switch getString(part, "type") {
		case "input_text", "output_text", "text":
			parts = append(parts, newObjOf("type", "text", "text", getString(part, "text")))
		case "input_image":
			url := getString(part, "image_url")
			if nested := getObj(part, "image_url"); nested != nil {
				url = getString(nested, "url")
			}
			if url == "" {
				continue
			}
			image := newObjOf("url", url)
			if detail, ok := get(part, "detail"); ok {
				image.Set("detail", detail)
			}
			parts = append(parts, newObjOf("type", "image_url", "image_url", image))
			allText = false
		}
	}
	if allText {
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			texts = append(texts, getString(asObj(part), "text"))
		}
		return joinStrings(texts)
	}
	return parts
}

// ToCanonical is toCanonical: a Responses request as a canonical one.
func (ResponsesTranslator) ToCanonical(body *Obj) (*Obj, error) {
	if _, ok := get(body, "previous_response_id"); ok {
		return nil, httpserver.New(400, "UNSUPPORTED_PARAMETER", "previous_response_id is not supported. Send the full conversation in `input`.")
	}
	messages := make([]any, 0, 4)
	if instructions, ok := get(body, "instructions"); ok && truthy(instructions) {
		messages = append(messages, newObjOf("role", "system", "content", jsString(instructions)))
	}
	items := getList(body, "input")
	if text, ok := getValue(body, "input").(string); ok {
		items = []any{newObjOf("role", "user", "content", text)}
	}
	for _, rawItem := range items {
		item := asObj(rawItem)
		kind := getString(item, "type")
		if kind == "" {
			kind = "message"
		}
		switch kind {
		case "message":
			role := getString(item, "role")
			if role == "developer" {
				role = "system"
			}
			messages = append(messages, newObjOf("role", role, "content", contentToParts(getValue(item, "content"))))
		case "function_call":
			id := getString(item, "call_id")
			if id == "" {
				id = getString(item, "id")
			}
			arguments := getString(item, "arguments")
			if arguments == "" {
				arguments = "{}"
			}
			call := newObjOf("id", id, "type", "function", "function", newObjOf("name", getString(item, "name"), "arguments", arguments))
			var last *Obj
			if len(messages) > 0 {
				last = asObj(messages[len(messages)-1])
			}
			if getString(last, "role") == "assistant" && len(getList(last, "tool_calls")) > 0 && !truthy(getValue(last, "content")) {
				last.Set("tool_calls", append(getList(last, "tool_calls"), call))
			} else {
				messages = append(messages, newObjOf("role", "assistant", "content", nil, "tool_calls", []any{call}))
			}
		case "function_call_output":
			output := getValue(item, "output")
			text, isText := output.(string)
			if !isText {
				text = marshal(output)
			}
			messages = append(messages, newObjOf("role", "tool", "tool_call_id", getString(item, "call_id"), "content", text))
		}
	}
	request := newObjOf("messages", messages, "stream", getBool(body, "stream"))
	if value, ok := getNumber(body, "max_output_tokens"); ok {
		request.Set("max_tokens", value)
	}
	for _, field := range []string{"temperature", "top_p", "prompt_cache_key", "user", "parallel_tool_calls"} {
		if value, ok := body.Get(field); ok && value != nil {
			request.Set(field, value)
		}
	}
	if effort := getString(getObj(body, "reasoning"), "effort"); effort != "" {
		request.Set("reasoning_effort", effort)
	}
	tools := make([]any, 0, 2)
	for _, rawTool := range getList(body, "tools") {
		tool := asObj(rawTool)
		if getString(tool, "type") != "function" {
			continue
		}
		parameters := getValue(tool, "parameters")
		if parameters == nil {
			parameters = newObjOf("type", "object", "properties", newObj())
		}
		function := newObjOf(
			"name", getString(tool, "name"),
			"description", getString(tool, "description"),
			"parameters", parameters,
		)
		if strict, ok := get(tool, "strict"); ok {
			function.Set("strict", strict)
		}
		tools = append(tools, newObjOf("type", "function", "function", function))
	}
	if len(tools) > 0 {
		request.Set("tools", tools)
	}
	if choice, ok := get(body, "tool_choice"); ok {
		if text, isString := choice.(string); isString {
			request.Set("tool_choice", text)
		} else if selected := asObj(choice); getString(selected, "type") == "function" {
			request.Set("tool_choice", newObjOf("type", "function", "function", newObjOf("name", getString(selected, "name"))))
		}
	}
	format := getObj(getObj(body, "text"), "format")
	switch getString(format, "type") {
	case "json_schema":
		schema := newObj()
		name := getString(format, "name")
		if name == "" {
			name = "response"
		}
		schema.Set("name", name)
		schema.Set("schema", getValue(format, "schema"))
		if strict, ok := get(format, "strict"); ok {
			schema.Set("strict", strict)
		}
		request.Set("response_format", newObjOf("type", "json_schema", "json_schema", schema))
	case "json_object":
		request.Set("response_format", newObjOf("type", "json_object"))
	}
	return request, nil
}

// usageOf is usageOf: a canonical usage block in the Responses shape.
func usageOf(usage *Obj) *Obj {
	normalized := NormalizeUsage(usage)
	if normalized == nil {
		normalized = newObjOf("prompt_tokens", 0, "completion_tokens", 0, "total_tokens", 0)
	}
	prompt, _ := getNumber(normalized, "prompt_tokens")
	completion, _ := getNumber(normalized, "completion_tokens")
	total, _ := getNumber(normalized, "total_tokens")
	cached, _ := getNumber(getObj(normalized, "prompt_tokens_details"), "cached_tokens")
	reasoning, _ := getNumber(getObj(normalized, "completion_tokens_details"), "reasoning_tokens")
	return newObjOf(
		"input_tokens", prompt,
		"output_tokens", completion,
		"total_tokens", total,
		"input_tokens_details", newObjOf("cached_tokens", cached),
		"output_tokens_details", newObjOf("reasoning_tokens", reasoning),
	)
}

// baseResponse is baseResponse: the skeleton both the plain and the streaming
// answer fill in.
func baseResponse(id, model string, createdAt int64, status string) *Obj {
	return newObjOf(
		"id", id,
		"object", "response",
		"created_at", createdAt,
		"status", status,
		"model", model,
		"output", []any{},
		"parallel_tool_calls", true,
		"tool_choice", "auto",
		"tools", []any{},
		"error", nil,
		"incomplete_details", nil,
	)
}

// finalizeResponse is finalize: the status, the usage and output_text.
func finalizeResponse(response *Obj, finishReason string, usage *Obj) *Obj {
	if finishReason == "length" {
		response.Set("status", "incomplete")
		response.Set("incomplete_details", newObjOf("reason", "max_output_tokens"))
	} else {
		response.Set("status", "completed")
	}
	response.Set("usage", usageOf(usage))
	var text string
	for _, rawItem := range getList(response, "output") {
		item := asObj(rawItem)
		if getString(item, "type") != "message" {
			continue
		}
		for _, rawPart := range getList(item, "content") {
			text += getString(asObj(rawPart), "text")
		}
	}
	response.Set("output_text", text)
	return response
}

// ResponseFromCanonical is responseFromCanonical: a canonical completion as a
// Responses object.
func (ResponsesTranslator) ResponseFromCanonical(completion *Obj, model string) *Obj {
	choice := asObj(firstItem(getList(completion, "choices")))
	message := getObj(choice, "message")
	response := baseResponse("resp_"+randomHex(12), model, nowSeconds(), "completed")
	reasoning := getString(message, "reasoning_content")
	if reasoning == "" {
		reasoning = getString(message, "reasoning")
	}
	if reasoning != "" {
		response.Set("output", append(getList(response, "output"), newObjOf(
			"type", "reasoning",
			"id", "rs_"+randomHex(12),
			"summary", []any{newObjOf("type", "summary_text", "text", reasoning)},
		)))
	}
	if text := TextOf(getValue(message, "content")); text != "" {
		response.Set("output", append(getList(response, "output"), newObjOf(
			"type", "message", "id", "msg_"+randomHex(12), "status", "completed", "role", "assistant",
			"content", []any{newObjOf("type", "output_text", "text", text, "annotations", []any{})},
		)))
	}
	for _, rawCall := range getList(message, "tool_calls") {
		call := asObj(rawCall)
		arguments := getString(getObj(call, "function"), "arguments")
		if arguments == "" {
			arguments = "{}"
		}
		response.Set("output", append(getList(response, "output"), newObjOf(
			"type", "function_call",
			"id", "fc_"+randomHex(12),
			"call_id", getString(call, "id"),
			"name", getString(getObj(call, "function"), "name"),
			"arguments", arguments,
			"status", "completed",
		)))
	}
	return finalizeResponse(response, getString(choice, "finish_reason"), getObj(completion, "usage"))
}

// StreamFromCanonical is streamFromCanonical: canonical chunks as the Responses
// event sequence.
func (ResponsesTranslator) StreamFromCanonical(chunks iter.Seq2[*Obj, error], model string, yield func(Event) bool) error {
	response := baseResponse("resp_"+randomHex(12), model, nowSeconds(), "in_progress")
	sequence := 0
	emit := func(kind string, payload *Obj) (Event, bool) {
		data := newObjOf("type", kind, "sequence_number", float64(sequence))
		sequence++
		setAll(data, payload)
		return Event{Event: kind, Data: data}, true
	}
	send := func(kind string, payload *Obj) bool {
		event, _ := emit(kind, payload)
		return yield(event)
	}
	var textItem *Obj
	textIndex := 0
	hasText := false
	type toolItem struct {
		item  *Obj
		index int
	}
	toolItems := map[int]*toolItem{}
	toolOrder := make([]int, 0, 2)
	finish := ""
	var usage *Obj

	startPayload := newObjOf("response", cloneWithOutput(response, []any{}))
	if !send("response.created", startPayload) {
		return nil
	}
	if !send("response.in_progress", startPayload) {
		return nil
	}
	closeText := func() bool {
		if !hasText {
			return true
		}
		part := asObj(firstItem(getList(textItem, "content")))
		if !send("response.output_text.done", newObjOf(
			"item_id", getString(textItem, "id"),
			"output_index", float64(textIndex),
			"content_index", 0,
			"text", getString(part, "text"),
		)) {
			return false
		}
		if !send("response.content_part.done", newObjOf(
			"item_id", getString(textItem, "id"),
			"output_index", float64(textIndex),
			"content_index", 0,
			"part", part,
		)) {
			return false
		}
		textItem.Set("status", "completed")
		if !send("response.output_item.done", newObjOf("output_index", float64(textIndex), "item", textItem)) {
			return false
		}
		hasText = false
		textItem = nil
		return true
	}

	for chunk, err := range chunks {
		if err != nil {
			return err
		}
		if raw := getValue(chunk, "usage"); truthy(raw) {
			usage = asObj(raw)
		}
		for _, rawChoice := range getList(chunk, "choices") {
			choice := asObj(rawChoice)
			delta := getObj(choice, "delta")
			if content := getString(delta, "content"); content != "" {
				if !hasText {
					item := newObjOf("type", "message", "id", "msg_"+randomHex(12), "status", "in_progress", "role", "assistant", "content", []any{})
					response.Set("output", append(getList(response, "output"), item))
					textItem = item
					textIndex = len(getList(response, "output")) - 1
					hasText = true
					if !send("response.output_item.added", newObjOf("output_index", float64(textIndex), "item", clone(item))) {
						return nil
					}
					item.Set("content", append(getList(item, "content"), newObjOf("type", "output_text", "text", "", "annotations", []any{})))
					if !send("response.content_part.added", newObjOf(
						"item_id", getString(item, "id"),
						"output_index", float64(textIndex),
						"content_index", 0,
						"part", newObjOf("type", "output_text", "text", "", "annotations", []any{}),
					)) {
						return nil
					}
				}
				part := asObj(getList(textItem, "content")[0])
				part.Set("text", getString(part, "text")+content)
				if !send("response.output_text.delta", newObjOf(
					"item_id", getString(textItem, "id"),
					"output_index", float64(textIndex),
					"content_index", 0,
					"delta", content,
				)) {
					return nil
				}
			}
			for _, rawCall := range getList(delta, "tool_calls") {
				call := asObj(rawCall)
				key := 0
				if position, ok := getNumber(call, "index"); ok {
					key = int(position)
				}
				entry, seen := toolItems[key]
				if !seen {
					if !closeText() {
						return nil
					}
					callID := getString(call, "id")
					if callID == "" {
						callID = "call_" + randomHex(8)
					}
					item := newObjOf(
						"type", "function_call",
						"id", "fc_"+randomHex(12),
						"call_id", callID,
						"name", getString(getObj(call, "function"), "name"),
						"arguments", "",
						"status", "in_progress",
					)
					response.Set("output", append(getList(response, "output"), item))
					entry = &toolItem{item: item, index: len(getList(response, "output")) - 1}
					toolItems[key] = entry
					toolOrder = append(toolOrder, key)
					if !send("response.output_item.added", newObjOf("output_index", float64(entry.index), "item", clone(item))) {
						return nil
					}
				}
				if args := getString(getObj(call, "function"), "arguments"); args != "" {
					entry.item.Set("arguments", getString(entry.item, "arguments")+args)
					if !send("response.function_call_arguments.delta", newObjOf(
						"item_id", getString(entry.item, "id"),
						"output_index", float64(entry.index),
						"delta", args,
					)) {
						return nil
					}
				}
			}
			if reason := getString(choice, "finish_reason"); reason != "" {
				finish = reason
			}
		}
	}
	if !closeText() {
		return nil
	}
	for _, key := range toolOrder {
		entry := toolItems[key]
		entry.item.Set("status", "completed")
		if !send("response.function_call_arguments.done", newObjOf(
			"item_id", getString(entry.item, "id"),
			"output_index", float64(entry.index),
			"arguments", getString(entry.item, "arguments"),
		)) {
			return nil
		}
		if !send("response.output_item.done", newObjOf("output_index", float64(entry.index), "item", entry.item)) {
			return nil
		}
	}
	finalizeResponse(response, finish, usage)
	kind := "response.completed"
	if getString(response, "status") == "incomplete" {
		kind = "response.incomplete"
	}
	send(kind, newObjOf("response", response))
	return nil
}

// cloneWithOutput is {...object, output: list}: a shallow copy with the output
// replaced (the events carry a response snapshot, as {...response} did).
func cloneWithOutput(object *Obj, output []any) *Obj {
	out := clone(object)
	out.Set("output", output)
	return out
}
