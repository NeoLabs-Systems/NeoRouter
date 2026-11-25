package apps

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// testEnv builds a Service on a real temporary database and directory, the way
// internal/app builds it.
type testEnv struct {
	t         *testing.T
	svc       *Service
	db        *dbx.DB
	cfg       config.Config
	mux       *http.ServeMux
	env       map[string]string
	now       time.Time
	restarted chan struct{}
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	home := t.TempDir()
	env := &testEnv{t: t, env: map[string]string{}, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), restarted: make(chan struct{}, 1)}
	getenv := func(key string) string { return env.env[key] }
	cfg := config.Config{
		Paths: config.ResolvePaths(emptyEnv, home),
		Host:  "127.0.0.1",
		Port:  4600,
	}
	if err := cfg.Paths.Ensure(); err != nil {
		t.Fatalf("prepare paths: %v", err)
	}
	db, err := dbx.Open(context.Background(), cfg.Paths.Database, dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range schemaStatements {
		if _, err := db.W.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	env.db = db
	env.cfg = cfg
	env.svc = New(Options{
		DB:      db,
		Config:  func() config.Config { return env.cfg },
		Getenv:  getenv,
		Log:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Guard:   Guard{Authenticate: env.authenticate, Space: env.space},
		Restart: func() { env.restarted <- struct{}{} },
	})
	env.svc.now = func() time.Time { return env.now }
	env.mux = http.NewServeMux()
	env.svc.Routes(env.mux)
	return env
}

// schemaStatements are the two tables key_activity.js reads (baseline.sql).
var schemaStatements = []string{
	`CREATE TABLE users (id TEXT PRIMARY KEY)`,
	`CREATE TABLE router_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		prefix TEXT NOT NULL UNIQUE,
		key_hash TEXT NOT NULL UNIQUE,
		mode TEXT NOT NULL,
		last_used_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`,
	`CREATE TABLE requests (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		router_key_id TEXT REFERENCES router_keys(id) ON DELETE SET NULL,
		created_at INTEGER NOT NULL,
		status TEXT NOT NULL,
		model_label TEXT,
		requested_model TEXT NOT NULL,
		error_code TEXT,
		error_message TEXT,
		latency_ms INTEGER)`,
}

// emptyEnv reads no configuration: the runtime paths come from the home alone.
func emptyEnv(string) string { return "" }

// sessions maps a bearer token to the caller it authenticates.
var sessions = map[string]Session{
	"admin-token": {UserID: "user-admin", Role: "admin"},
	"user-token":  {UserID: "user-plain", Role: "user"},
}

// spaces maps a session to the space it works in.
var spaces = map[string]Space{
	"admin-token": {OwnerID: "user-admin", ActorID: "user-admin", IsOwner: true,
		Permissions: map[string]bool{"keys.view": true, "keys.create": true, "providers.manage": true}},
	"user-token": {OwnerID: "user-plain", ActorID: "user-plain", IsOwner: true,
		Permissions: map[string]bool{"keys.view": true}},
}

func (e *testEnv) authenticate(r *http.Request) (*Session, error) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	session, ok := sessions[token]
	if !ok {
		return nil, ErrAuthenticationRequired()
	}
	return &session, nil
}

func (e *testEnv) space(r *http.Request, session *Session) (*Space, error) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	space, ok := spaces[token]
	if !ok {
		return nil, ErrAuthenticationRequired()
	}
	if requested := r.Header.Get("X-NeoRouter-Space"); requested != "" && requested != session.UserID {
		return nil, httpserver.New(http.StatusNotFound, "SPACE_NOT_FOUND", "This space does not exist.")
	}
	return &space, nil
}

// do is one request with a bearer token.
func (e *testEnv) do(token, method, path, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	e.mux.ServeHTTP(recorder, request)
	return recorder
}

// insertKey adds a router key and the user that owns it.
func (e *testEnv) insertKey(id, ownerID, name, lastUsedAt string) {
	e.t.Helper()
	if _, err := e.db.W.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO users (id) VALUES (?)`, ownerID); err != nil {
		e.t.Fatalf("insert user: %v", err)
	}
	if _, err := e.db.W.ExecContext(context.Background(),
		`INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode, last_used_at) VALUES (?, ?, ?, ?, ?, 'both', ?)`,
		id, ownerID, name, id+"-prefix", id+"-hash", nullable(lastUsedAt)); err != nil {
		e.t.Fatalf("insert key: %v", err)
	}
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
}

func TestCatalogHasEveryApp(t *testing.T) {
	env := newTestEnv(t)
	response := env.do("user-token", http.MethodGet, "/api/v1/apps", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body)
	}
	var body struct {
		Apps    []App         `json:"apps"`
		Places  []Place       `json:"places"`
		Network NetworkStatus `json:"network"`
	}
	decodeBody(t, response, &body)

	ids := make([]string, 0, len(body.Apps))
	for _, entry := range body.Apps {
		ids = append(ids, entry.ID)
	}
	// The apps the Flutter dashboard shows first, plus the catch-alls.
	for _, want := range []string{"pi-dev", "cline", "open-webui", "claude-code", "other-openai"} {
		if !contains(ids, want) {
			t.Errorf("catalog is missing %q: %v", want, ids)
		}
	}
	if len(body.Apps) != len(catalog) {
		t.Errorf("apps = %d, want %d", len(body.Apps), len(catalog))
	}
	// An entry keeps its template and its per-field labels verbatim.
	piDev, ok := Get("pi-dev")
	if !ok {
		t.Fatal("no pi-dev entry")
	}
	if !strings.Contains(piDev.File.Template, `"baseUrl": "{{baseUrl}}"`) {
		t.Errorf("pi-dev template = %q", piDev.File.Template)
	}
	if piDev.Summary.DE != "Coding-Agent-Umgebung." {
		t.Errorf("pi-dev summary = %q", piDev.Summary.DE)
	}

	places := map[string]Place{}
	for _, place := range body.Places {
		places[place.ID] = place
	}
	if got := deref(places["local"].Address); got != "http://127.0.0.1:4600/v1" {
		t.Errorf("local place = %q", got)
	}
	if !places["local"].Reachable {
		t.Error("local place is not reachable")
	}
	if got := deref(places["docker"].Address); got != "http://host.docker.internal:4600/v1" {
		t.Errorf("docker place = %q", got)
	}
	if places["internet"].Reachable {
		t.Error("internet place is reachable without a public URL")
	}
	if got := deref(places["internet"].Fix); got != "public_url" {
		t.Errorf("internet fix = %q", got)
	}
	if scope := body.Network.Scope; scope != "local" && scope != "network" {
		t.Errorf("network scope = %q", scope)
	}

	// The raw JSON keeps the catalog's own key order and spellings.
	if !strings.Contains(response.Body.String(), `"cloudOnly":false`) {
		t.Errorf("catalog body has no explicit cloudOnly: %s", response.Body)
	}
	// Express matches the trailing slash too.
	if trailing := env.do("user-token", http.MethodGet, "/api/v1/apps/", ""); trailing.Code != http.StatusOK {
		t.Errorf("trailing slash status = %d", trailing.Code)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// TestPlacesByOrigin is the JS test "on its own domain, apps get that one
// address and no local choices".
func TestPlacesByOrigin(t *testing.T) {
	env := newTestEnv(t)
	hosted := env.svc.Places("https://router.example.com")
	if len(hosted) != 1 {
		t.Fatalf("hosted places = %d, want 1", len(hosted))
	}
	if hosted[0].ID != "internet" || deref(hosted[0].Address) != "https://router.example.com/v1" || !hosted[0].Reachable {
		t.Errorf("hosted place = %+v", hosted[0])
	}
	// An address only reachable at home is not its own domain.
	for _, origin := range []string{"http://127.0.0.1:4600", "https://neorouter.local", "https://192.168.1.20", "http://router.example.com"} {
		places := env.svc.Places(origin)
		ids := make([]string, 0, len(places))
		for _, place := range places {
			ids = append(ids, place.ID)
		}
		if strings.Join(ids, ",") != "local,docker,network,internet" {
			t.Errorf("places(%s) = %v", origin, ids)
		}
	}
	// NEOROUTER_PUBLIC_URL wins over the origin.
	env.cfg.PublicURL = "https://router.example.com"
	places := env.svc.Places("http://127.0.0.1:4600")
	if len(places) != 1 || deref(places[0].Address) != "https://router.example.com/v1" {
		t.Errorf("public URL places = %+v", places)
	}
}

// TestKeyActivity ports "the live check sees a request arrive, and why it failed".
func TestKeyActivity(t *testing.T) {
	env := newTestEnv(t)
	env.insertKey("key-1", "user-plain", "Pi Dev", "")

	before := env.do("user-token", http.MethodGet, "/api/v1/apps/keys/key-1/activity", "")
	if before.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", before.Code, before.Body)
	}
	var activity Activity
	decodeBody(t, before, &activity)
	if activity.LastUsedAt != nil || activity.Request != nil {
		t.Fatalf("unused key reports activity: %s", before.Body)
	}

	// A request arrived: the gateway updates last_used_at and records the row.
	env.now = env.now.Add(2 * time.Second)
	if _, err := env.db.W.ExecContext(context.Background(),
		`UPDATE router_keys SET last_used_at = ? WHERE id = ?`, "2026-01-02T03:04:07.000Z", "key-1"); err != nil {
		t.Fatalf("touch key: %v", err)
	}
	if _, err := env.db.W.ExecContext(context.Background(),
		`INSERT INTO requests (id, user_id, router_key_id, created_at, status, model_label, requested_model, error_code, error_message, latency_ms)
		 VALUES ('req-1', 'user-plain', 'key-1', ?, 'error', NULL, 'spark/qwen3.8', 'MODEL_NOT_FOUND', 'The model does not exist.', 12)`,
		env.now.UnixMilli()); err != nil {
		t.Fatalf("insert request: %v", err)
	}
	failed := env.do("user-token", http.MethodGet, "/api/v1/apps/keys/key-1/activity", "")
	decodeBody(t, failed, &activity)
	if activity.LastUsedAt == nil || *activity.LastUsedAt != env.now.UnixMilli() {
		t.Errorf("lastUsedAt = %v", activity.LastUsedAt)
	}
	if activity.Request == nil {
		t.Fatal("no request recorded")
	}
	if activity.Request.OK {
		t.Error("failed request reported as ok")
	}
	if got := deref(activity.Request.ErrorCode); got != "MODEL_NOT_FOUND" {
		t.Errorf("errorCode = %q", got)
	}
	if got := deref(activity.Request.Requested); got != "spark/qwen3.8" {
		t.Errorf("requested = %q", got)
	}
	// model_label is null, so the requested name stands in for it.
	if got := deref(activity.Request.Model); got != "spark/qwen3.8" {
		t.Errorf("model = %q", got)
	}

	// A newer failure from the gateway's error handler wins over the stored row.
	env.now = env.now.Add(time.Second)
	env.svc.RecordFailure("key-1", Failure{Status: 404, Code: "MODEL_NOT_FOUND", Message: "Did you mean \"spark/qwen3.8-flash-next\"?", Model: "spark/qwen3.8"})
	reported := env.do("user-token", http.MethodGet, "/api/v1/apps/keys/key-1/activity", "")
	decodeBody(t, reported, &activity)
	if activity.Request == nil || activity.Request.OK {
		t.Fatalf("failure not reported: %s", reported.Body)
	}
	if activity.Request.Model != nil {
		t.Errorf("failure model = %v, want null", *activity.Request.Model)
	}
	if activity.Request.LatencyMs != nil {
		t.Errorf("failure latency = %v, want null", *activity.Request.LatencyMs)
	}
	if got := deref(activity.Request.ErrorMessage); !strings.Contains(got, "Did you mean") {
		t.Errorf("errorMessage = %q", got)
	}
	if !strings.Contains(reported.Body.String(), `"lastUsedAt":`) || !strings.Contains(reported.Body.String(), `"errorCode":"MODEL_NOT_FOUND"`) {
		t.Errorf("key order or nullable shape changed: %s", reported.Body)
	}

	// Somebody else's key is not found.
	other := env.do("admin-token", http.MethodGet, "/api/v1/apps/keys/key-1/activity", "")
	if other.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", other.Code)
	}
	if !strings.Contains(other.Body.String(), `"code":"KEY_NOT_FOUND"`) || !strings.Contains(other.Body.String(), `"message":"This key does not exist."`) {
		t.Errorf("body = %s", other.Body)
	}
}

// TestRecordFailureKeepsNewestOnly ports key_activity's one-entry-per-key map.
func TestRecordFailureKeepsNewestOnly(t *testing.T) {
	env := newTestEnv(t)
	env.insertKey("key-1", "user-plain", "Pi Dev", "")
	env.svc.RecordFailure("key-1", Failure{Status: 404, Code: "MODEL_NOT_FOUND", Message: "first", Model: "a"})
	env.now = env.now.Add(time.Second)
	env.svc.RecordFailure("key-1", Failure{Status: 400, Code: "INVALID_REQUEST", Message: "second", Model: "b"})
	activity, err := env.svc.Activity(context.Background(), "user-plain", "key-1")
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if got := deref(activity.Request.ErrorMessage); got != "second" {
		t.Errorf("message = %q, want the newest", got)
	}
	// An empty key id is ignored, and pruning drops the oldest keys.
	env.svc.RecordFailure("", Failure{Status: 500})
	env.svc.ResetForTests()
	activity, err = env.svc.Activity(context.Background(), "user-plain", "key-1")
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if activity.Request != nil {
		t.Errorf("reset kept a failure: %+v", activity.Request)
	}
}

// TestNetworkScope ports "only an admin opens NeoRouter to containers and the
// network".
func TestNetworkScope(t *testing.T) {
	env := newTestEnv(t)
	for _, key := range []string{"NEOROUTER_CONTAINER", "KUBERNETES_SERVICE_HOST", "container", "INVOCATION_ID", "XPC_SERVICE_NAME"} {
		delete(env.env, key)
	}

	denied := env.do("user-token", http.MethodPut, "/api/v1/apps/network", `{"scope":"network"}`)
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), `"code":"ADMIN_REQUIRED"`) {
		t.Fatalf("status = %d, body %s", denied.Code, denied.Body)
	}

	container := detectContainer(env.svc.getenv)
	opened := env.do("admin-token", http.MethodPut, "/api/v1/apps/network", `{"scope":"network"}`)
	if opened.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", opened.Code, opened.Body)
	}
	var body struct {
		Scope         string  `json:"scope"`
		Restarting    bool    `json:"restarting"`
		RestartNeeded bool    `json:"restartNeeded"`
		Places        []Place `json:"places"`
	}
	decodeBody(t, opened, &body)
	if body.Restarting {
		t.Error("restarting without launchd or systemd")
	}
	if !container {
		if !body.RestartNeeded {
			t.Error("restartNeeded is false outside a container")
		}
		envFile, err := os.ReadFile(env.cfg.Paths.EnvFile)
		if err != nil {
			t.Fatalf("read .env: %v", err)
		}
		if !strings.Contains(string(envFile), "NEOROUTER_HOST=0.0.0.0\n") {
			t.Errorf(".env = %q", envFile)
		}
		if len(body.Places) != 4 {
			t.Errorf("places = %d, want 4", len(body.Places))
		}
		closed := env.do("admin-token", http.MethodPut, "/api/v1/apps/network", `{"scope":"local"}`)
		if closed.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", closed.Code, closed.Body)
		}
		envFile, err = os.ReadFile(env.cfg.Paths.EnvFile)
		if err != nil {
			t.Fatalf("read .env: %v", err)
		}
		if !strings.Contains(string(envFile), "NEOROUTER_HOST=127.0.0.1\n") {
			t.Errorf(".env = %q", envFile)
		}
	}
}

// TestNetworkRestartsUnderServiceManager covers the systemd and launchd path.
func TestNetworkRestartsUnderServiceManager(t *testing.T) {
	env := newTestEnv(t)
	env.env["INVOCATION_ID"] = "systemd-run"
	result, err := env.svc.SetScope("network")
	if err != nil {
		t.Fatalf("set scope: %v", err)
	}
	if !result.Restarting || result.RestartNeeded {
		t.Errorf("result = %+v", result)
	}
	select {
	case <-env.restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("the process was not asked to restart")
	}
}

// TestNetworkScopeValidation ports the zod enum the route validates with.
func TestNetworkScopeValidation(t *testing.T) {
	env := newTestEnv(t)
	cases := []struct {
		body    string
		message string
	}{
		{`{}`, "scope: Required"},
		{`{"scope":"x"}`, "scope: Invalid enum value. Expected 'local' | 'network', received 'x'"},
		{`{"scope":1}`, "scope: Expected 'local' | 'network', received number"},
		{`{"scope":null}`, "scope: Expected 'local' | 'network', received null"},
		{`{"scope":true}`, "scope: Expected 'local' | 'network', received boolean"},
	}
	for _, testCase := range cases {
		response := env.do("admin-token", http.MethodPut, "/api/v1/apps/network", testCase.body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", testCase.body, response.Code)
		}
		var body struct {
			Error struct {
				Code    string          `json:"code"`
				Message string          `json:"message"`
				Details json.RawMessage `json:"details"`
			} `json:"error"`
		}
		decodeBody(t, response, &body)
		if body.Error.Code != "VALIDATION_ERROR" || body.Error.Message != testCase.message {
			t.Errorf("%s: %+v", testCase.body, body.Error)
		}
		if !strings.Contains(string(body.Error.Details), `"fieldErrors":{"scope":["`) {
			t.Errorf("%s: details = %s", testCase.body, body.Error.Details)
		}
	}
}

// TestAppsRoutesNeedASession covers the requireSession the /apps routes sit behind.
func TestAppsRoutesNeedASession(t *testing.T) {
	env := newTestEnv(t)
	for _, target := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/apps"},
		{http.MethodGet, "/api/v1/apps/keys/key-1/activity"},
		{http.MethodPut, "/api/v1/apps/network"},
	} {
		response := env.do("", target.method, target.path, "")
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d", target.method, target.path, response.Code)
		}
		if !strings.Contains(response.Body.String(), `"code":"AUTHENTICATION_REQUIRED"`) {
			t.Errorf("%s %s: body = %s", target.method, target.path, response.Body)
		}
	}
	// A space the caller does not belong to looks like it does not exist.
	request := httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil)
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-NeoRouter-Space", "user-admin")
	recorder := httptest.NewRecorder()
	env.mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"SPACE_NOT_FOUND"`) {
		t.Errorf("foreign space = %d %s", recorder.Code, recorder.Body)
	}
}

func TestDetectContainerReadsTheEnvironment(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{"NEOROUTER_CONTAINER": "yes"}, true},
		{map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"}, true},
		{map[string]string{"container": "podman"}, true},
		{map[string]string{"container": "host"}, false},
	}
	for _, testCase := range cases {
		lookup := func(key string) string { return testCase.env[key] }
		if got := detectContainer(lookup); got != testCase.want {
			t.Errorf("%v: got %v", testCase.env, got)
		}
	}
}
