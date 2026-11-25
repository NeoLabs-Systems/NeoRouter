package settings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// adminCall serves one request through the routes, with a fixed request id so
// error envelopes are byte-comparable.
func adminCall(t *testing.T, svc *Service, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	svc.Routes(mux)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-Request-Id", "test-request-id")
	recorder := httptest.NewRecorder()
	httpserver.RequestID(mux).ServeHTTP(recorder, request)
	return recorder
}

// adminService is a service whose other-domain hooks are recorded stubs.
type adminService struct {
	*Service
	metadata       any
	privacy        any
	analysisKeys   []*string
	privacyToggles []bool
}

func newAdminService(t *testing.T) *adminService {
	t.Helper()
	svc := &adminService{
		Service:  testService(t),
		metadata: json.RawMessage(`{"fetchedAt":null,"artificialAnalysisConfigured":false,"sourcesEnabled":false}`),
		privacy:  json.RawMessage(`{"enabled":false,"model":"privacy-filter","memoryMb":96}`),
	}
	svc.Service.MetadataStatus = func() any { return svc.metadata }
	svc.Service.PrivacyModelStatus = func() any { return svc.privacy }
	svc.Service.SetArtificialAnalysisKey = func(_ context.Context, key *string) error {
		svc.analysisKeys = append(svc.analysisKeys, key)
		return nil
	}
	svc.Service.SetPrivacyModelEnabled = func(_ context.Context, enabled bool) error {
		svc.privacyToggles = append(svc.privacyToggles, enabled)
		return nil
	}
	return svc
}

func TestGetAdminSettings(t *testing.T) {
	svc := newAdminService(t)
	ctx := context.Background()
	if err := svc.SetGlobal(ctx, KeyRegistrationOpen, true); err != nil {
		t.Fatalf("set registration_open: %v", err)
	}

	recorder := adminCall(t, svc.Service, http.MethodGet, "/api/v1/admin/settings", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	want := `{"registrationOpen":true,` +
		`"metadata":{"fetchedAt":null,"artificialAnalysisConfigured":false,"sourcesEnabled":false},` +
		`"privacyModel":{"enabled":false,"model":"privacy-filter","memoryMb":96}}`
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
	if recorder.Header().Get("Cache-Control") != "" {
		t.Fatalf("only the account export sets Cache-Control")
	}
}

func TestPatchAdminSettings(t *testing.T) {
	svc := newAdminService(t)
	recorder := adminCall(t, svc.Service, http.MethodPatch, "/api/v1/admin/settings",
		`{"registrationOpen":true,"artificialAnalysisKey":null,"privacyModel":true}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	if open, err := svc.RegistrationOpen(context.Background()); err != nil || !open {
		t.Fatalf("registration_open = %v (err %v), want true", open, err)
	}
	if len(svc.analysisKeys) != 1 || svc.analysisKeys[0] != nil {
		t.Fatalf("the key must be cleared with null, got %v", svc.analysisKeys)
	}
	if len(svc.privacyToggles) != 1 || !svc.privacyToggles[0] {
		t.Fatalf("privacy toggles = %v, want [true]", svc.privacyToggles)
	}
	if !strings.Contains(recorder.Body.String(), `"registrationOpen":true`) {
		t.Fatalf("the response is the updated settings: %s", recorder.Body)
	}

	// A present key is passed through, and an absent field changes nothing.
	recorder = adminCall(t, svc.Service, http.MethodPatch, "/api/v1/admin/settings",
		`{"artificialAnalysisKey":"aa-key"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	if len(svc.analysisKeys) != 2 || svc.analysisKeys[1] == nil || *svc.analysisKeys[1] != "aa-key" {
		t.Fatalf("the key was not stored: %v", svc.analysisKeys)
	}
	if open, _ := svc.RegistrationOpen(context.Background()); !open {
		t.Fatalf("an absent registrationOpen must not change it")
	}
	if len(svc.privacyToggles) != 1 {
		t.Fatalf("an absent privacyModel must not be applied")
	}

	// No body at all is an empty patch, as express leaves it.
	recorder = adminCall(t, svc.Service, http.MethodPatch, "/api/v1/admin/settings", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	// Unknown members are stripped by zod, not refused.
	recorder = adminCall(t, svc.Service, http.MethodPatch, "/api/v1/admin/settings", `{"nope":{"a":1}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
}

func TestPatchAdminSettingsValidation(t *testing.T) {
	const uuid = "test-request-id"
	cases := []struct {
		name string
		body string
		// message and details of the 400 envelope.
		message string
		details string
	}{
		{
			name:    "boolean given a string",
			body:    `{"registrationOpen":"x"}`,
			message: "registrationOpen: Expected boolean, received string",
			details: `{"formErrors":[],"fieldErrors":{"registrationOpen":["Expected boolean, received string"]}}`,
		},
		{
			name:    "boolean given a number",
			body:    `{"registrationOpen":1}`,
			message: "registrationOpen: Expected boolean, received number",
			details: `{"formErrors":[],"fieldErrors":{"registrationOpen":["Expected boolean, received number"]}}`,
		},
		{
			name:    "boolean given null",
			body:    `{"registrationOpen":null}`,
			message: "registrationOpen: Expected boolean, received null",
			details: `{"formErrors":[],"fieldErrors":{"registrationOpen":["Expected boolean, received null"]}}`,
		},
		{
			name:    "string given a number",
			body:    `{"artificialAnalysisKey":123}`,
			message: "artificialAnalysisKey: Expected string, received number",
			details: `{"formErrors":[],"fieldErrors":{"artificialAnalysisKey":["Expected string, received number"]}}`,
		},
		{
			name:    "string given an array",
			body:    `{"artificialAnalysisKey":[1]}`,
			message: "artificialAnalysisKey: Expected string, received array",
			details: `{"formErrors":[],"fieldErrors":{"artificialAnalysisKey":["Expected string, received array"]}}`,
		},
		{
			name:    "string too long",
			body:    `{"artificialAnalysisKey":"` + strings.Repeat("a", 513) + `"}`,
			message: "artificialAnalysisKey: String must contain at most 512 character(s)",
			details: `{"formErrors":[],"fieldErrors":{"artificialAnalysisKey":["String must contain at most 512 character(s)"]}}`,
		},
		{
			name:    "privacy flag given a string",
			body:    `{"privacyModel":"no"}`,
			message: "privacyModel: Expected boolean, received string",
			details: `{"formErrors":[],"fieldErrors":{"privacyModel":["Expected boolean, received string"]}}`,
		},
		{
			name:    "body is an array",
			body:    `[]`,
			message: "Expected object, received array",
			details: `{"formErrors":["Expected object, received array"],"fieldErrors":{}}`,
		},
		{
			name:    "first issue wins, in schema order",
			body:    `{"privacyModel":"no","artificialAnalysisKey":1,"registrationOpen":"x"}`,
			message: "registrationOpen: Expected boolean, received string",
			details: `{"formErrors":[],"fieldErrors":{"registrationOpen":["Expected boolean, received string"],` +
				`"artificialAnalysisKey":["Expected string, received number"],"privacyModel":["Expected boolean, received string"]}}`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			svc := newAdminService(t)
			recorder := adminCall(t, svc.Service, http.MethodPatch, "/api/v1/admin/settings", test.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", recorder.Code, recorder.Body)
			}
			want := `{"error":{"code":"VALIDATION_ERROR","message":` + quote(test.message) +
				`,"details":` + test.details + `,"requestId":"` + uuid + `"}}`
			if got := recorder.Body.String(); got != want {
				t.Fatalf("body =\n%s\nwant\n%s", got, want)
			}
			if len(svc.analysisKeys) != 0 || len(svc.privacyToggles) != 0 {
				t.Fatalf("a rejected patch must not be applied")
			}
		})
	}
}

func TestPatchAdminSettingsAcceptsBoundaries(t *testing.T) {
	svc := newAdminService(t)
	recorder := adminCall(t, svc.Service, http.MethodPatch, "/api/v1/admin/settings",
		`{"artificialAnalysisKey":"`+strings.Repeat("a", 512)+`"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("512 characters are allowed: %d (%s)", recorder.Code, recorder.Body)
	}
	recorder = adminCall(t, svc.Service, http.MethodPatch, "/api/v1/admin/settings", `{"privacyModel":false}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", recorder.Code, recorder.Body)
	}
	if len(svc.privacyToggles) != 1 || svc.privacyToggles[0] {
		t.Fatalf("privacy toggles = %v, want [false]", svc.privacyToggles)
	}
}

func TestAdminSettingsOtherMethodsAreNotFound(t *testing.T) {
	svc := newAdminService(t)
	recorder := adminCall(t, svc.Service, http.MethodPost, "/api/v1/admin/settings", `{}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	want := `{"error":{"code":"NOT_FOUND","message":"The requested resource was not found."}}`
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestAdminSettingsInvalidJSON(t *testing.T) {
	svc := newAdminService(t)
	recorder := adminCall(t, svc.Service, http.MethodPatch, "/api/v1/admin/settings", `{"registrationOpen":`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", recorder.Code, recorder.Body)
	}
	want := `{"error":{"code":"INVALID_JSON","message":"The request body is not valid JSON.","requestId":"test-request-id"}}`
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

// quote renders a string as a JSON string without HTML escaping.
func quote(value string) string {
	encoded, _ := marshalNoEscape(value)
	return string(encoded)
}
