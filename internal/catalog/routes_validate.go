package catalog

import (
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// ModelMetrics are the routing-side numbers the models API adds to one model.
// modelRef parses a model reference the way JavaScript's Number() does: the
// query only matches a whole model id.
func modelRef(raw string) (int64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	ref := int64(value)
	if float64(ref) != value {
		return 0, false
	}
	return ref, true
}

// readObject reads a JSON object body, defaulting to an empty object when the
// request has no body.
func readObject(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	var body map[string]any
	if err := httpserver.ReadJSONManagement(w, r, &body, 2<<20); err != nil {
		return nil, err
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

// validationIssue is one rejected field, in zod's wording.
type validationIssue struct {
	path    string
	message string
}

// invalid builds the 400 VALIDATION_ERROR the validate middleware returns.
func invalid(issue validationIssue) error {
	message := issue.message
	fieldErrors := map[string]any{}
	if issue.path != "" {
		message = issue.path + ": " + issue.message
		fieldErrors[issue.path] = []string{issue.message}
	}
	return httpserver.New(400, "VALIDATION_ERROR", message, map[string]any{
		"formErrors":  []string{},
		"fieldErrors": fieldErrors,
	})
}

// typeName is zod's name for a decoded JSON value.
func typeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64, json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// validateModelPatch validates a PATCH /api/v1/models/{ref} body the way the
// zod schema in routes/models.js does, including its wording.
func validateModelPatch(body map[string]any) (ModelUpdate, error) {
	update := ModelUpdate{}
	if raw, present := body["enabled"]; present {
		flag, ok := raw.(bool)
		if !ok {
			return update, invalid(validationIssue{"enabled", "Expected boolean, received " + typeName(raw)})
		}
		update.Enabled = &flag
	}
	raw, present := body["overrides"]
	if !present {
		return update, nil
	}
	overrides, ok := raw.(map[string]any)
	if !ok {
		return update, invalid(validationIssue{"overrides", "Expected object, received " + typeName(raw)})
	}
	update.Overrides = overrides
	checks := []struct {
		key      string
		kind     string
		min, max *float64
		integer  bool
		enum     []string
	}{
		{key: "free", kind: "boolean"},
		{key: "priceIn", kind: "number", min: ptrFloat(0)},
		{key: "priceCachedIn", kind: "number", min: ptrFloat(0)},
		{key: "priceCacheWrite", kind: "number", min: ptrFloat(0)},
		{key: "priceOut", kind: "number", min: ptrFloat(0)},
		{key: "contextWindow", kind: "number", integer: true},
		{key: "tools", kind: "boolean"},
		{key: "vision", kind: "boolean"},
		{key: "intelligence", kind: "number", min: ptrFloat(0), max: ptrFloat(1)},
		{key: "priceUnit", kind: "enum", enum: []string{"image", "second", "minute", "mchar", "search", "call"}},
		{key: "unitPrice", kind: "number", min: ptrFloat(0), max: ptrFloat(1000)},
		{key: "billedAt", kind: "null"},
		{key: "noFreeTier", kind: "null"},
	}
	for _, check := range checks {
		value, present := overrides[check.key]
		if !present {
			continue
		}
		path := "overrides." + check.key
		// Every overridable field accepts null; for the evidence fields null
		// is the only accepted value, and it clears them.
		if value == nil {
			continue
		}
		switch check.kind {
		case "null":
			return update, invalid(validationIssue{path, "Expected null, received " + typeName(value)})
		case "boolean":
			if _, isBool := value.(bool); !isBool {
				return update, invalid(validationIssue{path, "Expected boolean, received " + typeName(value)})
			}
		case "enum":
			text, isText := value.(string)
			if !isText {
				return update, invalid(validationIssue{path, "Expected 'image' | 'second' | 'minute' | 'mchar' | 'search' | 'call', received " + typeName(value)})
			}
			valid := false
			for _, allowed := range check.enum {
				if text == allowed {
					valid = true
					break
				}
			}
			if !valid {
				return update, invalid(validationIssue{path,
					"Invalid enum value. Expected 'image' | 'second' | 'minute' | 'mchar' | 'search' | 'call', received '" + text + "'"})
			}
		case "number":
			number, isNumber := value.(float64)
			if !isNumber {
				return update, invalid(validationIssue{path, "Expected number, received " + typeName(value)})
			}
			if check.integer && number != math.Trunc(number) {
				return update, invalid(validationIssue{path, "Expected integer, received float"})
			}
			if check.min != nil && number < *check.min {
				if *check.min == 0 {
					return update, invalid(validationIssue{path, "Number must be greater than or equal to 0"})
				}
				return update, invalid(validationIssue{path, "Number must be greater than or equal to " + formatNumber(*check.min)})
			}
			if check.max != nil && number > *check.max {
				return update, invalid(validationIssue{path, "Number must be less than or equal to " + formatNumber(*check.max)})
			}
			if check.integer && number <= 0 {
				return update, invalid(validationIssue{path, "Number must be greater than 0"})
			}
		}
	}
	return update, nil
}

// validateBulk validates a POST /api/v1/models/bulk body.
func validateBulk(body map[string]any) (*string, bool, bool, error) {
	var accountID *string
	if raw, present := body["accountId"]; present {
		text, isText := raw.(string)
		if !isText {
			return nil, false, false, invalid(validationIssue{"accountId", "Expected string, received " + typeName(raw)})
		}
		accountID = &text
	}
	raw, present := body["enabled"]
	if !present {
		return nil, false, false, invalid(validationIssue{"enabled", "Required"})
	}
	enabled, isBool := raw.(bool)
	if !isBool {
		return nil, false, false, invalid(validationIssue{"enabled", "Expected boolean, received " + typeName(raw)})
	}
	onlyFree := false
	if raw, present := body["onlyFree"]; present {
		flag, isBool := raw.(bool)
		if !isBool {
			return nil, false, false, invalid(validationIssue{"onlyFree", "Expected boolean, received " + typeName(raw)})
		}
		onlyFree = flag
	}
	return accountID, enabled, onlyFree, nil
}

// formatNumber renders a number the way the validation messages do.
func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// Logger returns the routes' logger, slog.Default when unset.
func (rt *Routes) logger() *slog.Logger {
	if rt.Log == nil {
		return slog.Default()
	}
	return rt.Log
}
