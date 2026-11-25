package gateway

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/llm/transport"
	"github.com/google/uuid"
)

// Obj is an insertion-ordered JSON object (llm/transport.Obj). The gateway
// builds every wire body and every canonical chat value as one, so a response
// carries the same keys in the same order the Node code wrote them.
type Obj = transport.Obj

// newObj is an empty ordered object.
func newObj() *Obj { return transport.NewObj() }

// marshal is JSON.stringify: compact, no HTML escaping.
func marshal(v any) string {
	data, err := transport.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(data)
}

// parseOrdered decodes JSON keeping object key order.
func parseOrdered(data []byte) (any, error) { return transport.ParseOrdered(data) }

// parseObject decodes a JSON object body into an ordered object.
func parseObject(data []byte) (*Obj, error) {
	value, err := transport.ParseOrdered(data)
	if err != nil {
		return nil, err
	}
	object, _ := value.(*Obj)
	if object == nil {
		object = newObj()
	}
	return object, nil
}

// obj copies a plain map into an ordered object (keys sorted).
func obj(m map[string]any) *Obj { return transport.FromMap(m) }

// asObj is value as an ordered object, nil when it is not one.
func asObj(value any) *Obj {
	object, _ := value.(*Obj)
	return object
}

// asList is value as an array, nil when it is not one.
func asList(value any) []any {
	list, _ := value.([]any)
	return list
}

// get reads a key; a missing key and an explicit null both count as absent.
func get(object *Obj, key string) (any, bool) {
	if object == nil {
		return nil, false
	}
	value, ok := object.Get(key)
	if !ok || value == nil {
		return nil, false
	}
	return value, true
}

// getObj is a nested object value.
func getObj(object *Obj, key string) *Obj {
	value, _ := get(object, key)
	return asObj(value)
}

// getList is an array value.
func getList(object *Obj, key string) []any {
	value, _ := get(object, key)
	return asList(value)
}

// getString is a string value, "" when absent or not a string.
func getString(object *Obj, key string) string {
	value, _ := get(object, key)
	text, _ := value.(string)
	return text
}

// getBool is a boolean value, false when absent or not a boolean.
func getBool(object *Obj, key string) bool {
	value, _ := get(object, key)
	flag, _ := value.(bool)
	return flag
}

// getNumber is a JSON number, ok false when absent or not a number.
func getNumber(object *Obj, key string) (float64, bool) {
	value, ok := get(object, key)
	if !ok {
		return 0, false
	}
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

// number is any value as a JSON number (numbers, numeric strings), the way
// JavaScript's Number() reads it; ok false when it is not finite.
func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// truthy is JavaScript truthiness for the values a parsed body holds.
func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0 && !math.IsNaN(v)
	case []any:
		return len(v) > 0
	case *Obj:
		return v != nil
	}
	return true
}

// nowSeconds is Math.floor(Date.now() / 1000).
func nowSeconds() int64 { return time.Now().Unix() }

// marshalAny is JSON.stringify for a value that must be written as it is.
func marshalAny(value any) string { return marshal(value) }

// stringPtr is a pointer to a value.
func stringPtr(value string) *string { return &value }

// joinComma is array.join(',').
func joinComma(values []string) string { return strings.Join(values, ",") }

// newRequestID is crypto.randomUUID().
func newRequestID() string { return uuid.NewString() }
