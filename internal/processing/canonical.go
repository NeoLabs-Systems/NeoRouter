package processing

import (
	"bytes"
	"encoding/json"
)

// A canonical request is a JSON object whose "messages" key holds the message
// list, a dynamic shape: a message may carry fields this domain never looks at
// (name, tool_call_id, images, …) and every step here copies them along
// untouched.
//
// Messages are map[string]any rather than a struct because a provider payload
// is open-ended; only "content" and "tool_calls" are edited.

// Canonical is one request in canonical form.
type Canonical map[string]any

// Messages are the message objects of the request.
func (c Canonical) Messages() []map[string]any {
	raw, ok := c["messages"].([]any)
	if !ok {
		if typed, ok := c["messages"].([]map[string]any); ok {
			return typed
		}
		return nil
	}
	messages := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if message, ok := item.(map[string]any); ok {
			messages = append(messages, message)
		}
	}
	return messages
}

// WithMessages is a shallow copy of the request with another message list, like
// {...canonical, messages}.
func (c Canonical) WithMessages(messages []map[string]any) Canonical {
	next := make(Canonical, len(c)+1)
	for key, value := range c {
		next[key] = value
	}
	list := make([]any, 0, len(messages))
	for _, message := range messages {
		list = append(list, message)
	}
	next["messages"] = list
	return next
}

// Role is a message's role.
func messageRole(message map[string]any) string {
	role, _ := message["role"].(string)
	return role
}

// cloneMap copies one message object without deep-copying its values.
func cloneMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source)+2)
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

// mapMessageText runs transform over the texts of a message (its content, or the
// text parts of its content) and over its tool-call arguments, and returns a
// copy.
func mapMessageText(message map[string]any, transform func(string) string, argumentsTransform func(string) string) map[string]any {
	next := cloneMap(message)
	switch content := message["content"].(type) {
	case string:
		next["content"] = transform(content)
	case []any:
		parts := make([]any, len(content))
		for index, raw := range content {
			part, ok := raw.(map[string]any)
			if !ok {
				parts[index] = raw
				continue
			}
			if part["type"] != "text" {
				parts[index] = raw
				continue
			}
			text, ok := part["text"].(string)
			if !ok {
				parts[index] = raw
				continue
			}
			copied := cloneMap(part)
			copied["text"] = transform(text)
			parts[index] = copied
		}
		next["content"] = parts
	}
	if calls, ok := message["tool_calls"].([]any); ok {
		mapped := make([]any, len(calls))
		for index, raw := range calls {
			call, ok := raw.(map[string]any)
			if !ok {
				mapped[index] = raw
				continue
			}
			function, ok := call["function"].(map[string]any)
			if !ok {
				mapped[index] = raw
				continue
			}
			arguments, ok := function["arguments"].(string)
			if !ok {
				mapped[index] = raw
				continue
			}
			copiedFunction := cloneMap(function)
			if argumentsTransform != nil {
				copiedFunction["arguments"] = argumentsTransform(arguments)
			} else {
				copiedFunction["arguments"] = transform(arguments)
			}
			copiedCall := cloneMap(call)
			copiedCall["function"] = copiedFunction
			mapped[index] = copiedCall
		}
		next["tool_calls"] = mapped
	}
	return next
}

// mapArguments walks the strings inside a JSON document. Tool-call arguments are
// JSON, so a value spread over several lines is found inside the string as the
// person wrote it, not as its escaped form; arguments that are not whole JSON are
// changed as plain text.
func mapArguments(text string, transform func(string) string, transformRaw func(string) string) string {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return transformRaw(text)
	}
	switch parsed.(type) {
	case map[string]any, []any:
		encoded, err := marshalJSON(walkStrings(parsed, transform))
		if err != nil {
			return transformRaw(text)
		}
		return encoded
	}
	return transformRaw(text)
}

// walkStrings applies transform to every string in a decoded JSON document.
func walkStrings(node any, transform func(string) string) any {
	switch value := node.(type) {
	case string:
		return transform(value)
	case []any:
		out := make([]any, len(value))
		for index, item := range value {
			out[index] = walkStrings(item, transform)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = walkStrings(item, transform)
		}
		return out
	default:
		return node
	}
}

// marshalJSON encodes compactly: no HTML escaping and no trailing newline.
func marshalJSON(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return string(bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))), nil
}
