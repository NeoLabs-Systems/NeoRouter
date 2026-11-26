package settings

import (
	"bytes"
	"encoding/json"
)

// jsonNull and jsonObject are the fallbacks for stored JSON: null for free-form
// values, {} for stored option objects.
var (
	jsonNull   = json.RawMessage("null")
	jsonObject = json.RawMessage("{}")
)

// jsonOr returns the parsed value, or the fallback when the text is empty or is
// not JSON.
func jsonOr(text *string, fallback json.RawMessage) json.RawMessage {
	if text == nil || *text == "" {
		return fallback
	}
	if json.Valid([]byte(*text)) {
		return json.RawMessage(*text)
	}
	return fallback
}

// jsonOrSelf keeps an unparsable value as the string it was, not as null.
func jsonOrSelf(text string) json.RawMessage {
	if text != "" && json.Valid([]byte(text)) {
		return json.RawMessage(text)
	}
	return marshalString(text)
}

// marshalString encodes s without HTML escaping.
func marshalString(s string) json.RawMessage {
	// A string always encodes; the error branch of marshalNoEscape cannot fire.
	encoded, _ := marshalNoEscape(s)
	return encoded
}

// marshalNoEscape encodes v without escaping <, > and &, and without the
// trailing newline json.Encoder adds, so the output is compact.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// orderedObject is a JSON object whose keys keep insertion order; a Go map
// would sort them. Values are raw JSON, so a stored document passes through
// with its own key order intact.
type orderedObject struct {
	keys   []string
	values map[string]json.RawMessage
}

func newOrderedObject() *orderedObject {
	return &orderedObject{values: map[string]json.RawMessage{}}
}

// set adds or replaces a key; a replaced key keeps its original position.
func (o *orderedObject) set(key string, value json.RawMessage) {
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	if len(value) == 0 {
		value = jsonNull
	}
	o.values[key] = value
}

// remove deletes a key and reports what it held.
func (o *orderedObject) remove(key string) (json.RawMessage, bool) {
	value, exists := o.values[key]
	if !exists {
		return nil, false
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	return value, true
}

func (o *orderedObject) len() int { return len(o.keys) }

// MarshalJSON writes the members in insertion order.
func (o *orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encoded, err := marshalNoEscape(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encoded)
		buf.WriteByte(':')
		value := o.values[key]
		if len(value) == 0 {
			value = jsonNull
		}
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// parseOrderedObject parses one JSON object and keeps its key order. It fails
// for anything that is not an object.
func parseOrderedObject(raw []byte) (*orderedObject, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, false
	}
	out := newOrderedObject()
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		out.set(key, value)
	}
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	return out, true
}

// jsonTypeName names a JSON value as "Expected X, received Y" messages do.
func jsonTypeName(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) == 0:
		return "undefined"
	case raw[0] == '{':
		return "object"
	case raw[0] == '[':
		return "array"
	case raw[0] == '"':
		return "string"
	case bytes.Equal(raw, []byte("true")), bytes.Equal(raw, []byte("false")):
		return "boolean"
	case bytes.Equal(raw, []byte("null")):
		return "null"
	default:
		return "number"
	}
}

// jsTruthy reports whether a JSON value is truthy in JavaScript: null, false,
// "", 0 and NaN are not, every object and array is.
func jsTruthy(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	switch jsonTypeName(trimmed) {
	case "null", "undefined":
		return false
	case "boolean":
		return bytes.Equal(trimmed, []byte("true"))
	case "string":
		return !bytes.Equal(trimmed, []byte(`""`))
	case "number":
		var number float64
		if err := json.Unmarshal(trimmed, &number); err != nil {
			return false
		}
		return number != 0
	default:
		return true
	}
}

// utf16Length is the length in UTF-16 code units, which the maximum lengths
// count.
func utf16Length(s string) int {
	units := 0
	for _, r := range s {
		if r > 0xFFFF {
			units += 2
			continue
		}
		units++
	}
	return units
}
