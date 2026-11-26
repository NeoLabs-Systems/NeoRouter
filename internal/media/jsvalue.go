package media

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// The JavaScript value semantics the ported code keeps: how zod names a value's
// type, how Number() converts one, how a numeric bound prints, how
// String.prototype.length counts, how JSON.stringify renders a body and which
// values are truthy.

// typeName names a decoded JSON value the way zod's messages do.
func typeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		if math.IsNaN(v) {
			return "nan"
		}
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "undefined"
}

// typeIssue is zod's invalid_type message.
func typeIssue(path, expected string, value any) *issue {
	return &issue{path: path, message: fmt.Sprintf("Expected %s, received %s", expected, typeName(value))}
}

// formatNumber prints a bound the way zod's message template does (0.25, 4).
func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// jsonText is JSON.stringify of a decoded value, for the size estimates.
func jsonText(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// utf16Length is String.prototype.length: the number of UTF-16 code units, not
// the number of runes.
func utf16Length(value string) int {
	length := 0
	for _, r := range value {
		if r >= 0x10000 {
			length += 2
			continue
		}
		length++
	}
	return length
}

// truthy is JavaScript truthiness of a decoded JSON value: only null, false,
// zero and "" are falsy.
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
