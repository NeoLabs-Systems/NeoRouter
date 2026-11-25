package providers

import (
	"strconv"
)

// The helpers here read decoded JSON the way the JS sources read plain objects.
// A provider listing or an error body is a shape a provider decides, so the code
// asks for members instead of unmarshaling into a struct.

// object reads a decoded JSON value as an object, or nil when it is not one.
func object(value any) map[string]any {
	if fields, ok := value.(map[string]any); ok {
		return fields
	}
	return nil
}

// member reads one member of a decoded JSON value (nil when it is not an object).
func member(value any, name string) any {
	return object(value)[name]
}

// list reads a decoded JSON value as an array, or nil when it is not one.
func list(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

// first reads the first item of a decoded JSON array (nil when empty).
func first(value any) any {
	items := list(value)
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

// truthy is JavaScript truthiness of a decoded JSON value: only null, false,
// zero and "" are falsy, and an empty array or object is truthy.
func truthy(value any) bool {
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

// firstNonNull is JavaScript's ?? chain: the first value that is not null.
func firstNonNull(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// firstTruthy is JavaScript's || chain: the first value that is truthy.
func firstTruthy(values ...any) any {
	for _, value := range values {
		if truthy(value) {
			return value
		}
	}
	return nil
}

// jsString is String(value) for the scalars a listing carries.
func jsString(value any) string {
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
