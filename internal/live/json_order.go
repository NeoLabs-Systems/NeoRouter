package live

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// The live gateway rewrites the model name in a session's opening message and
// passes every other byte through untouched. An object's keys must stay in the
// order they were written; Go's encoding/json loses that order in a map, so the
// relay reads messages into this ordered form instead.
//
// Numbers keep the text the sender wrote (json.Number) rather than being
// re-formatted; strings and control characters are re-escaped like compact JSON
// does (short escapes for \n, \r, \t and \u00XX for the rest).
type object struct {
	keys   []string
	values map[string]any
}

func newObject() *object { return &object{values: map[string]any{}} }

// Set stores a value, keeping the key's position when it is already there.
func (o *object) Set(key string, value any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Get returns the value and whether the key is present (a JSON null is present).
func (o *object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	value, ok := o.values[key]
	return value, ok
}

// Has reports whether the key is present.
func (o *object) Has(key string) bool {
	_, ok := o.Get(key)
	return ok
}

// Len is the number of keys.
func (o *object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Object returns the object stored under key, nil when it is absent or not an
// object.
func (o *object) Object(key string) *object {
	value, _ := o.Get(key)
	nested, _ := value.(*object)
	return nested
}

// String returns the string stored under key, "" when it is absent or not a
// string.
func (o *object) String(key string) string {
	value, _ := o.Get(key)
	text, _ := value.(string)
	return text
}

// MarshalJSON writes the object with its keys in order.
func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	writeObject(&b, o)
	return b.Bytes(), nil
}

func writeObject(b *bytes.Buffer, o *object) {
	b.WriteByte('{')
	if o != nil {
		for index, key := range o.keys {
			if index > 0 {
				b.WriteByte(',')
			}
			writeJSONValue(b, key)
			b.WriteByte(':')
			writeJSONValue(b, o.values[key])
		}
	}
	b.WriteByte('}')
}

// writeJSONValue encodes one value without HTML escaping, as compact JSON does.
func writeJSONValue(b *bytes.Buffer, value any) {
	switch typed := value.(type) {
	case *object:
		writeObject(b, typed)
	case []any:
		b.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				b.WriteByte(',')
			}
			writeJSONValue(b, item)
		}
		b.WriteByte(']')
	default:
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(typed); err != nil {
			b.WriteString("null")
			return
		}
		b.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	}
}

// parseJSON reads one JSON value into the ordered form. A malformed document
// (bad syntax, trailing data) is an error, and the caller treats the message as
// unreadable.
func parseJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("live: trailing data after JSON value")
	}
	return value, nil
}

// parseObject reads a message as a JSON object; ok is false for anything else.
func parseObject(data []byte) (*object, bool) {
	value, err := parseJSON(data)
	if err != nil {
		return nil, false
	}
	parsed, ok := value.(*object)
	return parsed, ok
}

func decodeJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil
	}
	switch delim {
	case '{':
		out := newObject()
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("live: object key is not a string")
			}
			value, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			out.Set(key, value)
		}
		if _, err := decoder.Token(); err != nil { // the closing brace
			return nil, err
		}
		return out, nil
	case '[':
		out := []any{}
		for decoder.More() {
			value, err := decodeJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		if _, err := decoder.Token(); err != nil { // the closing bracket
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("live: unexpected JSON delimiter %q", delim)
}

// jsNumber reads a JSON number, 0 for anything that is not a number.
func jsNumber(value any) float64 {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return 0
		}
		return number
	case string:
		number, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return number
	}
	return 0
}

// orZero reads a missing, null or zero value as 0.
func orZero(value any, present bool) float64 {
	if !present {
		return 0
	}
	return jsNumber(value)
}

// coalesce returns the first value that is present and not null, 0 included.
func coalesce(o *object, first, second string) float64 {
	if value, ok := o.Get(first); ok && value != nil {
		return jsNumber(value)
	}
	if value, ok := o.Get(second); ok && value != nil {
		return jsNumber(value)
	}
	return 0
}
