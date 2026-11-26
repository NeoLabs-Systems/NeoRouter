package providers

// The providers HTTP API, driven through a mux the way the server mounts it.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// routeHarness is the providers API behind a request-scoped space.
type routeHarness struct {
	service *Service
	mux     *http.ServeMux
	denied  map[string]bool
}

// newRouteHarness builds the router; adjust may change the service first.
func newRouteHarness(t *testing.T, adjust func(*Service)) *routeHarness {
	t.Helper()
	service, _ := testServiceWith(t, adjust)
	harness := &routeHarness{service: service, mux: http.NewServeMux(), denied: map[string]bool{}}
	service.Routes(harness.mux, RouteDeps{
		Space: func(r *http.Request) (Space, bool) {
			actor := r.Header.Get("X-Actor")
			if actor == "" {
				return Space{}, false
			}
			owner := r.Header.Get("X-Space")
			if owner == "" {
				owner = actor
			}
			role := r.Header.Get("X-Role")
			if role == "" {
				role = "admin"
			}
			return Space{OwnerID: owner, ActorID: actor, IsOwner: owner == actor, Role: role}, true
		},
		Require: func(permission string) func(http.Handler) http.Handler {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if harness.denied[permission] {
						writeRouteError(w, http.StatusForbidden, "PERMISSION_DENIED", "The team owner has not allowed this for you.")
						return
					}
					next.ServeHTTP(w, r)
				})
			}
		},
	})
	return harness
}

// writeRouteError answers with the management envelope.
func writeRouteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}

// do sends one request as a signed-in caller and returns the recorder.
func (h *routeHarness) do(t *testing.T, method, path, actor string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if actor != "" {
		request.Header.Set("X-Actor", actor)
	}
	recorder := httptest.NewRecorder()
	h.mux.ServeHTTP(recorder, request)
	return recorder
}

// decodeBody reads a JSON answer and fails the test on a non-JSON body.
func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %d %s: %v", recorder.Code, recorder.Body.String(), err)
	}
	return decoded
}

// errorOf reads the code of an error answer.
func errorOf(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	body := decodeBody(t, recorder)
	envelope, _ := body["error"].(map[string]any)
	code, _ := envelope["code"].(string)
	return code
}

func TestRoutesDefinitions(t *testing.T) {
	harness := newRouteHarness(t, nil)
	response := harness.do(t, http.MethodGet, "/api/v1/providers/definitions", "admin", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	body := decodeBody(t, response)
	definitions, _ := body["definitions"].([]any)
	seen := map[string]bool{}
	for _, entry := range definitions {
		definition, _ := entry.(map[string]any)
		seen[definition["id"].(string)] = true
	}
	if seen["neorouter_peer"] {
		t.Fatalf("a pool partner is not offered by hand")
	}
	if !seen["custom_openai"] || !seen["neorouter_local"] {
		t.Fatalf("definitions = %v", seen)
	}
	// The local models are offered only when an admin shares them and some are
	// installed.
	hidden := newRouteHarness(t, func(service *Service) {
		service.LocalModels = fakeLocalModels{allowed: false, offered: false}
	})
	response = hidden.do(t, http.MethodGet, "/api/v1/providers/definitions", "member", nil)
	if strings.Contains(response.Body.String(), "neorouter_local") {
		t.Fatalf("local models were offered: %s", response.Body.String())
	}
}

func TestRoutesAccountLifecycle(t *testing.T) {
	harness := newRouteHarness(t, nil)
	base := "http://127.0.0.1:9/v1"
	created := harness.do(t, http.MethodPost, "/api/v1/providers/accounts", "admin", map[string]any{
		"definitionId": "custom_openai",
		"baseUrl":      base,
		"secret":       "test-key-1",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	body := decodeBody(t, created)
	account, _ := body["account"].(map[string]any)
	accountID, _ := account["id"].(string)
	if accountID == "" || body["validation"] == nil || body["sync"] == nil {
		t.Fatalf("create body = %s", created.Body.String())
	}
	validation, _ := body["validation"].(map[string]any)
	if validation["status"] != "healthy" {
		t.Fatalf("validation = %v", validation)
	}

	listed := harness.do(t, http.MethodGet, "/api/v1/providers/accounts", "admin", nil)
	if accounts, _ := decodeBody(t, listed)["accounts"].([]any); len(accounts) != 1 {
		t.Fatalf("accounts = %s", listed.Body.String())
	}

	patched := harness.do(t, http.MethodPatch, "/api/v1/providers/accounts/"+accountID, "admin", map[string]any{"label": "Renamed"})
	if patched.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", patched.Code, patched.Body.String())
	}
	if label, _ := decodeBody(t, patched)["account"].(map[string]any)["label"].(string); label != "Renamed" {
		t.Fatalf("label = %s", label)
	}

	limits := harness.do(t, http.MethodPut, "/api/v1/providers/accounts/"+accountID+"/limits", "admin",
		map[string]any{"rpd": 60, "concurrent": 4})
	if limits.Code != http.StatusOK {
		t.Fatalf("limits = %d %s", limits.Code, limits.Body.String())
	}
	stored, _ := decodeBody(t, limits)["account"].(map[string]any)["limits"].(map[string]any)
	if stored["rpd"] != float64(60) || stored["concurrent"] != float64(4) {
		t.Fatalf("limits = %v", stored)
	}
	// A strict body: an unknown metric is a validation error, not a silent write.
	bad := harness.do(t, http.MethodPut, "/api/v1/providers/accounts/"+accountID+"/limits", "admin", map[string]any{"rpmm": 5})
	if bad.Code != http.StatusBadRequest || errorOf(t, bad) != "VALIDATION_ERROR" {
		t.Fatalf("bad limits = %d %s", bad.Code, bad.Body.String())
	}

	validated := harness.do(t, http.MethodPost, "/api/v1/providers/accounts/"+accountID+"/validate", "admin", nil)
	if validated.Code != http.StatusOK {
		t.Fatalf("validate = %d %s", validated.Code, validated.Body.String())
	}
	if decodeBody(t, validated)["validation"] == nil {
		t.Fatalf("validate body = %s", validated.Body.String())
	}

	synced := harness.do(t, http.MethodPost, "/api/v1/providers/accounts/"+accountID+"/sync", "admin", nil)
	if synced.Code != http.StatusOK {
		t.Fatalf("sync = %d %s", synced.Code, synced.Body.String())
	}
	if sync := decodeBody(t, synced)["sync"]; sync == nil {
		t.Fatalf("sync body = %s", synced.Body.String())
	}

	quota := harness.do(t, http.MethodPost, "/api/v1/providers/accounts/"+accountID+"/quota", "admin", nil)
	if quota.Code != http.StatusOK {
		t.Fatalf("quota = %d %s", quota.Code, quota.Body.String())
	}
	if _, ok := decodeBody(t, quota)["quota"].([]any); !ok {
		t.Fatalf("quota body = %s", quota.Body.String())
	}

	fixed := harness.do(t, http.MethodPost, "/api/v1/providers/accounts/"+accountID+"/mark-fixed", "admin", nil)
	if fixed.Code != http.StatusOK {
		t.Fatalf("mark-fixed = %d %s", fixed.Code, fixed.Body.String())
	}
	if decodeBody(t, fixed)["fixed"] == nil {
		t.Fatalf("mark-fixed body = %s", fixed.Body.String())
	}

	removed := harness.do(t, http.MethodDelete, "/api/v1/providers/accounts/"+accountID, "admin", nil)
	if removed.Code != http.StatusNoContent || removed.Body.Len() != 0 {
		t.Fatalf("delete = %d %q", removed.Code, removed.Body.String())
	}
	missing := harness.do(t, http.MethodGet, "/api/v1/providers/accounts", "admin", nil)
	if accounts, _ := decodeBody(t, missing)["accounts"].([]any); len(accounts) != 0 {
		t.Fatalf("accounts = %s", missing.Body.String())
	}
	if gone := harness.do(t, http.MethodPost, "/api/v1/providers/accounts/"+accountID+"/validate", "admin", nil); gone.Code != http.StatusNotFound || errorOf(t, gone) != "ACCOUNT_NOT_FOUND" {
		t.Fatalf("a deleted connection = %d %s", gone.Code, gone.Body.String())
	}
}

func TestRoutesProbeAndCreateChecks(t *testing.T) {
	harness := newRouteHarness(t, nil)
	probe := harness.do(t, http.MethodPost, "/api/v1/providers/probe", "admin", map[string]any{
		"definitionId": "custom_openai",
		"baseUrl":      "http://127.0.0.1:9/v1",
		"secret":       "test-key-1",
	})
	if probe.Code != http.StatusOK {
		t.Fatalf("probe = %d %s", probe.Code, probe.Body.String())
	}
	body := decodeBody(t, probe)
	if body["validation"] == nil || body["models"] == nil {
		t.Fatalf("probe body = %s", probe.Body.String())
	}
	// The schema is enforced before anything else happens.
	invalid := harness.do(t, http.MethodPost, "/api/v1/providers/accounts", "admin", map[string]any{"label": "no definition"})
	if invalid.Code != http.StatusBadRequest || errorOf(t, invalid) != "VALIDATION_ERROR" {
		t.Fatalf("invalid create = %d %s", invalid.Code, invalid.Body.String())
	}
	freeTier := harness.do(t, http.MethodPost, "/api/v1/providers/accounts", "admin", map[string]any{
		"definitionId":   "custom_openai",
		"baseUrl":        "http://127.0.0.1:9/v1",
		"secret":         "test-key-1",
		"billingMode":    "free_tier",
		"classification": "free",
	})
	if freeTier.Code != http.StatusCreated {
		t.Fatalf("create with a classification = %d %s", freeTier.Code, freeTier.Body.String())
	}
	if mode, _ := decodeBody(t, freeTier)["account"].(map[string]any)["classification"].(string); mode != "free" {
		t.Fatalf("classification = %s", mode)
	}
	// Local models are an admin's decision, and the API says so.
	harness.service.LocalModels = fakeLocalModels{allowed: false, offered: false}
	local := harness.do(t, http.MethodPost, "/api/v1/providers/accounts", "member", map[string]any{
		"definitionId": "neorouter_local",
	})
	if local.Code != http.StatusForbidden || errorOf(t, local) != "LOCAL_MODELS_NOT_SHARED" {
		t.Fatalf("local = %d %s", local.Code, local.Body.String())
	}
}

func TestRoutesImportIsRateLimited(t *testing.T) {
	harness := newRouteHarness(t, nil)
	file := map[string]any{
		"kind": KeyImportKind,
		"keys": []any{map[string]any{"definitionId": "groq", "secret": "test-key-1"}},
	}
	statuses := []int{}
	for i := 0; i < 6; i++ {
		response := harness.do(t, http.MethodPost, "/api/v1/providers/accounts/import", "admin", file)
		statuses = append(statuses, response.Code)
	}
	if statuses[0] != http.StatusOK {
		t.Fatalf("first import = %d", statuses[0])
	}
	if statuses[5] != http.StatusTooManyRequests {
		t.Fatalf("statuses = %v", statuses)
	}
}

func TestRoutesPermissions(t *testing.T) {
	harness := newRouteHarness(t, nil)
	harness.denied["providers.manage"] = true
	response := harness.do(t, http.MethodPost, "/api/v1/providers/probe", "admin", map[string]any{"definitionId": "custom_openai"})
	if response.Code != http.StatusForbidden || errorOf(t, response) != "PERMISSION_DENIED" {
		t.Fatalf("denied = %d %s", response.Code, response.Body.String())
	}
	// Reading the accounts needs its own permission, and a request with no space
	// at all is not signed in.
	harness.denied = map[string]bool{"providers.view": true}
	if response := harness.do(t, http.MethodGet, "/api/v1/providers/accounts", "admin", nil); response.Code != http.StatusForbidden {
		t.Fatalf("view = %d", response.Code)
	}
	harness.denied = map[string]bool{}
	anonymous := harness.do(t, http.MethodGet, "/api/v1/providers/accounts", "", nil)
	if anonymous.Code != http.StatusUnauthorized || errorOf(t, anonymous) != "AUTHENTICATION_REQUIRED" {
		t.Fatalf("anonymous = %d %s", anonymous.Code, anonymous.Body.String())
	}
}

func TestRoutesSignIns(t *testing.T) {
	codex := newCodexAuthServer(t)
	portal := newNousPortalServer(t)
	harness := newRouteHarness(t, func(service *Service) {
		service.Getenv = func(name string) string {
			switch name {
			case "NEOROUTER_CODEX_ISSUER":
				return codex.url
			case "NEOROUTER_NOUS_PORTAL_URL":
				return portal.url
			}
			return ""
		}
	})

	start := harness.do(t, http.MethodPost, "/api/v1/providers/chatgpt/login", "admin", map[string]any{"billingMode": "plan_paid"})
	if start.Code != http.StatusOK {
		t.Fatalf("start = %d %s", start.Code, start.Body.String())
	}
	login := decodeBody(t, start)
	if login["userCode"] != "ABCD-1234" || !strings.HasSuffix(login["verificationUrl"].(string), "/codex/device") {
		t.Fatalf("login = %s", start.Body.String())
	}
	loginID, _ := login["loginId"].(string)
	pending := harness.do(t, http.MethodPost, "/api/v1/providers/chatgpt/login/"+loginID, "admin", nil)
	if pending.Code != http.StatusOK || decodeBody(t, pending)["status"] != "pending" {
		t.Fatalf("pending = %d %s", pending.Code, pending.Body.String())
	}
	// A sign-in belongs to the space that started it.
	stranger := harness.do(t, http.MethodPost, "/api/v1/providers/chatgpt/login/"+loginID, "member", nil)
	if stranger.Code != http.StatusNotFound || errorOf(t, stranger) != "LOGIN_NOT_FOUND" {
		t.Fatalf("stranger = %d %s", stranger.Code, stranger.Body.String())
	}
	done := harness.do(t, http.MethodPost, "/api/v1/providers/chatgpt/login/"+loginID, "admin", nil)
	if done.Code != http.StatusOK {
		t.Fatalf("done = %d %s", done.Code, done.Body.String())
	}
	body := decodeBody(t, done)
	if body["status"] != "done" || body["plan"] != "plus" {
		t.Fatalf("done = %s", done.Body.String())
	}
	account, _ := body["account"].(map[string]any)
	if account["definitionId"] != "chatgpt" || account["billingMode"] != "plan_paid" {
		t.Fatalf("account = %v", account)
	}
	// The sign-in is used up.
	again := harness.do(t, http.MethodPost, "/api/v1/providers/chatgpt/login/"+loginID, "admin", nil)
	if again.Code != http.StatusNotFound {
		t.Fatalf("again = %d %s", again.Code, again.Body.String())
	}

	// Nous Portal: a declined sign-in is reported as it is.
	portal.decline = true
	nousStart := harness.do(t, http.MethodPost, "/api/v1/providers/nous/login", "admin", nil)
	if nousStart.Code != http.StatusOK {
		t.Fatalf("nous start = %d %s", nousStart.Code, nousStart.Body.String())
	}
	nousID, _ := decodeBody(t, nousStart)["loginId"].(string)
	declined := harness.do(t, http.MethodPost, "/api/v1/providers/nous/login/"+nousID, "admin", nil)
	if declined.Code != http.StatusBadRequest || errorOf(t, declined) != "NOUS_SIGN_IN_FAILED" {
		t.Fatalf("declined = %d %s", declined.Code, declined.Body.String())
	}
	if message, _ := decodeBody(t, declined)["error"].(map[string]any)["message"].(string); !strings.Contains(message, "declined") {
		t.Fatalf("message = %q", message)
	}
	if gone := harness.do(t, http.MethodPost, "/api/v1/providers/nous/login/"+nousID, "admin", nil); gone.Code != http.StatusNotFound {
		t.Fatalf("a declined sign-in is gone: %d", gone.Code)
	}
	// A working sign-in creates a paid connection.
	portal.decline = false
	portal.polls = 1
	nousStart = harness.do(t, http.MethodPost, "/api/v1/providers/nous/login", "admin", nil)
	nousID, _ = decodeBody(t, nousStart)["loginId"].(string)
	nousDone := harness.do(t, http.MethodPost, "/api/v1/providers/nous/login/"+nousID, "admin", nil)
	if nousDone.Code != http.StatusOK || decodeBody(t, nousDone)["status"] != "done" {
		t.Fatalf("nous done = %d %s", nousDone.Code, nousDone.Body.String())
	}
	nousBody := decodeBody(t, nousDone)
	if nousBody["email"] != "portal@example.test" || nousBody["tier"] != "paid" {
		t.Fatalf("nous done = %s", nousDone.Body.String())
	}
	if account, _ := nousBody["account"].(map[string]any); account["definitionId"] != "nous" || account["billingMode"] != "paid" {
		t.Fatalf("nous account = %v", nousBody["account"])
	}
}

func TestRoutesSlowedDownSignIn(t *testing.T) {
	slow := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/device/code":
			_ = r.ParseForm()
			writeTestJSON(w, map[string]any{"device_code": "dc-9", "user_code": "SLOW-1", "expires_in": float64(600), "interval": float64(1)})
		case "/api/oauth/token":
			w.WriteHeader(http.StatusBadRequest)
			writeTestJSON(w, map[string]any{"error": "slow_down"})
		}
	})
	harness := newRouteHarness(t, func(service *Service) {
		service.Getenv = func(name string) string {
			if name == "NEOROUTER_NOUS_PORTAL_URL" {
				return slow
			}
			return ""
		}
	})
	start := harness.do(t, http.MethodPost, "/api/v1/providers/nous/login", "admin", nil)
	loginID, _ := decodeBody(t, start)["loginId"].(string)
	first := decodeBody(t, harness.do(t, http.MethodPost, "/api/v1/providers/nous/login/"+loginID, "admin", nil))
	if first["status"] != "pending" || first["interval"] != float64(10) {
		t.Fatalf("first poll = %v", first)
	}
	second := decodeBody(t, harness.do(t, http.MethodPost, "/api/v1/providers/nous/login/"+loginID, "admin", nil))
	if second["interval"] != float64(15) {
		t.Fatalf("second poll = %v", second)
	}
}
