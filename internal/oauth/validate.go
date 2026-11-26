package oauth

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// access is what the approval screen decided the new key may use. The access
// object: the three switches are required, everything else is an optional
// refinement. A nil optional field was not sent.
type access struct {
	AllowFree          bool
	AllowPlan          bool
	AllowPaid          bool
	BudgetUSD          *float64
	BudgetPeriod       *string // total, monthly or daily
	RPMLimit           *int64
	TokenLimit         *int64
	TokenPeriod        *string // total, monthly or daily
	MaxPricePerMillion *float64
	ExpiresAt          *string
}

// errChooseAccess is the 400 the approval route answers with a bad body.
func errChooseAccess() *httpserver.HttpError {
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", "Choose what the app may use.")
}

// parseAccess validates an approval body.
func parseAccess(raw []byte) (access, error) {
	var fields map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return access{}, errChooseAccess()
		}
	}
	var approval access
	switches := []struct {
		name  string
		value *bool
	}{
		{"allowFree", &approval.AllowFree},
		{"allowPlan", &approval.AllowPlan},
		{"allowPaid", &approval.AllowPaid},
	}
	for _, item := range switches {
		value, ok := boolField(fields[item.name])
		if !ok {
			return access{}, errChooseAccess()
		}
		*item.value = value
	}
	var err error
	if approval.BudgetUSD, err = numberField(fields["budgetUsd"]); err != nil {
		return access{}, err
	}
	if approval.MaxPricePerMillion, err = numberField(fields["maxPricePerMillion"]); err != nil {
		return access{}, err
	}
	if approval.BudgetPeriod, err = periodField(fields["budgetPeriod"]); err != nil {
		return access{}, err
	}
	if approval.TokenPeriod, err = periodField(fields["tokenPeriod"]); err != nil {
		return access{}, err
	}
	if approval.RPMLimit, err = positiveIntField(fields["rpmLimit"]); err != nil {
		return access{}, err
	}
	if approval.TokenLimit, err = positiveIntField(fields["tokenLimit"]); err != nil {
		return access{}, err
	}
	if approval.ExpiresAt, err = datetimeField(fields["expiresAt"]); err != nil {
		return access{}, err
	}
	return approval, nil
}

// isNull reports whether a JSON field is the literal null.
func isNull(raw json.RawMessage) bool { return string(raw) == "null" }

// boolField is a required boolean.
func boolField(raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 || isNull(raw) {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

// numberField is z.number().min(0).nullable().optional().
func numberField(raw json.RawMessage) (*float64, error) {
	if len(raw) == 0 || isNull(raw) {
		return nil, nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil || value < 0 {
		return nil, errChooseAccess()
	}
	return &value, nil
}

// positiveIntField is z.number().int().positive().nullable().optional().
func positiveIntField(raw json.RawMessage) (*int64, error) {
	if len(raw) == 0 || isNull(raw) {
		return nil, nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil || value <= 0 || value != math.Trunc(value) || value > math.MaxInt64 {
		return nil, errChooseAccess()
	}
	whole := int64(value)
	return &whole, nil
}

// periodField is z.enum(['total','monthly','daily']).optional().
func periodField(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errChooseAccess()
	}
	switch value {
	case "total", "monthly", "daily":
		return &value, nil
	}
	return nil, errChooseAccess()
}

// datetimePattern is the grammar of an ISO 8601 datetime with offset.
var datetimePattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T([01]\d|2[0-3]):[0-5]\d(:[0-5]\d(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})$`)

// datetimeField is z.string().datetime({offset:true}).nullable().optional().
func datetimeField(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || isNull(raw) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || !datetimePattern.MatchString(value) {
		return nil, errChooseAccess()
	}
	return &value, nil
}

// maxReturnTo is the longest returnTo the OpenRouter start route accepts.
const maxReturnTo = 2048

// parseReturnTo validates {returnTo: URL of at most 2048 characters}; its
// messages are forwarded verbatim.
func parseReturnTo(raw []byte) (string, error) {
	var body struct {
		ReturnTo *string `json:"returnTo"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	if body.ReturnTo == nil {
		return "", validationError("returnTo", "Required")
	}
	value := *body.ReturnTo
	if len([]rune(value)) > maxReturnTo {
		return "", validationError("returnTo", fmt.Sprintf("String must contain at most %d character(s)", maxReturnTo))
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return "", validationError("returnTo", "Invalid url")
	}
	return value, nil
}

// flattenDetails groups the issues into form errors and field errors, which are
// sent as details.
type flattenDetails struct {
	FormErrors  []string            `json:"formErrors"`
	FieldErrors map[string][]string `json:"fieldErrors"`
}

// validationError is the 400 VALIDATION_ERROR: `field: message` and the
// flattened issues as details.
func validationError(field, message string) *httpserver.HttpError {
	return httpserver.New(http.StatusBadRequest, "VALIDATION_ERROR", field+": "+message, flattenDetails{
		FormErrors:  []string{},
		FieldErrors: map[string][]string{field: {message}},
	})
}
