package teams_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
)

const testPassword = "SI_SECRET_PASSWORD_J3H7W06F"

// gate is the settings domain's answer to "is registration open".
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

// harness is a teams server wired the way internal/app will wire it: the auth
// domain supplies the session and the actor, the teams domain supplies the space
// and the team endpoints, and the paths that belong to other domains are
// stand-ins that only read the resolved space.
type harness struct {
	t       *testing.T
	auth    *auth.Service
	teams   *teams.Service
	db      *dbx.DB
	gate    *gate
	handler http.Handler

	ownerID    string
	ownerToken string
}

// newHarness is a server with the bootstrap owner account already registered.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := newServer(t)
	h.registerBootstrap()
	return h
}

// newServer is the same server with no account yet, for tests that register
// their own first user.
func newServer(t *testing.T) *harness {
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

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfgFn := func() config.Config { return cfg }
	g := &gate{}
	authService := &auth.Service{DB: db, Cfg: cfgFn, Log: quiet, Settings: g}
	teamService := &teams.Service{DB: db, Cfg: cfgFn, Log: quiet, Actor: auth.Actor}

	root := http.NewServeMux()
	authService.Routes(root)

	// The protected API, mounted the way routes/index.js nests it: the session
	// middleware first, then the space resolution, then the routers. Teams
	// guards its own endpoints with the own-space rule; the stand-ins for the
	// keys and providers domains guard themselves with the permission middleware.
	protected := http.NewServeMux()
	teamService.Routes(protected)
	protected.Handle("GET /api/v1/keys", teams.RequirePermission("keys.view")(http.HandlerFunc(echoSpace)))
	protected.Handle("POST /api/v1/keys", teams.RequirePermission("keys.create")(http.HandlerFunc(echoSpace)))
	protected.Handle("GET /api/v1/providers/accounts", teams.RequirePermission("providers.view")(http.HandlerFunc(echoSpace)))

	root.Handle("/api/v1/", authService.Middleware(teamService.SpaceMiddleware(auth.Actor, protected)))

	return &harness{t: t, auth: authService, teams: teamService, db: db, gate: g, handler: root}
}

// echoSpace answers with whose space the request resolved to, which is what a
// data endpoint does with teams.SpaceFromContext.
func echoSpace(w http.ResponseWriter, r *http.Request) {
	space, ok := teams.SpaceFromContext(r.Context())
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	io.WriteString(w, `{"owner":"`+space.OwnerID+`","actor":"`+space.ActorID+`"}`)
}

// registerBootstrap creates the administrator the team tests start from.
func (h *harness) registerBootstrap() {
	h.t.Helper()
	response := h.do("POST", "/api/v1/auth/register", map[string]any{
		"username": "owner", "email": "owner@example.test", "password": testPassword,
	}, "", "")
	if response.status != http.StatusCreated {
		h.t.Fatalf("register owner: %d %s", response.status, response.body)
	}
	h.ownerToken = path(h.t, response.object(h.t), "session", "token").(string)
	h.ownerID = path(h.t, response.object(h.t), "user", "id").(string)
}

// register creates an ordinary account and returns its token and id.
func (h *harness) register(username string) (token, id string) {
	h.t.Helper()
	h.gate.set(true)
	response := h.do("POST", "/api/v1/auth/register", map[string]any{
		"username": username, "email": username + "@example.test", "password": testPassword,
	}, "", "")
	if response.status != http.StatusCreated {
		h.t.Fatalf("register %s: %d %s", username, response.status, response.body)
	}
	return path(h.t, response.object(h.t), "session", "token").(string),
		path(h.t, response.object(h.t), "user", "id").(string)
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

func (r response) code(t *testing.T) string {
	t.Helper()
	if value, ok := path(t, r.object(t), "error", "code").(string); ok {
		return value
	}
	return ""
}

func (h *harness) do(method, target string, body any, token, space string) response {
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

// invite mints an invite link for the owner and returns it.
func (h *harness) invite(body map[string]any) string {
	h.t.Helper()
	if _, ok := body["permissions"]; !ok {
		body["permissions"] = []string{"keys.view"}
	}
	response := h.do("POST", "/api/v1/teams/invites", body, h.ownerToken, "")
	if response.status != http.StatusCreated {
		h.t.Fatalf("create invite: %d %s", response.status, response.body)
	}
	return path(h.t, response.object(h.t), "link").(string)
}

// path walks decoded JSON by keys and slice indexes.
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

// orderedKeys returns the top-level keys of a JSON object in document order.
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
