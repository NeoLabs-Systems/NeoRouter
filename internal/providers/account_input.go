package providers

// The request and response types of the providers API and the validation its
// bodies get (server/routes/providers.js: the zod schemas and accountInput).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/google/uuid"
)

// Actor is the signed-in person acting through the API (providers.js actorOf):
// the checks on where a key may be sent need their role and whether they own
// the space. It is nil for trusted local callers such as the CLI.
type Actor struct {
	Role    string
	IsOwner bool
}

// Optional is an optional request field: Set says the client sent it, Value is
// what it sent (nil when the client sent null). The account API tells "leave it
// alone" from "clear it" this way.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON records that the field was present, then its value.
func (f *Optional[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	if string(data) == "null" {
		f.Value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = &value
	return nil
}

// Input is one create or update body of the account API (the accountInput
// schema). The same type serves both: a create needs DefinitionID, a patch
// leaves whatever it does not send alone.
type Input struct {
	DefinitionID   Optional[string]            `json:"definitionId"`
	Label          Optional[string]            `json:"label"`
	Slug           Optional[string]            `json:"slug"`
	Secret         Optional[string]            `json:"secret"`
	BaseURL        Optional[string]            `json:"baseUrl"`
	Options        Optional[map[string]any]    `json:"options"`
	Headers        Optional[map[string]string] `json:"headers"`
	BillingMode    Optional[string]            `json:"billingMode"`
	Classification Optional[string]            `json:"classification"`
	OverflowPolicy Optional[string]            `json:"overflowPolicy"`
	SlotWaitMS     Optional[float64]           `json:"slotWaitMs"`
	Concurrent     Optional[float64]           `json:"concurrent"`
	// Limits are the limits the person states, by metric. A metric present with
	// a nil value is cleared; an absent metric stays as it is.
	Limits  map[string]*float64 `json:"limits"`
	Enabled Optional[bool]      `json:"enabled"`
}

// Created is one new connection with the key check that accepted it.
type Created struct {
	Account    PublicAccount `json:"account"`
	Validation Validation    `json:"validation"`
}

// ProbedModel is one model a preview found.
type ProbedModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	FreeHint      bool   `json:"freeHint"`
	ContextWindow *int   `json:"contextWindow"`
}

// Probed is what checking credentials without saving anything found.
type Probed struct {
	Validation  Validation    `json:"validation"`
	Models      []ProbedModel `json:"models"`
	ModelsError *string       `json:"modelsError"`
}

// limitedMetrics are the limits a person may state for an account, in the order
// account_limits.js writes them.
var limitedMetrics = []string{"rpm", "rpd", "tpm", "tpd", "concurrent"}

// maxLimit is the largest limit the API accepts.
const maxLimit = 10_000_000_000

// ValidateInput checks one create or update body the way the zod schemas do,
// answering 400 VALIDATION_ERROR with the first issue's text
// (name: message), like the JS validate middleware.
func (s *Service) ValidateInput(in Input, creating bool) error {
	maxWaitMS := s.config().Routing.SlotMaxWait.Milliseconds()
	if creating {
		if err := requiredText("definitionId", in.DefinitionID); err != nil {
			return err
		}
	}
	if err := optionalText("label", in.Label, 80); err != nil {
		return err
	}
	if err := optionalText("slug", in.Slug, 40); err != nil {
		return err
	}
	if err := optionalText("secret", in.Secret, 4096); err != nil {
		return err
	}
	if err := optionalText("baseUrl", in.BaseURL, 2048); err != nil {
		return err
	}
	if err := validateOptions(in.Options); err != nil {
		return err
	}
	if in.Headers.Set && in.Headers.Value != nil {
		for name, value := range *in.Headers.Value {
			if utf16Len(value) > 2048 {
				return validationError("headers." + name + ": String must contain at most 2048 character(s)")
			}
		}
	}
	if err := optionalEnum("billingMode", in.BillingMode, BillingModes); err != nil {
		return err
	}
	if err := optionalEnum("classification", in.Classification, []BillingMode{"auto", "free", "paid"}); err != nil {
		return err
	}
	if err := optionalEnum("overflowPolicy", in.OverflowPolicy, []BillingMode{"auto", "overflow", "queue"}); err != nil {
		return err
	}
	if in.SlotWaitMS.Set && in.SlotWaitMS.Value != nil {
		if err := wholeNumber("slotWaitMs", *in.SlotWaitMS.Value); err != nil {
			return err
		}
		value := *in.SlotWaitMS.Value
		if value < 0 {
			return validationError("slotWaitMs: Number must be greater than or equal to 0")
		}
		if maxWaitMS > 0 && value > float64(maxWaitMS) {
			return validationError(fmt.Sprintf("slotWaitMs: Number must be less than or equal to %d", maxWaitMS))
		}
	}
	if err := optionalLimit("concurrent", in.Concurrent); err != nil {
		return err
	}
	for _, metric := range limitedMetrics {
		value, ok := in.Limits[metric]
		if !ok || value == nil {
			continue
		}
		if err := optionalLimit("limits."+metric, Optional[float64]{Set: true, Value: value}); err != nil {
			return err
		}
	}
	for metric := range in.Limits {
		known := false
		for _, candidate := range limitedMetrics {
			if candidate == metric {
				known = true
				break
			}
		}
		if !known {
			return validationError("limits: Unrecognized key(s) in object: '" + metric + "'")
		}
	}
	return nil
}

// validateOptions checks the options object: the account settings a provider
// definition names, plus headers, manualModels and modelsPath.
func validateOptions(field Optional[map[string]any]) error {
	if !field.Set || field.Value == nil {
		return nil
	}
	for name, value := range *field.Value {
		switch name {
		case "manualModels":
			list, ok := value.([]any)
			if !ok {
				return validationError("options.manualModels: Expected array, received " + jsTypeName(value))
			}
			if len(list) > 500 {
				return validationError("options.manualModels: Array must contain at most 500 element(s)")
			}
			for _, item := range list {
				text, ok := item.(string)
				if !ok {
					return validationError("options.manualModels: Expected string, received " + jsTypeName(item))
				}
				if len(text) == 0 {
					return validationError("options.manualModels: String must contain at least 1 character(s)")
				}
				if utf16Len(text) > 300 {
					return validationError("options.manualModels: String must contain at most 300 character(s)")
				}
			}
		case "authHeader":
			text, ok := value.(string)
			if !ok {
				return validationError("options.authHeader: Expected string, received " + jsTypeName(value))
			}
			if !authHeaderPattern.MatchString(text) {
				return validationError("options.authHeader: Invalid")
			}
		case "modelsPath":
			text, ok := value.(string)
			if !ok {
				return validationError("options.modelsPath: Expected string, received " + jsTypeName(value))
			}
			if utf16Len(text) > 512 {
				return validationError("options.modelsPath: String must contain at most 512 character(s)")
			}
			if text != "" && !modelsPathOnBase(text) {
				return validationError("options.modelsPath: The models path must start with /")
			}
		default:
			text, ok := value.(string)
			if !ok {
				return validationError("options." + name + ": Expected string, received " + jsTypeName(value))
			}
			if utf16Len(text) > 512 {
				return validationError("options." + name + ": String must contain at most 512 character(s)")
			}
		}
	}
	return nil
}

// authHeaderPattern is the header name a key may be sent in.
var authHeaderPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// requiredText checks a text that must be there and not be empty.
func requiredText(name string, field Optional[string]) error {
	if !field.Set || field.Value == nil {
		return validationError(name + ": Required")
	}
	if *field.Value == "" {
		return validationError(name + ": String must contain at least 1 character(s)")
	}
	return nil
}

// optionalText checks a text that may be absent, with its maximum length.
func optionalText(name string, field Optional[string], max int) error {
	if !field.Set || field.Value == nil {
		return nil
	}
	if limit := utf16Len(*field.Value); limit > max {
		return validationError(fmt.Sprintf("%s: String must contain at most %d character(s)", name, max))
	}
	return nil
}

// optionalEnum checks a value that must be one of a closed list.
func optionalEnum(name string, field Optional[string], allowed []BillingMode) error {
	if !field.Set || field.Value == nil {
		return nil
	}
	for _, candidate := range allowed {
		if string(candidate) == *field.Value {
			return nil
		}
	}
	quoted := make([]string, 0, len(allowed))
	for _, candidate := range allowed {
		quoted = append(quoted, "'"+string(candidate)+"'")
	}
	return validationError(fmt.Sprintf("%s: Invalid enum value. Expected %s, received '%s'", name, strings.Join(quoted, " | "), *field.Value))
}

// optionalLimit checks one limit: a whole number from 1 up, or null.
func optionalLimit(name string, field Optional[float64]) error {
	if !field.Set || field.Value == nil {
		return nil
	}
	if err := wholeNumber(name, *field.Value); err != nil {
		return err
	}
	value := *field.Value
	if value < 1 {
		return validationError(name + ": Number must be greater than or equal to 1")
	}
	if value > maxLimit {
		return validationError(fmt.Sprintf("%s: Number must be less than or equal to %d", name, maxLimit))
	}
	return nil
}

// wholeNumber checks that a number is an integer.
func wholeNumber(name string, value float64) error {
	if value != float64(int64(value)) {
		return validationError(name + ": Expected integer, received float")
	}
	return nil
}

// validationError is the 400 the JS validate middleware answers.
func validationError(message string) error {
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", message)
}

// jsTypeName is the type name a zod message prints for a JSON value.
func jsTypeName(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case float64, int, int64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	}
	return "unknown"
}

// utf16Len is JavaScript's String.prototype.length: UTF-16 code units.
func utf16Len(value string) int { return len(utf16.Encode([]rune(value))) }

// boolInt is a boolean stored in an INTEGER column.
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// nullableEmpty is NULL for an empty text.
func nullableEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullableString is NULL for a missing or empty text.
func nullableString(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

// stringField reads an optional string field, "" when it is absent or null.
func stringField(field Optional[string]) string {
	if field.Value == nil {
		return ""
	}
	return *field.Value
}

// stringValue reads an optional string, "" when it is nil.
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// firstNonEmpty is JavaScript's `a || b`.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// valueOr reads an optional value with a default.
func valueOr[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}

// headerMap reads the extra request headers as a plain map.
func headerMap(field Optional[map[string]string]) map[string]any {
	out := map[string]any{}
	for name, value := range valueOr(field.Value, map[string]string{}) {
		out[name] = value
	}
	return out
}

// cloneOptions copies an account's options so a patch cannot mutate the stored
// map through a shared reference.
func cloneOptions(options map[string]any) map[string]any {
	out := make(map[string]any, len(options))
	for name, value := range options {
		out[name] = value
	}
	return out
}

// slugify is account_service.slugify: lowercase letters, digits and dashes,
// at most 40 of them.
func slugify(value string) string {
	lowered := strings.ToLower(value)
	replaced := slugNonAlnum.ReplaceAllString(lowered, "-")
	trimmed := strings.Trim(replaced, "-")
	if len(trimmed) > 40 {
		trimmed = trimmed[:40]
	}
	if trimmed == "" {
		return "provider"
	}
	return trimmed
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// isHTTPScheme reports whether a scheme names a network URL that needs a host.
func isHTTPScheme(scheme string) bool { return scheme == "http" || scheme == "https" }

// maskSecret is account_service.maskSecret: the first and last four characters,
// never the key itself.
func maskSecret(secret string) *string {
	if secret == "" {
		return nil
	}
	units := utf16.Encode([]rune(secret))
	if len(units) <= 8 {
		masked := "••••"
		return &masked
	}
	head := string(utf16.Decode(units[:4]))
	tail := string(utf16.Decode(units[len(units)-4:]))
	masked := head + "••••" + tail
	return &masked
}

// newAccountID is the id of a new connection (crypto.randomUUID).
func newAccountID() string { return uuid.NewString() }

// modelsPathOnBase reports whether a models path stays on the base URL: one
// leading slash, so it can never be another host's URL.
func modelsPathOnBase(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//")
}

// assertSlotPlan refuses a concurrency plan the provider does not sell.
func assertSlotPlan(definition Definition, options map[string]any) error {
	plan, _ := options["slotPlan"].(string)
	if plan == "" || definition.Billing.FlatRate == nil {
		return nil
	}
	for _, candidate := range definition.Billing.FlatRate.Plans {
		if candidate.ID == plan {
			return nil
		}
	}
	return httpserver.New(http.StatusBadRequest, "UNKNOWN_SLOT_PLAN", "This concurrency plan does not exist.")
}
