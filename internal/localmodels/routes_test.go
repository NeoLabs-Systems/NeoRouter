package localmodels

// The request validation of server/routes/local_models.js: zod's messages and
// flatten() trees, byte for byte, and the admin gate.

import (
	"net/http"
	"strings"
	"testing"
)

func TestValidationMatchesZod(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name    string
		method  string
		path    string
		body    string
		status  int
		code    string
		message string
		details string
	}{
		{
			name: "access level missing", method: http.MethodPut, path: "/access", body: `{}`,
			status: http.StatusBadRequest, code: "VALIDATION_ERROR", message: "level: Required",
			details: `{"formErrors":[],"fieldErrors":{"level":["Required"]}}`,
		},
		{
			name: "access level unknown", method: http.MethodPut, path: "/access", body: `{"level":"nobody"}`,
			status: http.StatusBadRequest, code: "VALIDATION_ERROR",
			message: "level: Invalid enum value. Expected 'admins' | 'everyone', received 'nobody'",
			details: `{"formErrors":[],"fieldErrors":{"level":["Invalid enum value. Expected 'admins' | 'everyone', received 'nobody'"]}}`,
		},
		{
			name: "access level wrong type", method: http.MethodPut, path: "/access", body: `{"level":7}`,
			status: http.StatusBadRequest, code: "VALIDATION_ERROR",
			message: "level: Expected 'admins' | 'everyone', received number",
		},
		{
			name: "privacy flag missing", method: http.MethodPut, path: "/privacy-filter", body: `{}`,
			status: http.StatusBadRequest, code: "VALIDATION_ERROR", message: "enabled: Required",
			details: `{"formErrors":[],"fieldErrors":{"enabled":["Required"]}}`,
		},
		{
			name: "privacy flag wrong type", method: http.MethodPut, path: "/privacy-filter", body: `{"enabled":"yes"}`,
			status: http.StatusBadRequest, code: "VALIDATION_ERROR",
			message: "enabled: Expected boolean, received string",
		},
		{
			name: "not an object", method: http.MethodPut, path: "/access", body: `[1,2]`,
			status: http.StatusBadRequest, code: "VALIDATION_ERROR",
			message: "Expected object, received array",
			details: `{"formErrors":["Expected object, received array"],"fieldErrors":{}}`,
		},
		{
			// body-parser's strict mode refuses a top-level null before zod runs.
			name: "invalid json", method: http.MethodPut, path: "/access", body: `null`,
			status: http.StatusBadRequest, code: "INVALID_JSON",
			message: "The request body is not valid JSON.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := h.apiRaw(tc.method, tc.path, adminToken, tc.body)
			if status != tc.status {
				t.Fatalf("status = %d body %s", status, raw)
			}
			body := string(raw)
			if !strings.Contains(body, `"code":"`+tc.code+`"`) {
				t.Fatalf("code missing: %s", body)
			}
			if !strings.Contains(body, `"message":"`+tc.message+`"`) {
				t.Fatalf("message = %s, want %q", body, tc.message)
			}
			if tc.details != "" && !strings.Contains(body, `"details":`+tc.details) {
				t.Fatalf("details = %s, want %s", body, tc.details)
			}
		})
	}
}

func TestTheRoutesSitBehindTheAdminGate(t *testing.T) {
	h := newHarness(t)
	status, raw := h.apiRaw(http.MethodGet, "", "no-token", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d body %s", status, raw)
	}
	if !strings.Contains(string(raw), `"code":"AUTHENTICATION_REQUIRED"`) {
		t.Fatalf("body = %s", raw)
	}
}
