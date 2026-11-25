package proxies

// Request validation, ported from server/middleware/validate.js + the zod
// schemas in server/routes/proxies.js. A failure answers 400 VALIDATION_ERROR
// with zod's first issue message and ZodError.flatten() as details, so the
// Flutter client sees the same text and field tree as before.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// fieldErrors serializes in insertion order: zod fills fieldErrors in schema
// declaration order and the JSON body must keep that order.
type fieldErrors struct {
	keys   []string
	values map[string][]string
}

func (f *fieldErrors) add(key, message string) {
	if f.values == nil {
		f.values = map[string][]string{}
	}
	if _, ok := f.values[key]; !ok {
		f.keys = append(f.keys, key)
	}
	f.values[key] = append(f.values[key], message)
}

func (f *fieldErrors) len() int { return len(f.keys) }

func (f *fieldErrors) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range f.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(f.values[key])
		if err != nil {
			return nil, err
		}
		b.Write(name)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type validationDetails struct {
	FormErrors  []string     `json:"formErrors"`
	FieldErrors *fieldErrors `json:"fieldErrors"`
}

// validator collects zod-shaped issues for one request body.
type validator struct {
	form   []string
	fields fieldErrors
}

// objectBody decodes the request body into an object, answering body-parser's
// own errors (400 INVALID_JSON, 413 BODY_TOO_LARGE) through the foundation
// handlers. A missing or non-JSON body is an empty object, which is what
// express.json leaves in req.body before zod sees it.
func objectBody(w http.ResponseWriter, r *http.Request, maxBytes int64) (map[string]json.RawMessage, *validator, bool) {
	var raw json.RawMessage
	if err := httpserver.ReadJSONManagement(w, r, &raw, maxBytes); err != nil {
		return nil, nil, false
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return map[string]json.RawMessage{}, &validator{}, true
	}
	if trimmed[0] != '{' {
		return nil, &validator{form: []string{"Expected object, received " + jsonTypeName(trimmed)}}, true
	}
	object := map[string]json.RawMessage{}
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, &validator{form: []string{"Expected object, received unknown"}}, true
	}
	return object, &validator{}, true
}

// err is nil when the body is valid, otherwise the 400 the route returns. zod's
// message is "<field>: <first issue>", or the form issue for a non-object body.
func (v *validator) err() error {
	if len(v.form) == 0 && v.fields.len() == 0 {
		return nil
	}
	message := "The request is invalid."
	if len(v.form) > 0 {
		message = v.form[0]
	} else {
		field := v.fields.keys[0]
		message = field + ": " + v.fields.values[field][0]
	}
	formErrors := v.form
	if formErrors == nil {
		formErrors = []string{}
	}
	details := validationDetails{FormErrors: formErrors, FieldErrors: &v.fields}
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", message, details)
}

func (v *validator) issue(field, message string) { v.fields.add(field, message) }

func jsonTypeName(raw []byte) string {
	if len(raw) == 0 {
		return "undefined"
	}
	switch raw[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

func isString(raw json.RawMessage) (string, bool) {
	if jsonTypeName(raw) != "string" {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// requiredBool is zod's z.boolean().
func (v *validator) requiredBool(object map[string]json.RawMessage, field string, dst *bool) {
	if len(v.form) > 0 {
		return // zod reports the type failure alone
	}
	if _, ok := object[field]; !ok {
		v.issue(field, "Required")
		return
	}
	v.boolDefault(object, field, *dst, dst)
}

// boolDefault is zod's z.boolean().default(fallback).
func (v *validator) boolDefault(object map[string]json.RawMessage, field string, fallback bool, dst *bool) {
	if len(v.form) > 0 {
		return // zod reports the type failure alone
	}
	raw, ok := object[field]
	if !ok {
		*dst = fallback
		return
	}
	if jsonTypeName(raw) != "boolean" {
		v.issue(field, "Expected boolean, received "+jsonTypeName(raw))
		return
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		v.issue(field, "Expected boolean, received "+jsonTypeName(raw))
		return
	}
	*dst = value
}

// requiredString is zod's z.string().max(max).
func (v *validator) requiredString(object map[string]json.RawMessage, field string, max int, dst *string) {
	if len(v.form) > 0 {
		return // zod reports the type failure alone
	}
	raw, ok := object[field]
	if !ok {
		v.issue(field, "Required")
		return
	}
	value, ok := isString(raw)
	if !ok {
		v.issue(field, "Expected string, received "+jsonTypeName(raw))
		return
	}
	if max > 0 && len([]rune(value)) > max {
		v.issue(field, fmt.Sprintf("String must contain at most %d character(s)", max))
		return
	}
	*dst = value
}

// requiredEnum is zod's z.enum([...]) without a default.
func (v *validator) requiredEnum(object map[string]json.RawMessage, field string, allowed []string, dst *string) {
	if len(v.form) > 0 {
		return // zod reports the type failure alone
	}
	if _, ok := object[field]; !ok {
		v.issue(field, "Required")
		return
	}
	v.enumDefault(object, field, allowed, *dst, dst)
}

// enumDefault is zod's z.enum([...]).default(fallback).
func (v *validator) enumDefault(object map[string]json.RawMessage, field string, allowed []string, fallback string, dst *string) {
	if len(v.form) > 0 {
		return // zod reports the type failure alone
	}
	raw, ok := object[field]
	if !ok {
		*dst = fallback
		return
	}
	value, ok := isString(raw)
	if !ok {
		v.issue(field, "Expected '"+strings.Join(allowed, "' | '")+"', received "+jsonTypeName(raw))
		return
	}
	for _, option := range allowed {
		if value == option {
			*dst = value
			return
		}
	}
	v.issue(field, "Invalid enum value. Expected '"+strings.Join(allowed, "' | '")+"', received '"+value+"'")
}

// stringArray is zod's z.array(z.string().max(itemMax)).max(max), defaulting to
// an empty list.
func (v *validator) stringArray(object map[string]json.RawMessage, field string, max, itemMax int, dst *[]string) {
	if len(v.form) > 0 {
		return // zod reports the type failure alone
	}
	raw, ok := object[field]
	if !ok {
		*dst = []string{}
		return
	}
	if jsonTypeName(raw) != "array" {
		v.issue(field, "Expected array, received "+jsonTypeName(raw))
		return
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		v.issue(field, "Expected array, received "+jsonTypeName(raw))
		return
	}
	if max > 0 && len(values) > max {
		v.issue(field, fmt.Sprintf("Array must contain at most %d element(s)", max))
		return
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		value, ok := isString(item)
		if !ok {
			v.issue(field, "Expected string, received "+jsonTypeName(item))
			return
		}
		if itemMax > 0 && len([]rune(value)) > itemMax {
			v.issue(field, fmt.Sprintf("String must contain at most %d character(s)", itemMax))
			return
		}
		out = append(out, value)
	}
	*dst = out
}

// requiredURL is zod's z.string().url().max(max).
func (v *validator) requiredURL(object map[string]json.RawMessage, field string, max int, dst *string) {
	if len(v.form) > 0 {
		return // zod reports the type failure alone
	}
	raw, ok := object[field]
	if !ok {
		v.issue(field, "Required")
		return
	}
	value, ok := isString(raw)
	if !ok {
		v.issue(field, "Expected string, received "+jsonTypeName(raw))
		return
	}
	if max > 0 && len([]rune(value)) > max {
		v.issue(field, fmt.Sprintf("String must contain at most %d character(s)", max))
		return
	}
	if !isAbsoluteURL(value) {
		v.issue(field, "Invalid url")
		return
	}
	*dst = value
}

// isAbsoluteURL reports whether raw is a URL with a scheme and a host, which is
// what zod's url() regex requires.
func isAbsoluteURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme != "" && parsed.Host != ""
}
