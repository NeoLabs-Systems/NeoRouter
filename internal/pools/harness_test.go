package pools

// The test harness: a real SQLite database, a real HTTP server with the pool
// and federation routes on it, and small fakes for the domains this one only
// talks to through interfaces. Three people take part from one server, each
// with their own space, signing key and connection, and every pool call travels
// over real HTTP to the server's own address, exactly as it would between three
// machines.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
	"github.com/NeoLabs-Systems/neorouter/internal/teams"
)

// testResponse is one answered test request.
type testResponse struct {
	Code   int
	Body   *bytes.Buffer
	Header http.Header
}

// harness is one running test NeoRouter.
type harness struct {
	t   *testing.T
	db  *dbx.DB
	svc *Service

	mux    *http.ServeMux
	server *httptest.Server

	accounts *fakeAccounts
	keys     *fakeRouterKeys
	catalog  *fakeCatalog
	quota    *fakeQuota
	settings *fakeSettings
	peers    *providers.Client

	people map[string]*person
	mu     sync.Mutex
	// keySecret maps a router key secret to the person it belongs to, for the
	// small models/chat handlers relays answer through.
	keySecret map[string]*person
}

// person is one account taking part in the pools.
type person struct {
	ID       string
	Name     string
	MemberID string
	Account  string
	PoolKey  string
}

// newHarness starts one test NeoRouter.
func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	db, err := dbx.Open(ctx, filepath.Join(t.TempDir(), "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := schema.Ensure(ctx, db.W); err != nil {
		t.Fatalf("prepare schema: %v", err)
	}
	h := &harness{t: t, db: db, people: map[string]*person{}, keySecret: map[string]*person{}}
	h.svc = New(db)
	h.svc.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	sealer := testSealer{}
	h.svc.Seal = sealer.seal
	h.svc.Unseal = sealer.unseal
	h.accounts = &fakeAccounts{h: h}
	h.keys = &fakeRouterKeys{h: h}
	h.catalog = &fakeCatalog{h: h}
	h.quota = &fakeQuota{}
	h.settings = &fakeSettings{values: map[string]json.RawMessage{}}
	h.peers = providers.NewClient(providers.Options{Timeout: 5 * time.Second})
	h.svc.Peers = h.peers
	h.svc.Accounts = h.accounts
	h.svc.RouterKeys = h.keys
	h.svc.Catalog = h.catalog.sync
	h.svc.Quota = h.quota.evaluate
	h.svc.ForgetQuota = func(accountID string, refs []int64) error { return nil }
	h.svc.MarkFixed = func(accountID string, now time.Time) {}
	h.svc.Concurrency = func(model Model, now time.Time) (int, bool) { return 0, false }
	h.svc.Inflight = func(model Model) int { return 0 }
	h.svc.GetSetting = h.settings.get
	h.svc.SetSetting = h.settings.set

	h.mux = http.NewServeMux()
	h.svc.Routes(h.mux)
	h.svc.FederationRoutes(h.mux)
	h.svc.JoinRoute(h.mux)
	h.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	h.mux.HandleFunc("GET /v1/models", h.modelsHandler)
	h.mux.HandleFunc("POST /v1/chat/completions", h.chatHandler)
	h.mux.HandleFunc("GET /api/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})

	handler := h.withSpace(h.mux)
	h.svc.Loopback = handler
	h.server = httptest.NewServer(handler)
	h.svc.Cfg = func() config.Config { return config.Config{PublicURL: h.server.URL} }
	return h
}

// close stops the server and the database.
func (h *harness) close() {
	h.svc.Stop()
	h.server.CloseClientConnections()
	h.server.Close()
	if err := h.db.Close(); err != nil {
		h.t.Errorf("close database: %v", err)
	}
}

// base is the server's address.
func (h *harness) base() string { return h.server.URL }

// withSpace resolves the request's space from a test header, standing in for the
// session and space middleware of the wired app.
func (h *harness) withSpace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("X-Test-User")
		if userID == "" {
			// A relayed request carries a router key instead; the actor is the
			// person whose data the call works on.
			if secret := bearerToken(r); secret != "" {
				if owner, ok := h.personForKey(secret); ok {
					userID = owner.ID
				}
			}
		}
		if userID == "" {
			next.ServeHTTP(w, r)
			return
		}
		space := &teams.Space{OwnerID: userID, ActorID: userID, IsOwner: true}
		next.ServeHTTP(w, r.WithContext(teams.ContextWithSpace(r.Context(), space)))
	})
}

// bearerToken is the key or session a request carries.
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(header[len("Bearer "):])
	}
	return ""
}

// personForKey finds whose router key a secret is.
func (h *harness) personForKey(secret string) (*person, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	owner, ok := h.keySecret[secret]
	return owner, ok
}

// modelsHandler answers as the gateway's model list does: 401 for an unknown key.
func (h *harness) modelsHandler(w http.ResponseWriter, r *http.Request) {
	secret := bearerToken(r)
	owner, ok := h.personForKey(secret)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"This key is not valid.","type":"invalid_request_error","code":"invalid_api_key"}}`))
		return
	}
	models := []map[string]any{}
	h.mu.Lock()
	for name, id := range h.catalog.modelsOf(owner.Account) {
		models = append(models, map[string]any{"id": name, "owned_by": id})
	}
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": models})
}

// chatHandler answers one chat completion, as a partner's gateway would.
func (h *harness) chatHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.personForKey(bearerToken(r)); !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"This key is not valid.","type":"invalid_request_error","code":"invalid_api_key"}}`))
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	h.mu.Lock()
	h.catalog.calls = append(h.catalog.calls, body)
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "hello"}}},
	})
}

// register makes one person: a users row, a pool identity and a connection.
func (h *harness) register(name string) *person {
	h.t.Helper()
	ctx := context.Background()
	id := "user-" + name
	if _, err := h.db.W.ExecContext(ctx, `INSERT INTO users (id, username, email, password_hash, role)
		VALUES (?, ?, ?, 'x', 'user')`, id, name, name+"@example.test"); err != nil {
		h.t.Fatalf("create user: %v", err)
	}
	entry := &person{ID: id, Name: name}
	record, err := h.svc.ensureIdentity(ctx, id)
	if err != nil {
		h.t.Fatalf("create pool identity: %v", err)
	}
	entry.MemberID = record.MemberID
	entry.Account = h.addAccount(entry, "custom_openai", "mock")
	secret := h.addKey(entry, "pool", "pool")
	entry.PoolKey = secret
	h.mu.Lock()
	h.people[name] = entry
	h.mu.Unlock()
	return entry
}

// addAccount adds one provider connection of a person's own.
func (h *harness) addAccount(owner *person, definitionID, label string) string {
	h.t.Helper()
	id := fmt.Sprintf("acct-%s-%s", owner.Name, label)
	if _, err := h.db.W.ExecContext(context.Background(), `INSERT INTO provider_accounts
		(id, user_id, definition_id, slug, label, base_url, secret_encrypted, options_json, billing_mode)
		VALUES (?, ?, ?, ?, ?, 'http://127.0.0.1:1/v1', 'x', '{}', 'free_tier')`,
		id, owner.ID, definitionID, label, label); err != nil {
		h.t.Fatalf("create connection: %v", err)
	}
	return id
}

// addKey adds one router key and remembers the secret.
func (h *harness) addKey(owner *person, name, poolAccess string) string {
	h.t.Helper()
	id := "key-" + owner.Name + "-" + name
	secret := "nrt_" + fmt.Sprintf("%08x", rand.Int63n(1<<32)) + "_" + strings.Repeat("a", 24)
	sum := sha256.Sum256([]byte(secret))
	prefix := secret[:12]
	if _, err := h.db.W.ExecContext(context.Background(), `INSERT INTO router_keys
		(id, user_id, name, prefix, key_hash, mode, pool_access) VALUES (?, ?, ?, ?, ?, 'both', ?)`,
		id, owner.ID, name, prefix+"-"+name, hex.EncodeToString(sum[:]), poolAccess); err != nil {
		h.t.Fatalf("create key: %v", err)
	}
	h.mu.Lock()
	h.keySecret[secret] = owner
	h.mu.Unlock()
	return secret
}

// api calls one pool endpoint as one person.
func (h *harness) api(who *person, method, path string, body any) *testResponse {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, h.base()+"/api/v1"+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-Test-User", who.ID)
	response, err := h.server.Client().Do(request)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		h.t.Fatalf("read answer: %v", err)
	}
	return &testResponse{Code: response.StatusCode, Body: bytes.NewBuffer(raw), Header: response.Header}
}

// decoded is one answer as a decoded JSON object.
func decoded(t *testing.T, response *testResponse) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode answer %q: %v", response.Body.String(), err)
	}
	return body
}

// summaryOf is one person's pool page state.
func (h *harness) summaryOf(who *person) map[string]any {
	h.t.Helper()
	response := h.api(who, http.MethodGet, "/pools", nil)
	if response.Code != http.StatusOK {
		h.t.Fatalf("GET /pools: %d %s", response.Code, response.Body.String())
	}
	return decoded(h.t, response)
}

// firstPool is one person's first pool as the page shows it.
func (h *harness) firstPool(who *person) map[string]any {
	h.t.Helper()
	pools, _ := h.summaryOf(who)["pools"].([]any)
	if len(pools) == 0 {
		return nil
	}
	first, _ := pools[0].(map[string]any)
	return first
}

// invite creates one invite link for a pool.
func (h *harness) invite(who *person, poolID string) string {
	h.t.Helper()
	response := h.api(who, http.MethodPost, "/pools/"+poolID+"/invites", nil)
	if response.Code != http.StatusCreated {
		h.t.Fatalf("create invite: %d %s", response.Code, response.Body.String())
	}
	link, _ := decoded(h.t, response)["link"].(string)
	return link
}

// join joins a pool with an invite link.
func (h *harness) join(who *person, link, selfName string, shared []string) map[string]any {
	h.t.Helper()
	body := map[string]any{"link": link, "selfName": selfName, "sharedAccountIds": shared}
	response := h.api(who, http.MethodPost, "/pools/join", body)
	if response.Code != http.StatusOK {
		h.t.Fatalf("%s joins: %d %s", who.Name, response.Code, response.Body.String())
	}
	return decoded(h.t, response)
}

// poolViewIn finds one pool in a page state.
func poolViewIn(t *testing.T, answer map[string]any, poolID string) map[string]any {
	t.Helper()
	pools, _ := answer["pools"].([]any)
	for _, item := range pools {
		view, _ := item.(map[string]any)
		if view["id"] == poolID {
			return view
		}
	}
	return nil
}

// partnersOf is one pool's partner states.
func partnersOf(t *testing.T, view map[string]any) []map[string]any {
	t.Helper()
	list, _ := view["partners"].([]any)
	partners := make([]map[string]any, 0, len(list))
	for _, item := range list {
		partner, _ := item.(map[string]any)
		partners = append(partners, partner)
	}
	return partners
}

// partnerNamed finds one partner by name.
func partnerNamed(t *testing.T, view map[string]any, name string) map[string]any {
	t.Helper()
	for _, partner := range partnersOf(t, view) {
		if partner["name"] == name {
			return partner
		}
	}
	t.Fatalf("no partner named %q in %v", name, view["partners"])
	return nil
}

// waitFor polls until check reports true, the way the relay tests wait for a
// connection to come back.
func waitFor(t *testing.T, what string, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// testSealer is a stand-in for the app's secretbox: reversible and recognisable.
type testSealer struct{}

// seal marks a value as sealed.
func (testSealer) seal(plain string) (string, error) { return "enc:v1:" + plain, nil }

// unseal reverses seal, and passes through a value that was never sealed.
func (testSealer) unseal(sealed string) (string, error) {
	return strings.TrimPrefix(sealed, "enc:v1:"), nil
}

// fakeAccounts is the providers-accounts domain, reduced to what pool links use.
type fakeAccounts struct {
	h         *harness
	mu        sync.Mutex
	validated []string
	removed   []string
	added     []PeerAccount
}

// AddPoolPeer adds one partner connection, with a unique slug.
func (f *fakeAccounts) AddPoolPeer(ctx context.Context, userID string, peer PeerAccount) (string, error) {
	f.mu.Lock()
	f.added = append(f.added, peer)
	f.mu.Unlock()
	base := "pool-" + strings.ToLower(strings.ReplaceAll(peer.Label, " ", "-"))
	slug := base
	for index := 2; ; index++ {
		var one int
		err := f.h.db.R.QueryRowContext(ctx, `SELECT 1 FROM provider_accounts WHERE user_id = ? AND slug = ?`,
			userID, slug).Scan(&one)
		if err != nil {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, index)
	}
	id := "peer-" + userID + "-" + slug
	if _, err := f.h.db.W.ExecContext(ctx, `INSERT INTO provider_accounts
		(id, user_id, definition_id, slug, label, base_url, secret_encrypted, options_json, billing_mode)
		VALUES (?, ?, ?, ?, ?, ?, ?, '{}', 'free_tier')`,
		id, userID, PeerDefinition, slug, peer.Label, peer.BaseURL, peer.Secret); err != nil {
		return "", fmt.Errorf("add partner connection: %w", err)
	}
	return id, nil
}

// Remove deletes one connection.
func (f *fakeAccounts) Remove(ctx context.Context, userID, accountID string) error {
	f.mu.Lock()
	f.removed = append(f.removed, accountID)
	f.mu.Unlock()
	_, err := f.h.db.W.ExecContext(ctx, `DELETE FROM provider_accounts WHERE id = ? AND user_id = ?`, accountID, userID)
	return err
}

// Validate re-checks one connection.
func (f *fakeAccounts) Validate(ctx context.Context, accountID string) error {
	f.mu.Lock()
	f.validated = append(f.validated, accountID)
	f.mu.Unlock()
	return nil
}

// PublicOnly reports that every account may reach public addresses only.
func (f *fakeAccounts) PublicOnly(ctx context.Context, userID string) bool { return false }

// fakeRouterKeys is the router-keys domain, reduced to issuing a partner's key.
type fakeRouterKeys struct {
	h   *harness
	mu  sync.Mutex
	seq int
}

// CreatePoolKey issues one key for a partner link.
func (f *fakeRouterKeys) CreatePoolKey(ctx context.Context, userID, name string) (string, string, error) {
	f.mu.Lock()
	f.seq++
	seq := f.seq
	f.mu.Unlock()
	id := fmt.Sprintf("poolkey-%d", seq)
	secret := fmt.Sprintf("nrt_%08x_%s", seq, strings.Repeat("b", 24))
	sum := sha256.Sum256([]byte(secret))
	if _, err := f.h.db.W.ExecContext(ctx, `INSERT INTO router_keys
		(id, user_id, name, prefix, key_hash, mode, priority, pool_access) VALUES (?, ?, ?, ?, ?, 'free_only', 'low', 'own')`,
		id, userID, name, fmt.Sprintf("nrt_%08x", seq), hex.EncodeToString(sum[:])); err != nil {
		return "", "", fmt.Errorf("issue partner key: %w", err)
	}
	f.h.mu.Lock()
	if owner, ok := f.h.peopleByID(userID); ok {
		f.h.keySecret[secret] = owner
	}
	f.h.mu.Unlock()
	return id, secret, nil
}

// peopleByID finds one registered person.
func (h *harness) peopleByID(userID string) (*person, bool) {
	for _, entry := range h.people {
		if entry.ID == userID {
			return entry, true
		}
	}
	return nil, false
}

// fakeCatalog is the catalog domain, reduced to syncing a connection's models.
type fakeCatalog struct {
	h      *harness
	mu     sync.Mutex
	synced []string
	models map[string]map[string]string // account → model → account
	calls  []map[string]any
}

// sync refreshes one connection's models.
func (f *fakeCatalog) sync(ctx context.Context, accountID string) error {
	f.mu.Lock()
	f.synced = append(f.synced, accountID)
	f.mu.Unlock()
	return nil
}

// modelsOf is the models one connection offers.
func (f *fakeCatalog) modelsOf(accountID string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.models == nil {
		return map[string]string{}
	}
	return f.models[accountID]
}

// addModel records that one connection offers one chat model, in the catalog's
// own table too, so the pool queries see it.
func (h *harness) addModel(accountID, modelID string, isFree bool) int64 {
	h.t.Helper()
	free := 0
	if isFree {
		free = 1
	}
	result, err := h.db.W.ExecContext(context.Background(), `INSERT INTO models
		(account_id, model_id, display_name, canonical_id, capability, is_free, available)
		VALUES (?, ?, ?, ?, 'chat', ?, 1)`, accountID, modelID, modelID, modelID, free)
	if err != nil {
		h.t.Fatalf("add model: %v", err)
	}
	ref, err := result.LastInsertId()
	if err != nil {
		h.t.Fatalf("add model: %v", err)
	}
	h.catalog.mu.Lock()
	if h.catalog.models == nil {
		h.catalog.models = map[string]map[string]string{}
	}
	if h.catalog.models[accountID] == nil {
		h.catalog.models[accountID] = map[string]string{}
	}
	h.catalog.models[accountID][modelID] = accountID
	h.catalog.mu.Unlock()
	return ref
}

// fakeQuota is the quota domain, reduced to one model's picture.
type fakeQuota struct {
	headroom float64
	rpd      *float64
	calls    int
}

// evaluate answers one model's quota picture.
func (f *fakeQuota) evaluate(model Model, tokens float64, now time.Time) (QuotaState, error) {
	f.calls++
	state := QuotaState{Headroom: 1}
	if f.headroom != 0 {
		state.Headroom = f.headroom
	}
	if f.rpd != nil {
		remaining := *f.rpd
		state.Details = append(state.Details, QuotaDetail{Metric: "rpd", Remaining: &remaining})
	}
	return state, nil
}

// fakeSettings is the settings domain, in memory.
type fakeSettings struct {
	mu     sync.Mutex
	values map[string]json.RawMessage
}

// get reads one setting.
func (f *fakeSettings) get(ctx context.Context, userID, key string) (json.RawMessage, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, ok := f.values[userID+"|"+key]
	return value, ok, nil
}

// set writes one setting.
func (f *fakeSettings) set(ctx context.Context, userID, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.values[userID+"|"+key] = encoded
	f.mu.Unlock()
	return nil
}
