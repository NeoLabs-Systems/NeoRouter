package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/auth"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
	"golang.org/x/crypto/bcrypt"
)

// gate is the settings domain's answer to "is registration open", under the
// test's control.
type gate struct {
	mu   sync.Mutex
	open bool
}

func (g *gate) RegistrationOpen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open
}

func (g *gate) set(open bool) {
	g.mu.Lock()
	g.open = open
	g.mu.Unlock()
}

// harness is one auth server on a real, temporary SQLite database.
type harness struct {
	t        *testing.T
	service  *auth.Service
	db       *dbx.DB
	gate     *gate
	handler  http.Handler
	cfg      config.Config
	token    string // the bootstrap administrator's session
	adminID  string
	password string

	clockMu sync.Mutex
	clock   time.Time
}

// now is the service's clock. It only moves when a test moves it, so neither
// the lockout nor authenticator codes depend on how long a bcrypt compare takes.
func (h *harness) now() time.Time {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	return h.clock
}

func (h *harness) advance(d time.Duration) {
	h.clockMu.Lock()
	defer h.clockMu.Unlock()
	h.clock = h.clock.Add(d)
}

const testPassword = "correct horse battery staple"

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	cfg := config.Config{
		Paths: config.Paths{
			Home:      dir,
			Data:      data,
			Logs:      filepath.Join(dir, "logs"),
			Backups:   filepath.Join(dir, "backups"),
			Database:  filepath.Join(data, "neorouter.sqlite3"),
			SecretKey: filepath.Join(data, "secret.key"),
		},
		SessionTTL: 30 * 24 * time.Hour,
	}
	ctx := context.Background()
	db, err := dbx.Open(ctx, cfg.Paths.Database, dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("prepare schema: %v", err)
	}
	g := &gate{}
	service := &auth.Service{
		DB:       db,
		Cfg:      func() config.Config { return cfg },
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Settings: g,
		HashCost: bcrypt.MinCost,
	}
	mux := http.NewServeMux()
	service.Routes(mux)
	h := &harness{t: t, service: service, db: db, gate: g, handler: mux, cfg: cfg, password: testPassword, clock: time.Now()}
	service.Now = h.now
	return h
}

// registerAdmin creates the bootstrap account every test starts from.
func (h *harness) registerAdmin() {
	h.t.Helper()
	response := h.do("POST", "/api/v1/auth/register", map[string]any{
		"username": "admin",
		"email":    "admin@example.test",
		"password": h.password,
	}, "")
	if response.status != http.StatusCreated {
		h.t.Fatalf("register admin: status %d body %s", response.status, response.body)
	}
	h.token = path(h.t, response.object(h.t), "session", "token").(string)
	h.adminID = path(h.t, response.object(h.t), "user", "id").(string)
}

type response struct {
	status int
	body   []byte
	header http.Header
}

func (r response) object(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("decode response %s: %v", r.body, err)
	}
	return out
}

// code is the machine code of an error response.
func (r response) code(t *testing.T) string {
	t.Helper()
	if value, ok := path(t, r.object(t), "error", "code").(string); ok {
		return value
	}
	return ""
}

func (h *harness) do(method, target string, body any, token string) response {
	return h.doAs(method, target, body, token, "")
}

// doAs sends a request with the session and, when space is not empty, the
// X-NeoRouter-Space header the team tests work with.
func (h *harness) doAs(method, target string, body any, token, space string) response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, target, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if space != "" {
		request.Header.Set("X-NeoRouter-Space", space)
	}
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	return response{status: recorder.Code, body: recorder.Body.Bytes(), header: recorder.Header()}
}

// register creates an ordinary account through the API, opening the gate first.
func (h *harness) register(username string) (token, id string) {
	h.t.Helper()
	h.gate.set(true)
	response := h.do("POST", "/api/v1/auth/register", map[string]any{
		"username": username,
		"email":    username + "@example.test",
		"password": h.password,
	}, "")
	if response.status != http.StatusCreated {
		h.t.Fatalf("register %s: status %d body %s", username, response.status, response.body)
	}
	return path(h.t, response.object(h.t), "session", "token").(string), path(h.t, response.object(h.t), "user", "id").(string)
}

// signIn signs an existing account in and returns its session token.
func (h *harness) signIn(username string) string {
	h.t.Helper()
	response := h.do("POST", "/api/v1/auth/login", map[string]any{
		"account": username, "password": h.password,
	}, "")
	if response.status != http.StatusOK {
		h.t.Fatalf("sign in as %s: status %d body %s", username, response.status, response.body)
	}
	return path(h.t, response.object(h.t), "session", "token").(string)
}

// context is the background context of a test.
func (h *harness) context() context.Context { return context.Background() }

// roleOf reads an account's role from the database.
func (h *harness) roleOf(userID string) string {
	h.t.Helper()
	var role string
	if err := h.db.R.QueryRow(`SELECT role FROM users WHERE id = ?`, userID).Scan(&role); err != nil {
		h.t.Fatalf("read role: %v", err)
	}
	return role
}

// assertHTTPError checks the status and machine code of a service error.
func assertHTTPError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var failure *httpserver.HttpError
	if !errors.As(err, &failure) {
		t.Fatalf("expected an *HttpError, got %v", err)
	}
	if failure.Status != status || failure.Code != code {
		t.Fatalf("got %d %s (%q), want %d %s", failure.Status, failure.Code, failure.Message, status, code)
	}
}

// doGuarded runs a middleware-guarded handler for the account behind token.
func doGuarded(t *testing.T, handler http.Handler, token string, service *auth.Service) response {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	service.Middleware(handler).ServeHTTP(recorder, request)
	return response{status: recorder.Code, body: recorder.Body.Bytes(), header: recorder.Header()}
}

// path walks decoded JSON by keys and slice indexes ("permissions", "0").
func path(t *testing.T, value any, keys ...string) any {
	t.Helper()
	current := value
	for _, key := range keys {
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[key]
			if !ok {
				return nil
			}
			current = next
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(node) {
				return nil
			}
			current = node[index]
		default:
			return nil
		}
	}
	return current
}

// orderedKeys returns the top-level keys of a JSON object in document order,
// which is how the byte-for-byte JSON contract is checked.
func orderedKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		t.Fatalf("read object %s: %v", raw, err)
	}
	keys := make([]string, 0, 4)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("read key %s: %v", raw, err)
		}
		keys = append(keys, token.(string))
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			t.Fatalf("skip value %s: %v", raw, err)
		}
	}
	return keys
}

// nested returns the raw JSON of one field, keeping its key order.
func nested(t *testing.T, raw []byte, key string) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return fields[key]
}

func equalStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("%s: got %v, want %v", what, got, want)
		}
	}
}

// anyStrings converts a decoded JSON array to []string.
func anyStrings(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected an array, got %v", value)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("expected strings, got %v", item)
		}
		out = append(out, text)
	}
	return out
}
