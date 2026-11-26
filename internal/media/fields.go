package media

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Reading one member of a request body: each reader applies the checks of one
// schema shape in order and records the first issue.

// str reads an optional string property.
func (f *fields) str(name string) *string {
	value, present := f.optional(name)
	if !present {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		f.fail(typeIssue(name, "string", value))
		return nil
	}
	return &text
}

// text reads an optional string with a length range.
func (f *fields) text(name string, minLen, maxLen int) *string {
	text := f.str(name)
	if text == nil {
		return nil
	}
	if found := textIssue(name, *text, minLen, maxLen); found != nil {
		f.fail(found)
		return nil
	}
	return text
}

// textOr is an optional bounded string with a default.
func (f *fields) textOr(name string, minLen, maxLen int, fallback string) string {
	if text := f.text(name, minLen, maxLen); text != nil {
		return *text
	}
	return fallback
}

// script reads a required string with a length range.
func (f *fields) script(name string, minLen, maxLen int) string {
	if _, present := f.optional(name); !present {
		f.fail(&issue{path: name, message: "Required"})
		return ""
	}
	text := f.str(name)
	if text == nil {
		return ""
	}
	if found := textIssue(name, *text, minLen, maxLen); found != nil {
		f.fail(found)
		return ""
	}
	return *text
}

// trimmed reads the way z.string().trim().min().max() does: spaces come off
// before the length is checked.
func (f *fields) trimmed(name string, minLen, maxLen int) string {
	if _, present := f.optional(name); !present {
		f.fail(&issue{path: name, message: "Required"})
		return ""
	}
	text := f.str(name)
	if text == nil {
		return ""
	}
	value := strings.TrimSpace(*text)
	if found := textIssue(name, value, minLen, maxLen); found != nil {
		f.fail(found)
		return ""
	}
	return value
}

// textIssue is the too-short / too-long message for a string.
func textIssue(path, value string, minLen, maxLen int) *issue {
	if length := utf16Length(value); length < minLen {
		return &issue{path: path, message: fmt.Sprintf("String must contain at least %d character(s)", minLen), dirty: true}
	}
	if maxLen > 0 && utf16Length(value) > maxLen {
		return &issue{path: path, message: fmt.Sprintf("String must contain at most %d character(s)", maxLen), dirty: true}
	}
	return nil
}

// enum reads an optional enum member.
func (f *fields) enum(name string, values []string) *string {
	value, present := f.optional(name)
	if !present {
		return nil
	}
	expected := "'" + strings.Join(values, "' | '") + "'"
	text, ok := value.(string)
	if !ok {
		f.fail(&issue{path: name, message: "Expected " + expected + ", received " + typeName(value), dirty: true})
		return nil
	}
	for _, allowed := range values {
		if text == allowed {
			return &text
		}
	}
	f.fail(&issue{path: name, message: "Invalid enum value. Expected " + expected + ", received '" + text + "'", dirty: true})
	return nil
}

// enumOr is an enum with a default.
func (f *fields) enumOr(name string, values []string, fallback string) string {
	if value := f.enum(name, values); value != nil {
		return *value
	}
	return fallback
}

// count reads an optional integer with a range (z.number().int().min().max());
// an unbounded side is passed as an infinity.
func (f *fields) count(name string, minValue, maxValue float64) *float64 {
	value, present := f.optional(name)
	if !present {
		return nil
	}
	number, found := numberValue(name, value)
	if found != nil {
		f.fail(found)
		return nil
	}
	if number != math.Trunc(number) {
		f.fail(&issue{path: name, message: "Expected integer, received float", dirty: true})
		return nil
	}
	if found := rangeIssue(name, number, minValue, maxValue); found != nil {
		f.fail(found)
		return nil
	}
	return &number
}

// positive is an integer above zero and at most max, with the "greater than 0"
// text.
func (f *fields) positive(name string, maxValue float64) *float64 {
	value, present := f.optional(name)
	if !present {
		return nil
	}
	number, found := numberValue(name, value)
	if found != nil {
		f.fail(found)
		return nil
	}
	if number != math.Trunc(number) {
		f.fail(&issue{path: name, message: "Expected integer, received float", dirty: true})
		return nil
	}
	if number <= 0 {
		f.fail(&issue{path: name, message: "Number must be greater than 0", dirty: true})
		return nil
	}
	if found := rangeIssue(name, number, math.Inf(-1), maxValue); found != nil {
		f.fail(found)
		return nil
	}
	return &number
}

// number reads an optional number with a range.
func (f *fields) number(name string, minValue, maxValue float64) *float64 {
	value, present := f.optional(name)
	if !present {
		return nil
	}
	number, found := numberValue(name, value)
	if found != nil {
		f.fail(found)
		return nil
	}
	if found := rangeIssue(name, number, minValue, maxValue); found != nil {
		f.fail(found)
		return nil
	}
	return &number
}

// coerce is a number in a range: the value is converted first, which makes null
// and true numbers, an empty string zero and a word NaN.
func (f *fields) coerce(name string, minValue, maxValue float64) *float64 {
	value, present := f.members[name]
	if !present {
		return nil
	}
	number, ok := coerceNumber(value)
	if !ok {
		f.fail(&issue{path: name, message: "Expected number, received nan"})
		return nil
	}
	if found := rangeIssue(name, number, minValue, maxValue); found != nil {
		f.fail(found)
		return nil
	}
	return &number
}

// coerceInt is a converted integer with a range and a default. The range is
// applied before the integer check.
func (f *fields) coerceInt(name string, minValue, maxValue, fallback int) int {
	value, present := f.members[name]
	if !present {
		return fallback
	}
	number, ok := coerceNumber(value)
	if !ok {
		f.fail(&issue{path: name, message: "Expected number, received nan"})
		return fallback
	}
	if found := rangeIssue(name, number, float64(minValue), float64(maxValue)); found != nil {
		f.fail(found)
		return fallback
	}
	if number != math.Trunc(number) {
		f.fail(&issue{path: name, message: "Expected integer, received float", dirty: true})
		return fallback
	}
	return int(number)
}

// coerceNumber converts a value to a number: the scalars a JSON body can carry
// convert, and the rest is NaN.
func coerceNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case float64:
		return v, true
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0, true
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		return number, true
	}
	return 0, false
}

// flag reads a boolean property with a default.
func (f *fields) flag(name string, fallback bool) bool {
	value, present := f.optional(name)
	if !present {
		return fallback
	}
	flag, ok := value.(bool)
	if !ok {
		f.fail(typeIssue(name, "boolean", value))
		return fallback
	}
	return flag
}

// pattern reads an optional string that must match re.
func (f *fields) pattern(name string, re *regexp.Regexp) string {
	text := f.str(name)
	if text == nil {
		return ""
	}
	if !re.MatchString(*text) {
		f.fail(&issue{path: name, message: "Invalid", dirty: true})
		return ""
	}
	return *text
}

// texts reads z.array(z.string().min().max()).min(minItems).max(items).
func (f *fields) texts(name string, minLen, maxLen, minItems, maxItems int) []string {
	value, present := f.optional(name)
	if !present {
		return []string{}
	}
	list, ok := value.([]any)
	if !ok {
		f.fail(typeIssue(name, "array", value))
		return []string{}
	}
	if found := arrayIssue(name, len(list), minItems, maxItems); found != nil {
		f.fail(found)
		return []string{}
	}
	out := make([]string, 0, len(list))
	for index, item := range list {
		path := name + "." + strconv.Itoa(index)
		text, isText := item.(string)
		if !isText {
			f.fail(&issue{path: path, message: "Invalid input"})
			return []string{}
		}
		if found := textIssue(path, text, minLen, maxLen); found != nil {
			f.fail(found)
			return []string{}
		}
		out = append(out, text)
	}
	return out
}

// arrayIssue is the too-short / too-long message for an array.
func arrayIssue(path string, length, minItems, maxItems int) *issue {
	if length < minItems {
		return &issue{path: path, message: fmt.Sprintf("Array must contain at least %d element(s)", minItems), dirty: true}
	}
	if maxItems > 0 && length > maxItems {
		return &issue{path: path, message: fmt.Sprintf("Array must contain at most %d element(s)", maxItems), dirty: true}
	}
	return nil
}

// numberValue is the number type check for one decoded JSON value.
func numberValue(path string, value any) (float64, *issue) {
	if number, ok := value.(float64); ok {
		return number, nil
	}
	return 0, typeIssue(path, "number", value)
}

// rangeIssue is the too-small / too-big message for a number.
func rangeIssue(path string, value, minValue, maxValue float64) *issue {
	if value < minValue {
		return &issue{path: path, message: "Number must be greater than or equal to " + formatNumber(minValue), dirty: true}
	}
	if value > maxValue {
		return &issue{path: path, message: "Number must be less than or equal to " + formatNumber(maxValue), dirty: true}
	}
	return nil
}
