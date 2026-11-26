package adapters

import (
	"net/http"
	"strconv"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// Reading decoded JSON the way the JS sources read plain objects. A provider
// listing or an error body is a shape a provider decides, so the code asks for
// members instead of unmarshaling into a struct. The parent package has the same
// readers for its own use; these are the media files' own.

// mediaObject reads a decoded JSON value as an object, or nil when it is not
// one (the parent package's equivalent is unexported).
func mediaObject(value any) map[string]any {
	if fields, ok := value.(map[string]any); ok {
		return fields
	}
	return nil
}

// mediaMember reads one member of a decoded JSON value.
func mediaMember(value any, name string) any { return mediaObject(value)[name] }

// mediaList reads a decoded JSON value as an array, or nil when it is not one.
func mediaList(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

// mediaStrings reads a decoded JSON array of strings.
func mediaStrings(value any) []string {
	items := mediaList(value)
	if items == nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// mediaFirst reads the first item of a decoded JSON array, or nil when empty.
func mediaFirst(value any) any {
	items := mediaList(value)
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

// mediaString is String(value ?? ”) for the scalars a listing carries.
func mediaString(value any) string {
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

// mediaTruthy is JavaScript truthiness: only null, false, zero and "" are
// falsy, and an empty array or object is truthy.
func mediaTruthy(value any) bool {
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

// mediaFirstTruthy is JavaScript's || chain.
func mediaFirstTruthy(values ...any) any {
	for _, value := range values {
		if mediaTruthy(value) {
			return value
		}
	}
	return nil
}

// mediaFirstNonNull is JavaScript's ?? chain.
func mediaFirstNonNull(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// mediaNumber reads an optional number.
func mediaNumber(value any) *float64 { return providers.ToNumber(value) }

// mediaFloats reads a JSON array of numbers, as an embedding is.
func mediaFloats(value any) []float64 {
	items := mediaList(value)
	out := make([]float64, len(items))
	for i, item := range items {
		if number := providers.ToNumber(item); number != nil {
			out[i] = *number
		}
	}
	return out
}

// mediaHeader builds a header set from alternating names and values.
func mediaHeader(pairs ...string) http.Header {
	headers := http.Header{}
	for i := 0; i+1 < len(pairs); i += 2 {
		headers.Set(pairs[i], pairs[i+1])
	}
	return headers
}

// mediaErrorText is the message of a failure as the JS adapter reported it.
func mediaErrorText(err error) string {
	if classified := providers.AsProviderError(err); classified != nil {
		return classified.Message
	}
	return err.Error()
}
