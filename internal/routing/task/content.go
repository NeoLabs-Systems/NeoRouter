// Package task recognises what a chat request asks for: a mix of task kinds,
// the skills that mix calls on and how hard it looks. Rule-based and instant,
// with no LLM call; a learned classifier could replace Classify with the same
// result shape.
package task

import (
	"encoding/json"
	"strings"
	"unicode/utf16"
)

// Message is one entry of a canonical chat request, as the classifier reads it.
type Message struct {
	Role       string
	Content    any
	Name       string
	ToolCallID string
	ToolCalls  []ToolCall
}

// ToolCall is a tool invocation a model produced.
type ToolCall struct {
	ID       string
	Type     string
	Name     string // message.tool_calls[].name (the alternative shape)
	Function Function
}

// Function is a tool call's function member.
type Function struct {
	Name      string
	Arguments string
}

// Tool is one tool on offer.
type Tool struct {
	Type     string
	Function ToolFunction
}

// ToolFunction names and describes a tool (a bare {name, description} object is
// accepted too).
type ToolFunction struct {
	Name        string
	Description string
	Parameters  any
}

// ResponseFormat is request.response_format.
type ResponseFormat struct {
	Type string
}

// Request is the part of a canonical chat request the classifier reads.
type Request struct {
	Messages       []Message
	Tools          []Tool
	ResponseFormat *ResponseFormat
}

// Part is one content part of a multimodal message.
type Part struct {
	Type string
	Text string
}

// ContentParts is capabilities/chat/canonical.contentParts: a string is one
// text part, a list is itself, anything else has no parts.
func ContentParts(content any) []Part {
	switch value := content.(type) {
	case nil:
		return nil
	case string:
		return []Part{{Type: "text", Text: value}}
	case Part:
		return []Part{value}
	case []Part:
		return value
	case []any:
		parts := make([]Part, 0, len(value))
		for _, item := range value {
			if part, ok := partOf(item); ok {
				parts = append(parts, part)
			}
		}
		return parts
	case []map[string]any:
		parts := make([]Part, 0, len(value))
		for _, item := range value {
			if part, ok := partOf(item); ok {
				parts = append(parts, part)
			}
		}
		return parts
	}
	return nil
}

func partOf(item any) (Part, bool) {
	switch value := item.(type) {
	case Part:
		return value, true
	case map[string]any:
		part := Part{}
		if s, ok := value["type"].(string); ok {
			part.Type = s
		}
		if s, ok := value["text"].(string); ok {
			part.Text = s
		}
		return part, true
	}
	return Part{}, false
}

// textOf is canonical.textOf: a string as itself, otherwise the text parts
// joined.
func textOf(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	var b strings.Builder
	for _, part := range ContentParts(content) {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// IsImagePart reports the part types that make a request need vision.
func IsImagePart(part Part) bool {
	return part.Type == "image_url" || part.Type == "input_image" || part.Type == "image"
}

// utf16Len is the length in UTF-16 code units, which the classifier measures
// text with.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// lastRunes keeps the last n code points.
func lastRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// firstRunes keeps the first n code points.
func firstRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// marshalOrdered writes a JSON object whose keys keep insertion order.
type orderedMap struct {
	keys   []string
	values map[string]float64
}

func newOrderedMap() *orderedMap {
	return &orderedMap{values: map[string]float64{}}
}

func (m *orderedMap) set(key string, value float64) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

func (m *orderedMap) get(key string) (float64, bool) {
	v, ok := m.values[key]
	return v, ok
}

// add accumulates like `skills[skill] = (skills[skill] || 0) + value`.
func (m *orderedMap) add(key string, value float64) {
	current, _ := m.values[key]
	m.set(key, current+value)
}

func (m *orderedMap) len() int { return len(m.keys) }

// each walks the map in insertion order.
func (m *orderedMap) each(fn func(key string, value float64)) {
	for _, key := range m.keys {
		fn(key, m.values[key])
	}
}

// maxValue is the largest value in the map, at least 0.
func (m *orderedMap) maxValue() float64 {
	max := 0.0
	m.each(func(_ string, value float64) {
		if value > max {
			max = value
		}
	})
	return max
}

// MarshalJSON writes the entries in insertion order.
func (m *orderedMap) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, key := range m.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		v, err := json.Marshal(m.values[key])
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// jsonMarshal is encoding/json for the hand-built JSON writers.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
