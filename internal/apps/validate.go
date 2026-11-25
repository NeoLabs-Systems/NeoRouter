package apps

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// flattenDetails is zod's error.flatten(), which validate.js sends as details.
type flattenDetails struct {
	FormErrors  []string            `json:"formErrors"`
	FieldErrors map[string][]string `json:"fieldErrors"`
}

// validationError is the 400 VALIDATION_ERROR server/middleware/validate.js
// answers with: `field: message` and zod's flattened issues as details.
func validationError(field, message string) *httpserver.HttpError {
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", field+": "+message, flattenDetails{
		FormErrors:  []string{},
		FieldErrors: map[string][]string{field: {message}},
	})
}

// zodTypeName is the type name zod prints for a received value.
func zodTypeName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	case '[':
		return "array"
	case '{':
		return "object"
	default:
		return "number"
	}
}

// scopeBody is the body of PUT /api/v1/apps/network. The raw value is kept so a
// missing scope reads "Required" and a wrong one reads like zod's enum error.
type scopeBody struct {
	Scope json.RawMessage `json:"scope"`
}

// parseScope validates {scope: 'local'|'network'} the way the route's
// validate(z.object({scope: z.enum(['local','network'])})) does.
func parseScope(raw json.RawMessage) (string, error) {
	const expected = "'local' | 'network'"
	switch kind := zodTypeName(raw); kind {
	case "":
		return "", validationError("scope", "Required")
	case "string":
		var scope string
		if err := json.Unmarshal(raw, &scope); err != nil {
			return "", validationError("scope", "Required")
		}
		if scope == "local" || scope == "network" {
			return scope, nil
		}
		return "", validationError("scope", fmt.Sprintf("Invalid enum value. Expected %s, received '%s'", expected, scope))
	default:
		return "", validationError("scope", fmt.Sprintf("Expected %s, received %s", expected, kind))
	}
}
