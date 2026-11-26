package localmodels

// Tests of the local model API — what the page shows, who may manage and use
// the models, installing, starting and stopping, the starter set, the privacy
// filter and the memory budget. The engine, the machine, the Hugging Face file
// list and the memory figures are replaced by fixtures, so nothing is
// downloaded and no model runs.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

const (
	adminToken  = "admin-token"
	memberToken = "member-token"
	adminID     = "user-admin"
	memberID    = "user-member"
)

// fakeProcess is a model process the test crashes on purpose.
type fakeProcess struct {
	done chan struct{}
	once sync.Once
	exit Exit
}

func newFakeProcess() *fakeProcess { return &fakeProcess{done: make(chan struct{})} }

func (p *fakeProcess) PID() int              { return 0 }
func (p *fakeProcess) Done() <-chan struct{} { return p.done }
func (p *fakeProcess) Exit() Exit            { return p.exit }
func (p *fakeProcess) Output() string        { return "" }
func (p *fakeProcess) Ready()                {}
func (p *fakeProcess) finish(code *int, signal string) {
	p.once.Do(func() {
		p.exit = Exit{Code: code, Signal: signal}
		close(p.done)
	})
}

// Stop ends the process like a clean exit.
func (p *fakeProcess) Stop() {
	code := 0
	p.finish(&code, "")
}

// Crash ends the process like a signal, which the runner reports as a crash.
func (p *fakeProcess) Crash() { p.finish(nil, "SIGKILL") }

// fakeEngine runs every model instantly and hands back an instance the test
// can inspect.
type fakeEngine struct {
	mu      sync.Mutex
	started []*fakeProcess
}

func (f *fakeEngine) Support(machine Machine) Support { return supported() }

func (f *fakeEngine) Prepare(ctx context.Context, machine Machine, opts PrepareOptions) error {
	return nil
}

func (f *fakeEngine) Start(ctx context.Context, in StartInput) (*Instance, error) {
	process := newFakeProcess()
	f.mu.Lock()
	f.started = append(f.started, process)
	f.mu.Unlock()
	window := 8192
	return &Instance{
		Proc:          process,
		Endpoint:      &Endpoint{BaseURL: "http://127.0.0.1:1/v1", Token: "good-key"},
		ContextWindow: &window,
		GPU:           true,
		Health:        func(context.Context) bool { return true },
	}, nil
}

func (f *fakeEngine) last() *fakeProcess {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.started) == 0 {
		return nil
	}
	return f.started[len(f.started)-1]
}

func (f *fakeEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

// fakeHardware describes the pretend server.
type fakeHardware struct {
	machine Machine
}

func (f *fakeHardware) Detect(ctx context.Context, fresh bool) (Machine, error) {
	return f.machine, nil
}

// emptyHFFiles resolves no files.
type emptyHFFiles struct{}

func (emptyHFFiles) Resolve(ctx context.Context, entry *Entry) ([]File, error) {
	return []File{}, nil
}

// memSettings is internal/settings, in memory.
type memSettings struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemSettings() *memSettings { return &memSettings{values: map[string]string{}} }

func (m *memSettings) GetGlobalString(ctx context.Context, key, def string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value, ok := m.values[key]; ok {
		return value
	}
	return def
}

func (m *memSettings) SetGlobal(ctx context.Context, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var text string
	if err := json.Unmarshal(encoded, &text); err != nil {
		return err
	}
	m.values[key] = text
	return nil
}

// harness is the running server of one test.
type harness struct {
	t        *testing.T
	db       *dbx.DB
	svc      *Service
	routes   *Routes
	server   *httptest.Server
	engine   *fakeEngine
	hardware *fakeHardware
	sampler  *Sampler
	settings *memSettings

	mu           sync.Mutex
	connectCalls []string
	privacy      PrivacyStatus
	privacyCalls []bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	for _, user := range []struct{ id, name, role string }{
		{adminID, "admin", "admin"},
		{memberID, "user", "user"},
	} {
		if _, err := db.W.ExecContext(ctx,
			`INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, 'x', ?)`,
			user.id, user.name, user.name+"@example.test", user.role); err != nil {
			t.Fatalf("insert user: %v", err)
		}
	}

	h := &harness{t: t, db: db, settings: newMemSettings(), engine: &fakeEngine{}}
	h.hardware = &fakeHardware{machine: Machine{
		Platform: "linux",
		Arch:     "x64",
		CPU:      CPU{Name: "Test CPU", Cores: 8},
		Memory:   Memory{TotalMB: 32768},
		GPU:      &GPU{Type: "cuda", Name: "Test GPU", MemoryMB: 24576, CUDA: "13.0"},
		Disk:     Disk{FreeMB: int64ptr(500_000), TotalMB: int64ptr(1_000_000)},
	}}
	h.sampler = NewSampler()
	h.sampler.SetForTests(&SamplerFigures{Others: int64ptr(2048)})
	h.privacy = PrivacyStatus{Model: privacyModelID, MemoryMB: privacyMemoryMB, Status: "off"}

	svc, err := New(Options{
		DB:       db,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Settings: h.settings,
		PrivacyStatus: func() PrivacyStatus {
			h.mu.Lock()
			defer h.mu.Unlock()
			status := h.privacy
			status.Enabled = status.Status != "off"
			return status
		},
		SetPrivacyEnabled: func(enabled bool) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.privacyCalls = append(h.privacyCalls, enabled)
			if enabled {
				h.privacy.Status = "idle"
			} else {
				h.privacy.Status = "off"
			}
		},
		ConnectAccount: func(ctx context.Context, userID string) error {
			h.mu.Lock()
			h.connectCalls = append(h.connectCalls, userID)
			h.mu.Unlock()
			// The providers domain would insert the account; the schema is
			// enough for the access checks here.
			_, err := db.W.ExecContext(ctx,
				`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode, created_by_user_id)
				 VALUES (?, ?, ?, 'local', 'NeoRouter Local', 'free_tier', ?)`,
				"acct-"+userID, userID, DefinitionID, userID)
			return err
		},
		SyncAccounts: func(ctx context.Context, accountID string) error { return nil },
		Hardware:     h.hardware,
		HFFiles:      emptyHFFiles{},
		Engines:      map[string]Engine{EngineLlamaCpp: h.engine, EngineTransformers: h.engine},
		DataDir:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	svc.Memory = h.sampler
	// Every llama.cpp build this pretend machine could use counts as installed,
	// so the installer reports no runtime bytes left to fetch and never asks
	// GitHub.
	for _, variant := range LlamaVariants(h.hardware.machine) {
		variantDir := filepath.Join(svc.Paths.RuntimesDir, "llama.cpp", LlamaCppBuild+"-"+variant.ID)
		if err := os.MkdirAll(variantDir, 0o700); err != nil {
			t.Fatalf("create runtime dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(variantDir, LlamaCppBinaryName), nil, 0o700); err != nil {
			t.Fatalf("create runtime binary: %v", err)
		}
	}
	h.svc = svc
	t.Cleanup(func() { svc.ResetForTests(context.Background()) })

	gate := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Header.Get("Authorization") {
			case "Bearer " + adminToken:
				r.Header.Set("X-Test-User", adminID)
			case "Bearer " + memberToken:
				r.Header.Set("X-Test-User", memberID)
			default:
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":{"code":"AUTHENTICATION_REQUIRED","message":"A valid session is required."}}`)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	adminOnly := func(next http.Handler) http.Handler {
		return gate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Test-User") != adminID {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"error":{"code":"ADMIN_REQUIRED","message":"Administrator privileges are required."}}`)
				return
			}
			next.ServeHTTP(w, r)
		}))
	}
	h.routes = &Routes{
		Svc:   svc,
		Log:   svc.Log,
		Admin: adminOnly,
		UserID: func(r *http.Request) (string, bool) {
			user := r.Header.Get("X-Test-User")
			return user, user != ""
		},
	}
	mux := http.NewServeMux()
	h.routes.Mount(mux)
	h.server = httptest.NewServer(mux)
	t.Cleanup(h.server.Close)
	return h
}

func int64ptr(value int64) *int64 { return &value }

// api calls one route as the given token, returning the status and the body.
func (h *harness) api(method, path, token string, body any) (int, []byte) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, h.server.URL+"/api/v1/admin/local-models"+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := h.server.Client().Do(request)
	if err != nil {
		h.t.Fatalf("call %s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, raw
}

// apiRaw calls one route with a raw body, for the validation cases.
func (h *harness) apiRaw(method, path, token, body string) (int, []byte) {
	h.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, h.server.URL+"/api/v1/admin/local-models"+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := h.server.Client().Do(request)
	if err != nil {
		h.t.Fatalf("call %s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, raw
}

// overview reads the page.
func (h *harness) overview() Overview {
	h.t.Helper()
	status, raw := h.api(http.MethodGet, "", adminToken, nil)
	if status != http.StatusOK {
		h.t.Fatalf("overview status = %d body %s", status, raw)
	}
	var page Overview
	if err := json.Unmarshal(raw, &page); err != nil {
		h.t.Fatalf("decode overview: %v (%s)", err, raw)
	}
	return page
}

// waitFor polls the page until a model reaches a state.
func (h *harness) waitFor(id, state string) InstalledModel {
	h.t.Helper()
	for attempt := 0; attempt < 200; attempt++ {
		page := h.overview()
		for _, model := range page.Installed {
			if model.ID == id && model.State == state {
				return model
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	page := h.overview()
	for _, model := range page.Installed {
		if model.ID == id {
			h.t.Fatalf("%s never became %s: state %q problem %+v", id, state, model.State, model.Problem)
		}
	}
	h.t.Fatalf("%s never became %s: it is not installed", id, state)
	return InstalledModel{}
}

func TestAdminsSeeTheServerAnEmptyListAStarterSetAndTheModelsThatFit(t *testing.T) {
	h := newHarness(t)
	page := h.overview()

	kinds := []string{}
	for _, item := range page.Starter {
		kinds = append(kinds, item.Kind)
	}
	if len(kinds) != 3 || kinds[0] != "chat" || kinds[1] != "transcription" || kinds[2] != "speech" {
		t.Fatalf("starter kinds = %v", kinds)
	}
	if page.System.GPU == nil || page.System.GPU.Name != "Test GPU" {
		t.Fatalf("system gpu = %+v", page.System.GPU)
	}
	if len(page.Installed) != 0 {
		t.Fatalf("installed = %v", page.Installed)
	}
	if page.Access != "admins" {
		t.Fatalf("access = %q", page.Access)
	}
	byID := map[string]CatalogView{}
	for _, entry := range page.Catalog {
		byID[entry.ID] = entry
	}
	if byID["qwen3.5-4b"].Rating != RatingGreat {
		t.Fatalf("qwen3.5-4b rating = %q", byID["qwen3.5-4b"].Rating)
	}
	if byID["gpt-oss-120b"].Rating != RatingTooBig {
		t.Fatalf("gpt-oss-120b rating = %q", byID["gpt-oss-120b"].Rating)
	}
	if byID["qwen3.5-4b"].Summary.De == "" {
		t.Fatalf("qwen3.5-4b summary = %+v", byID["qwen3.5-4b"].Summary)
	}
	if page.Memory.RoomMB != 32768-2048-1024 {
		t.Fatalf("roomMb = %d", page.Memory.RoomMB)
	}
}

func TestOnlyAdminsManageLocalModels(t *testing.T) {
	h := newHarness(t)
	if status, _ := h.api(http.MethodGet, "", memberToken, nil); status != http.StatusForbidden {
		t.Fatalf("member overview status = %d, want 403", status)
	}
	if status, _ := h.api(http.MethodPost, "/qwen3.5-0.8b/install", memberToken, nil); status != http.StatusForbidden {
		t.Fatalf("member install status = %d, want 403", status)
	}
}

func TestModelsThatDoNotFitOrDoNotExistAreRefused(t *testing.T) {
	h := newHarness(t)
	if status, raw := h.api(http.MethodPost, "/gpt-oss-120b/install", adminToken, nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("too big status = %d body %s", status, raw)
	} else if !bytes.Contains(raw, []byte(`"code":"LOCAL_MODEL_TOO_BIG"`)) {
		t.Fatalf("too big body = %s", raw)
	}
	if status, raw := h.api(http.MethodPost, "/no-such-model/install", adminToken, nil); status != http.StatusNotFound {
		t.Fatalf("unknown status = %d body %s", status, raw)
	} else if !bytes.Contains(raw, []byte(`"code":"LOCAL_MODEL_UNKNOWN"`)) {
		t.Fatalf("unknown body = %s", raw)
	}
	if status, raw := h.api(http.MethodPost, "/qwen3.5-0.8b/stop", adminToken, nil); status != http.StatusNotFound {
		t.Fatalf("not installed status = %d body %s", status, raw)
	}
}

func TestAnInstalledModelStartsByItselfAndTheAdminIsConnected(t *testing.T) {
	h := newHarness(t)
	if status, raw := h.api(http.MethodPost, "/qwen3.5-0.8b/install", adminToken, nil); status != http.StatusAccepted {
		t.Fatalf("install status = %d body %s", status, raw)
	}
	model := h.waitFor("qwen3.5-0.8b", StateRunning)
	if model.GPU == nil || !*model.GPU {
		t.Fatalf("model gpu = %v", model.GPU)
	}
	h.mu.Lock()
	calls := append([]string{}, h.connectCalls...)
	h.mu.Unlock()
	if len(calls) != 1 || calls[0] != adminID {
		t.Fatalf("connect calls = %v", calls)
	}
	served := h.svc.ServedModels()
	if len(served) != 1 || served[0].Entry.ID != "qwen3.5-0.8b" || served[0].ContextWindow == nil || *served[0].ContextWindow != 8192 {
		t.Fatalf("served models = %+v", served)
	}
	if instance := h.svc.RunningInstance("qwen3.5-0.8b"); instance == nil || instance.Endpoint == nil {
		t.Fatalf("running instance = %+v", instance)
	}
}

func TestAModelThatDoesNotFitBesideTheRunningOnesWaitsAndStartsOnRetry(t *testing.T) {
	h := newHarness(t)
	// Nothing else uses memory here, so only the fixed reserve is kept.
	h.sampler.SetForTests(&SamplerFigures{Others: int64ptr(0)})
	if status, raw := h.api(http.MethodPost, "/gemma-4-26b-a4b/install", adminToken, nil); status != http.StatusAccepted {
		t.Fatalf("install gemma status = %d body %s", status, raw)
	}
	h.waitFor("gemma-4-26b-a4b", StateRunning)

	if status, raw := h.api(http.MethodPost, "/qwen3.6-35b-a3b/install", adminToken, nil); status != http.StatusAccepted {
		t.Fatalf("install qwen status = %d body %s", status, raw)
	}
	refused := h.waitFor("qwen3.6-35b-a3b", StateFailed)
	problem, ok := refused.Problem.(map[string]any)
	if !ok || problem["code"] != "not_enough_memory" {
		t.Fatalf("refused problem = %+v", refused.Problem)
	}

	if status, raw := h.api(http.MethodPost, "/gemma-4-26b-a4b/stop", adminToken, nil); status != http.StatusOK {
		t.Fatalf("stop gemma status = %d body %s", status, raw)
	}
	if status, raw := h.api(http.MethodPost, "/qwen3.6-35b-a3b/retry", adminToken, nil); status != http.StatusOK {
		t.Fatalf("retry status = %d body %s", status, raw)
	}
	h.waitFor("qwen3.6-35b-a3b", StateRunning)
}

func TestStoppingStartingAndRemovingAModel(t *testing.T) {
	h := newHarness(t)
	if status, raw := h.api(http.MethodPost, "/qwen3.5-0.8b/install", adminToken, nil); status != http.StatusAccepted {
		t.Fatalf("install status = %d body %s", status, raw)
	}
	h.waitFor("qwen3.5-0.8b", StateRunning)

	status, raw := h.api(http.MethodPost, "/qwen3.5-0.8b/stop", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("stop status = %d body %s", status, raw)
	}
	var page Overview
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode stop: %v", err)
	}
	if len(page.Installed) != 1 || page.Installed[0].State != StateStopped {
		t.Fatalf("stopped page = %+v", page.Installed)
	}

	if status, raw := h.api(http.MethodPost, "/qwen3.5-0.8b/start", adminToken, nil); status != http.StatusOK {
		t.Fatalf("start status = %d body %s", status, raw)
	}
	h.waitFor("qwen3.5-0.8b", StateRunning)

	status, raw = h.api(http.MethodDelete, "/qwen3.5-0.8b", adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("remove status = %d body %s", status, raw)
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode remove: %v", err)
	}
	for _, model := range page.Installed {
		if model.ID == "qwen3.5-0.8b" {
			t.Fatalf("model still installed after remove")
		}
	}
	for _, entry := range page.Catalog {
		if entry.ID == "qwen3.5-0.8b" && entry.Installed {
			t.Fatalf("catalog still says installed")
		}
	}
}

func TestTheStarterSetInstallsInOneGo(t *testing.T) {
	h := newHarness(t)
	suggested := []string{}
	for _, item := range h.overview().Starter {
		suggested = append(suggested, item.ID)
	}
	if len(suggested) != 3 {
		t.Fatalf("starter set = %v", suggested)
	}
	status, raw := h.api(http.MethodPost, "/starter", adminToken, nil)
	if status != http.StatusAccepted {
		t.Fatalf("starter status = %d body %s", status, raw)
	}
	var page Overview
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode starter: %v", err)
	}
	if len(page.Starter) != 0 {
		t.Fatalf("starter suggestion after install = %v", page.Starter)
	}
	for _, id := range suggested {
		h.waitFor(id, StateRunning)
	}
}

func TestThePrivacyFilterIsSwitchedAndKeepsItsMemoryFree(t *testing.T) {
	h := newHarness(t)
	before := h.overview()
	if before.PrivacyFilter.Enabled {
		t.Fatalf("privacy filter starts enabled")
	}
	if before.Memory.OthersMB == nil || *before.Memory.OthersMB != 2048 {
		t.Fatalf("othersMb = %v", before.Memory.OthersMB)
	}
	if before.Memory.RoomMB != 32768-2048-1024 {
		t.Fatalf("roomMb = %d", before.Memory.RoomMB)
	}
	if status, _ := h.api(http.MethodPut, "/privacy-filter", memberToken, map[string]any{"enabled": true}); status != http.StatusForbidden {
		t.Fatalf("member privacy status = %d, want 403", status)
	}
	status, raw := h.api(http.MethodPut, "/privacy-filter", adminToken, map[string]any{"enabled": true})
	if status != http.StatusOK {
		t.Fatalf("privacy status = %d body %s", status, raw)
	}
	var page Overview
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode privacy: %v", err)
	}
	if !page.PrivacyFilter.Enabled {
		t.Fatalf("privacy filter not enabled: %+v", page.PrivacyFilter)
	}
	if page.Memory.UsedMB != before.Memory.UsedMB+page.PrivacyFilter.MemoryMB {
		t.Fatalf("usedMb = %d, want %d", page.Memory.UsedMB, before.Memory.UsedMB+page.PrivacyFilter.MemoryMB)
	}
	h.mu.Lock()
	calls := append([]bool{}, h.privacyCalls...)
	h.mu.Unlock()
	if len(calls) != 1 || !calls[0] {
		t.Fatalf("privacy calls = %v", calls)
	}
}

func TestAModelThatHasRunIsJudgedByTheMemoryItReallyUsed(t *testing.T) {
	h := newHarness(t)
	h.sampler.SetForTests(&SamplerFigures{Others: int64ptr(2048), Models: map[string]int64{"qwen3.5-9b": 2000}})
	page := h.overview()
	found := false
	for _, entry := range page.Catalog {
		if entry.ID != "qwen3.5-9b" {
			continue
		}
		found = true
		if entry.MemoryMB != 2000 || !entry.Measured {
			t.Fatalf("measured entry = %+v", entry.Assessment)
		}
	}
	if !found {
		t.Fatalf("qwen3.5-9b missing from the catalog")
	}
}

func TestAccessLevelIsStoredAndResumesTheConnections(t *testing.T) {
	h := newHarness(t)
	page := h.overview()
	if page.Access != "admins" {
		t.Fatalf("access = %q", page.Access)
	}
	if h.svc.Access.Allowed(context.Background(), memberID) {
		t.Fatalf("member allowed while the level is admins")
	}
	status, raw := h.api(http.MethodPut, "/access", adminToken, map[string]any{"level": "everyone"})
	if status != http.StatusOK {
		t.Fatalf("access status = %d body %s", status, raw)
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode access: %v", err)
	}
	if page.Access != "everyone" {
		t.Fatalf("access = %q", page.Access)
	}
	if !h.svc.Access.Allowed(context.Background(), memberID) {
		t.Fatalf("member still refused while the level is everyone")
	}
	if h.svc.Access.Offered(context.Background(), memberID) {
		t.Fatalf("offered with nothing installed")
	}
	if status, raw := h.api(http.MethodPost, "/qwen3.5-0.8b/install", adminToken, nil); status != http.StatusAccepted {
		t.Fatalf("install status = %d body %s", status, raw)
	}
	h.waitFor("qwen3.5-0.8b", StateRunning)
	if !h.svc.Access.Offered(context.Background(), memberID) {
		t.Fatalf("not offered with a model installed")
	}
}
