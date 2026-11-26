package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// body is a decoded request body: the raw JSON value of each field.
type body map[string]json.RawMessage

// issue is one validation failure: a path and the message for it.
type issue struct {
	path    []string
	message string
}

// validation collects issues so a rejected request gets a 400 with the first
// issue's message and the flattened issues as details.
type validation struct {
	body   body
	issues []issue
}

func (v *validation) add(path []string, message string) {
	v.issues = append(v.issues, issue{path: path, message: message})
}

// fieldError is one entry of flatten().fieldErrors, in first-seen order.
type fieldError struct {
	field    string
	messages []string
}

// orderedFieldErrors marshals as a JSON object that keeps the order the fields
// were seen in.
type orderedFieldErrors []fieldError

func (f orderedFieldErrors) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for index, entry := range f {
		if index > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(entry.field)
		if err != nil {
			return nil, err
		}
		messages, err := json.Marshal(entry.messages)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(messages)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// zodFlatten groups the issues into form errors and field errors.
type zodFlatten struct {
	FormErrors  []string           `json:"formErrors"`
	FieldErrors orderedFieldErrors `json:"fieldErrors"`
}

// err builds the 400 VALIDATION_ERROR the validate middleware raises, or nil.
func (v *validation) err() error {
	if len(v.issues) == 0 {
		return nil
	}
	first := v.issues[0]
	message := first.message
	if len(first.path) > 0 {
		message = strings.Join(first.path, ".") + ": " + message
	}
	ordered := make(orderedFieldErrors, 0, len(v.issues))
	position := make(map[string]int, len(v.issues))
	for _, item := range v.issues {
		if len(item.path) == 0 {
			continue
		}
		field := item.path[0]
		index, seen := position[field]
		if !seen {
			ordered = append(ordered, fieldError{field: field})
			index = len(ordered) - 1
			position[field] = index
		}
		ordered[index].messages = append(ordered[index].messages, item.message)
	}
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", message,
		zodFlatten{FormErrors: []string{}, FieldErrors: ordered})
}

// readBody decodes a management request body; a failure has been answered.
func readBody(w http.ResponseWriter, r *http.Request) (body, bool) {
	decoded := body{}
	if err := httpserver.ReadJSONManagement(w, r, &decoded, httpserver.DefaultJSONBodyLimit); err != nil {
		return nil, false
	}
	return decoded, true
}

// jsonKind names a JSON value the way the validation messages do.
func jsonKind(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case len(trimmed) == 0:
		return "undefined"
	case trimmed[0] == '"':
		return "string"
	case trimmed[0] == '{':
		return "object"
	case trimmed[0] == '[':
		return "array"
	}
	switch string(trimmed) {
	case "true", "false":
		return "boolean"
	case "null":
		return "null"
	}
	return "number"
}

func decodeString(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// emailPattern is the email regex, minus the leading-dot and double-dot
// lookaheads RE2 cannot express; isEmail checks those two separately.
var emailPattern = regexp.MustCompile(`^[A-Za-z0-9_'+\-.]*[A-Za-z0-9_+\-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$`)

// isEmail is z.string().email().
func isEmail(value string) bool {
	if strings.HasPrefix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	return emailPattern.MatchString(value)
}

// str reads a required string field with min/max character bounds; a negative
// bound means unbounded.
func (v *validation) str(name string, min, max int) string {
	raw, ok := v.body[name]
	if !ok {
		v.add([]string{name}, "Required")
		return ""
	}
	if kind := jsonKind(raw); kind != "string" {
		v.add([]string{name}, "Expected string, received "+kind)
		return ""
	}
	value, _ := decodeString(raw)
	v.bounds(name, value, min, max)
	return value
}

func (v *validation) bounds(name, value string, min, max int) bool {
	length := jsLen(value)
	if min >= 0 && length < min {
		v.add([]string{name}, fmt.Sprintf("String must contain at least %d character(s)", min))
		return false
	}
	if max >= 0 && length > max {
		v.add([]string{name}, fmt.Sprintf("String must contain at most %d character(s)", max))
		return false
	}
	return true
}

// email reads a required email field.
func (v *validation) email(name string) string {
	raw, ok := v.body[name]
	if !ok {
		v.add([]string{name}, "Required")
		return ""
	}
	if kind := jsonKind(raw); kind != "string" {
		v.add([]string{name}, "Expected string, received "+kind)
		return ""
	}
	value, _ := decodeString(raw)
	if !isEmail(value) {
		v.add([]string{name}, "Invalid email")
	}
	return value
}

// strOptional reads an optional string field: absent is fine, null is not.
func (v *validation) strOptional(name string, max int) *string {
	raw, ok := v.body[name]
	if !ok {
		return nil
	}
	if kind := jsonKind(raw); kind != "string" {
		v.add([]string{name}, "Expected string, received "+kind)
		return nil
	}
	value, _ := decodeString(raw)
	v.bounds(name, value, -1, max)
	return &value
}

// emailOptional reads an optional email field.
func (v *validation) emailOptional(name string) *string {
	raw, ok := v.body[name]
	if !ok {
		return nil
	}
	if kind := jsonKind(raw); kind != "string" {
		v.add([]string{name}, "Expected string, received "+kind)
		return nil
	}
	value, _ := decodeString(raw)
	if !isEmail(value) {
		v.add([]string{name}, "Invalid email")
	}
	return &value
}
