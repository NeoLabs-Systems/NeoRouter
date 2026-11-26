package routerkeys

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/schema"
)

func testDB(t *testing.T) *dbx.DB {
	t.Helper()
	db, err := dbx.Open(context.Background(), filepath.Join(t.TempDir(), "neorouter.sqlite3"), dbx.Options{})
	if err != nil {
		t.Fatalf("dbx.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := schema.Ensure(context.Background(), db.W); err != nil {
		t.Fatalf("schema.Ensure: %v", err)
	}
	if _, err := db.W.ExecContext(context.Background(), `INSERT INTO users (id, username, email, password_hash, role) VALUES ('u', 'u', 'u@example.test', 'x', 'admin')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return db
}

// fakeBudget answers the narrow budget surface with fixed numbers.
type fakeBudget struct {
	spent  float64
	tokens int64
	pace   *Pace
}

func (f *fakeBudget) Spent(string, string, time.Time) (float64, error) { return f.spent, nil }
func (f *fakeBudget) TokensUsed(string, string, time.Time) (int64, error) {
	return f.tokens, nil
}

func (f *fakeBudget) PeriodEnd(period string, now time.Time) *int64 {
	switch period {
	case "monthly":
		end := time.Date(now.UTC().Year(), now.UTC().Month()+1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
		return &end
	case "daily":
		end := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day()+1, 0, 0, 0, 0, time.UTC).UnixMilli()
		return &end
	default:
		return nil
	}
}

func (f *fakeBudget) Pace(string, *float64, string, time.Time) *Pace { return f.pace }

func newKeys(t *testing.T) *Keys {
	t.Helper()
	keys := New(testDB(t))
	keys.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	keys.Space = func(context.Context) (Space, bool) { return Space{OwnerID: "u", ActorID: "u"}, true }
	t.Cleanup(keys.Reset)
	return keys
}

func call(t *testing.T, keys *Keys, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	keys.Routes(mux)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, request)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

func TestCreateAndListAKey(t *testing.T) {
	keys := newKeys(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	keys.SetClock(func() time.Time { return now })
	rec := call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"laptop"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decode[struct {
		Key    map[string]any `json:"key"`
		Secret string         `json:"secret"`
	}](t, rec)
	if !strings.HasPrefix(created.Secret, "nrt_") {
		t.Fatalf("secret = %q", created.Secret)
	}
	parts := strings.SplitN(created.Secret, "_", 3)
	if len(parts) != 3 || len(parts[2]) != 43 {
		t.Fatalf("secret shape = %q", created.Secret)
	}
	if created.Key["mode"] != "both" || created.Key["allowFree"] != true || created.Key["allowPlan"] != true || created.Key["allowPaid"] != true {
		t.Fatalf("key = %v", created.Key)
	}
	if created.Key["priority"] != "normal" || created.Key["profile"] != "balanced" || created.Key["budgetPeriod"] != "total" || created.Key["tokenPeriod"] != "monthly" {
		t.Fatalf("defaults = %v", created.Key)
	}
	if created.Key["poolAccess"] != "own" || created.Key["spentUsd"] != float64(0) {
		t.Fatalf("key = %v", created.Key)
	}
	// The secret is never stored: only its digest.
	var stored string
	if err := keys.DB.R.QueryRowContext(context.Background(), `SELECT key_hash FROM router_keys`).Scan(&stored); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if stored == created.Secret || len(stored) != 64 {
		t.Fatalf("stored hash = %q", stored)
	}
	listed := decode[keysBody](t, call(t, keys, http.MethodGet, "/api/v1/keys", ""))
	if len(listed.Keys) != 1 || listed.Keys[0].Name != "laptop" || listed.Keys[0].ID != created.Key["id"] {
		t.Fatalf("list = %+v", listed.Keys)
	}
}

func TestCreatedKeyKeepsItsJSONShape(t *testing.T) {
	keys := newKeys(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	keys.SetClock(func() time.Time { return now })
	rec := call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"shape"}`)
	body := rec.Body.String()
	start := strings.Index(body, `"key":`)
	if start < 0 {
		t.Fatalf("no key in %s", body)
	}
	keyJSON := body[start+len(`"key":`) : strings.LastIndex(body, `,"secret"`)]
	var createdAt string
	if err := keys.DB.R.QueryRowContext(context.Background(), `SELECT created_at FROM router_keys`).Scan(&createdAt); err != nil {
		t.Fatalf("read created_at: %v", err)
	}
	want := fmt.Sprintf(`{"id":"`+"%s"+`","name":"shape","prefix":"nrt_`+"%s"+`","mode":"both","allowFree":true,"allowPlan":true,"allowPaid":true,`+
		`"budgetUsd":null,"budgetPeriod":"total","spentUsd":0,"remainingUsd":null,"periodResetsAt":null,"pacing":null,`+
		`"rpmLimit":null,"concurrentLimit":null,"priority":"normal","maxPricePerMillion":null,"tokenLimit":null,`+
		`"tokenPeriod":"monthly","tokensUsed":null,"tokenResetsAt":null,"allowedModels":[],"poolAccess":"own",`+
		`"routingGroup":null,"profile":"balanced","expiresAt":null,"expired":false,"revoked":false,"lastUsedAt":null,`+
		`"createdAt":"%s","createdBy":null,"app":null}`, idOf(t, keys), prefixOf(t, keys), createdAt)
	if keyJSON != want {
		t.Fatalf("key =\n%s\nwant\n%s", keyJSON, want)
	}
}

func idOf(t *testing.T, keys *Keys) string {
	t.Helper()
	var id string
	if err := keys.DB.R.QueryRowContext(context.Background(), `SELECT id FROM router_keys`).Scan(&id); err != nil {
		t.Fatalf("read id: %v", err)
	}
	return id
}

func prefixOf(t *testing.T, keys *Keys) string {
	t.Helper()
	var prefix string
	if err := keys.DB.R.QueryRowContext(context.Background(), `SELECT prefix FROM router_keys`).Scan(&prefix); err != nil {
		t.Fatalf("read prefix: %v", err)
	}
	return prefix
}

func TestExpiryIsValidatedEnforcedAndRenewable(t *testing.T) {
	keys := newKeys(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	keys.SetClock(func() time.Time { return now })
	past := now.Add(-time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
	future := now.Add(24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")

	if rec := call(t, keys, http.MethodPost, "/api/v1/keys", fmt.Sprintf(`{"name":"past","expiresAt":%q}`, past)); rec.Code != http.StatusBadRequest {
		t.Fatalf("a past expiry was accepted: %d", rec.Code)
	}
	created := decode[struct {
		Key    PublicKey `json:"key"`
		Secret string    `json:"secret"`
	}](t, call(t, keys, http.MethodPost, "/api/v1/keys", fmt.Sprintf(`{"name":"temporary","expiresAt":%q}`, future)))
	if created.Key.ExpiresAt == nil || *created.Key.ExpiresAt != future || created.Key.Expired {
		t.Fatalf("key = %+v", created.Key)
	}
	row, err := keys.Authenticate(created.Secret)
	if err != nil || row == nil {
		t.Fatalf("Authenticate = %v, %v", row, err)
	}
	// The provider moved the expiry into the past behind the API's back.
	if _, err := keys.DB.W.ExecContext(context.Background(), `UPDATE router_keys SET expires_at = ? WHERE id = ?`, past, created.Key.ID); err != nil {
		t.Fatalf("expire the key: %v", err)
	}
	if row, err := keys.Authenticate(created.Secret); err != nil || row != nil {
		t.Fatalf("an expired key authenticated: %v, %v", row, err)
	}
	edited := decode[keyBody](t, call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, `{"name":"renamed"}`))
	if edited.Key.Name != "renamed" || edited.Key.ExpiresAt == nil || *edited.Key.ExpiresAt != past || !edited.Key.Expired {
		t.Fatalf("renamed key = %+v", edited.Key)
	}
	if row, _ := keys.Authenticate(created.Secret); row != nil {
		t.Fatal("renaming reactivated the key")
	}
	// A different past expiry is refused; the one already stored may be kept.
	another := now.Add(-2 * time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
	if rec := call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, fmt.Sprintf(`{"expiresAt":%q}`, another)); rec.Code != http.StatusBadRequest {
		t.Fatalf("a past expiry was accepted on a patch: %d", rec.Code)
	}
	extended := decode[keyBody](t, call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, fmt.Sprintf(`{"expiresAt":%q}`, future)))
	if extended.Key.Expired {
		t.Fatalf("extended key = %+v", extended.Key)
	}
	if row, _ := keys.Authenticate(created.Secret); row == nil {
		t.Fatal("the same secret works again after the extension")
	}
	cleared := decode[keyBody](t, call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, `{"expiresAt":null}`))
	if cleared.Key.ExpiresAt != nil {
		t.Fatalf("cleared expiry = %v", *cleared.Key.ExpiresAt)
	}
	if rec := call(t, keys, http.MethodPost, "/api/v1/keys/"+created.Key.ID+"/revoke", ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d", rec.Code)
	}
	renewed := decode[keyBody](t, call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, fmt.Sprintf(`{"expiresAt":%q}`, past[:10]+"T13:04:05.000Z")))
	if !renewed.Key.Revoked {
		t.Fatalf("changing the expiry unrevoked the key: %+v", renewed.Key)
	}
	if row, _ := keys.Authenticate(created.Secret); row != nil {
		t.Fatal("a revoked key authenticated")
	}
}

func TestPriorityDefaultsAndChangesOnItsOwn(t *testing.T) {
	keys := newKeys(t)
	created := decode[struct {
		Key PublicKey `json:"key"`
	}](t, call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"plain","rpmLimit":7}`))
	if created.Key.Priority != "normal" {
		t.Fatalf("priority = %q", created.Key.Priority)
	}
	changed := decode[keyBody](t, call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, `{"priority":"high"}`))
	if changed.Key.Priority != "high" {
		t.Fatalf("priority = %q", changed.Key.Priority)
	}
	if changed.Key.RPMLimit == nil || *changed.Key.RPMLimit != 7 {
		t.Fatalf("other settings moved: %+v", changed.Key)
	}
	if rec := call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, `{"priority":"urgent"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown priority was accepted: %d", rec.Code)
	}
}

func TestAPatchClearsWhatItSendsAsNullAndKeepsTheRest(t *testing.T) {
	keys := newKeys(t)
	created := decode[struct {
		Key PublicKey `json:"key"`
	}](t, call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"patchy","budgetUsd":25,"budgetPeriod":"monthly","rpmLimit":9,"tokenLimit":500}`))
	if created.Key.BudgetUSD == nil || *created.Key.BudgetUSD != 25 {
		t.Fatalf("created = %+v", created.Key)
	}
	cleared := decode[keyBody](t, call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, `{"budgetUsd":null,"rpmLimit":null}`))
	if cleared.Key.BudgetUSD != nil || cleared.Key.RPMLimit != nil {
		t.Fatalf("null did not clear: %+v", cleared.Key)
	}
	if cleared.Key.TokenLimit == nil || *cleared.Key.TokenLimit != 500 || cleared.Key.BudgetPeriod != "monthly" {
		t.Fatalf("absent fields moved: %+v", cleared.Key)
	}
	kept := decode[keyBody](t, call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, `{"name":"renamed"}`))
	if kept.Key.TokenLimit == nil || *kept.Key.TokenLimit != 500 {
		t.Fatalf("an absent field was cleared: %+v", kept.Key)
	}
	if rec := call(t, keys, http.MethodPatch, "/api/v1/keys/"+created.Key.ID, `{"mode":null}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a null mode was accepted: %d", rec.Code)
	}
}

func TestValidation(t *testing.T) {
	keys := newKeys(t)
	cases := []string{
		`{}`,
		`{"name":""}`,
		`{"name":"x","mode":"free"}`,
		`{"name":"x","budgetUsd":-1}`,
		`{"name":"x","rpmLimit":0}`,
		`{"name":"x","concurrentLimit":0}`,
		`{"name":"x","tokenLimit":0}`,
		`{"name":"x","budgetPeriod":"weekly"}`,
		`{"name":"x","poolAccess":"shared"}`,
		`{"name":"x","expiresAt":"tomorrow"}`,
		`{"name":"x","allowedModels":[""]}`,
	}
	for _, body := range cases {
		if rec := call(t, keys, http.MethodPost, "/api/v1/keys", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s → %d", body, rec.Code)
		}
	}
	if rec := call(t, keys, http.MethodPatch, "/api/v1/keys/missing", `{"name":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown key → %d", rec.Code)
	}
	if rec := call(t, keys, http.MethodDelete, "/api/v1/keys/missing", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown key → %d", rec.Code)
	}
}

func TestKindsSwitches(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  Kinds
		fail  bool
	}{
		{name: "default is both", input: `{"name":"k"}`, want: Kinds{Mode: ModeBoth, AllowFree: true, AllowPlan: true}},
		{name: "free only", input: `{"name":"k","mode":"free_only"}`, want: Kinds{Mode: ModeFreeOnly, AllowFree: true, AllowPlan: true}},
		{name: "paid only", input: `{"name":"k","allowPaid":true,"allowFree":false,"allowPlan":false}`, want: Kinds{Mode: ModePaid}},
		{name: "paid with free", input: `{"name":"k","allowPaid":true,"allowFree":true,"allowPlan":false}`, want: Kinds{Mode: ModeBoth, AllowFree: true}},
		{name: "nothing allowed", input: `{"name":"k","allowPaid":false,"allowFree":false,"allowPlan":false}`, fail: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var in Input
			if err := json.Unmarshal([]byte(test.input), &in); err != nil {
				t.Fatalf("decode: %v", err)
			}
			kinds, err := ResolveKinds(in, nil)
			if test.fail {
				if err == nil {
					t.Fatalf("kinds = %+v", kinds)
				}
				var httpErr *httpserver.HttpError
				if !asHTTPError(err, &httpErr) || httpErr.Code != "NO_MODELS_ALLOWED" {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("kinds: %v", err)
			}
			if kinds != test.want {
				t.Fatalf("kinds = %+v, want %+v", kinds, test.want)
			}
		})
	}
}

func asHTTPError(err error, target **httpserver.HttpError) bool {
	httpErr, ok := err.(*httpserver.HttpError)
	if ok {
		*target = httpErr
	}
	return ok
}

func TestConcurrencyLimitRefusesAndReleases(t *testing.T) {
	keys := newKeys(t)
	limit := int64(2)
	row := &Row{ID: "k1", ConcurrentLimit: &limit}
	first, err := keys.Concurrency.Acquire(row)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	second, err := keys.Concurrency.Acquire(row)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := keys.Concurrency.Acquire(row); err == nil {
		t.Fatal("the third request was allowed")
	} else if httpErr, ok := err.(*httpserver.HttpError); !ok || httpErr.Code != "KEY_CONCURRENCY_LIMITED" || httpErr.Status != http.StatusTooManyRequests ||
		httpErr.Message != "This router key has reached its limit for simultaneous requests." {
		t.Fatalf("error = %v", err)
	}
	first()
	first() // releasing twice is harmless
	if got := keys.Concurrency.InFlight("k1"); got != 1 {
		t.Fatalf("in flight = %d", got)
	}
	second()
	if got := keys.Concurrency.InFlight("k1"); got != 0 {
		t.Fatalf("in flight = %d", got)
	}
	// An uncapped key is counted but never refused.
	uncapped := &Row{ID: "k2"}
	releases := make([]func(), 0, 5)
	for i := 0; i < 5; i++ {
		release, err := keys.Concurrency.Acquire(uncapped)
		if err != nil {
			t.Fatalf("uncapped Acquire: %v", err)
		}
		releases = append(releases, release)
	}
	if got := keys.Concurrency.InFlight("k2"); got != 5 {
		t.Fatalf("uncapped in flight = %d", got)
	}
	for _, release := range releases {
		release()
	}
	if got := keys.Concurrency.InFlight("k2"); got != 0 {
		t.Fatalf("uncapped in flight = %d", got)
	}
}

func TestRPMIsASlidingMinute(t *testing.T) {
	keys := newKeys(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	limit := int64(2)
	row := &Row{ID: "k1", RPMLimit: &limit}
	if result := keys.ConsumeRPM(row, now); !result.Allowed {
		t.Fatalf("first request refused: %+v", result)
	}
	if result := keys.ConsumeRPM(row, now.Add(10*time.Second)); !result.Allowed {
		t.Fatalf("second request refused: %+v", result)
	}
	refused := keys.ConsumeRPM(row, now.Add(20*time.Second))
	if refused.Allowed {
		t.Fatal("the third request was allowed")
	}
	if refused.RetryAfterSeconds != 40 {
		t.Fatalf("retryAfterSeconds = %d", refused.RetryAfterSeconds)
	}
	if state := keys.RPMStateOf(row, now.Add(20*time.Second)); state == nil || state.Remaining != 0 || state.ResetSeconds != 40 {
		t.Fatalf("state = %+v", state)
	}
	// A minute after the oldest request it leaves the window.
	if result := keys.ConsumeRPM(row, now.Add(61*time.Second)); !result.Allowed {
		t.Fatalf("a request after the window was refused: %+v", result)
	}
	if state := keys.RPMStateOf(&Row{ID: "k2"}, now); state != nil {
		t.Fatalf("an uncapped key has no window: %+v", state)
	}
}

func TestPacingAndBudgetFigures(t *testing.T) {
	keys := newKeys(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	keys.SetClock(func() time.Time { return now })
	budget := &fakeBudget{spent: 60, tokens: 120, pace: &Pace{State: "tight", Forecast: float64Ptr(862.5)}}
	keys.Budget = budget
	created := decode[struct {
		Key PublicKey `json:"key"`
	}](t, call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"spent","mode":"paid","budgetUsd":100,"budgetPeriod":"monthly","tokenLimit":1000}`))
	if created.Key.SpentUSD != 60 || created.Key.RemainingUSD == nil || *created.Key.RemainingUSD != 40 {
		t.Fatalf("spend figures = %+v", created.Key)
	}
	if created.Key.Pacing == nil || created.Key.Pacing.State != "tight" || created.Key.Pacing.Forecast == nil || *created.Key.Pacing.Forecast != 862.5 {
		t.Fatalf("pacing = %+v", created.Key.Pacing)
	}
	if created.Key.PeriodResetsAt == nil || *created.Key.PeriodResetsAt != "2026-04-01T00:00:00.000Z" {
		t.Fatalf("periodResetsAt = %v", created.Key.PeriodResetsAt)
	}
	if created.Key.TokensUsed == nil || *created.Key.TokensUsed != 120 {
		t.Fatalf("tokensUsed = %v", created.Key.TokensUsed)
	}
	if created.Key.TokenResetsAt == nil || *created.Key.TokenResetsAt != "2026-04-01T00:00:00.000Z" {
		t.Fatalf("tokenResetsAt = %v", created.Key.TokenResetsAt)
	}
	// A free-only key reports no pacing, whatever its budget.
	budget.pace = nil
	freeOnly := decode[struct {
		Key PublicKey `json:"key"`
	}](t, call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"free","mode":"free_only","budgetUsd":10}`))
	if freeOnly.Key.Pacing != nil {
		t.Fatalf("free-only pacing = %+v", freeOnly.Key.Pacing)
	}
	// A spent budget never shows a negative remainder.
	budget.spent = 150
	over := decode[struct {
		Key PublicKey `json:"key"`
	}](t, call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"over","budgetUsd":100}`))
	if over.Key.RemainingUSD == nil || *over.Key.RemainingUSD != 0 || over.Key.SpentUSD != 150 {
		t.Fatalf("spent-budget figures = %+v", over.Key)
	}
}

func TestAppKeyAndCreator(t *testing.T) {
	keys := newKeys(t)
	keys.PublicUser = func(userID string) any { return map[string]string{"id": userID, "username": "u", "displayName": "u"} }
	key, _, err := keys.Create("u", Input{}, "u", &App{Name: "Notes", Origin: stringPtr("https://notes.test"), Scopes: []string{"chat"}})
	if err == nil {
		t.Fatalf("a key without a name was created: %+v", key)
	}
	var in Input
	if err := json.Unmarshal([]byte(`{"name":"app","routingGroup":"fast","expiresAt":null}`), &in); err != nil {
		t.Fatalf("decode: %v", err)
	}
	keys.RequireGroup = func(userID, group string) error {
		if userID != "u" || group != "fast" {
			t.Fatalf("RequireGroup(%q, %q)", userID, group)
		}
		return nil
	}
	key, _, err = keys.Create("u", in, "u", &App{Name: "Notes", Origin: stringPtr("https://notes.test"), Scopes: []string{"chat"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if key.App == nil || key.App.Name != "Notes" || key.App.Origin == nil || *key.App.Origin != "https://notes.test" ||
		len(key.App.Scopes) != 1 || key.App.Scopes[0] != "chat" {
		t.Fatalf("app = %+v", key.App)
	}
	if key.RoutingGroup == nil || *key.RoutingGroup != "fast" {
		t.Fatalf("routing group = %v", key.RoutingGroup)
	}
	if key.CreatedBy == nil {
		t.Fatal("createdBy is empty")
	}
	// Removing the key takes it off the list.
	if err := keys.Remove("u", key.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if listed, _ := keys.List("u"); len(listed) != 0 {
		t.Fatalf("list after remove = %+v", listed)
	}
}

func stringPtr(v string) *string    { return &v }
func float64Ptr(v float64) *float64 { return &v }

func TestAnAppWithoutAnOriginReportsNull(t *testing.T) {
	keys := newKeys(t)
	created := decode[struct {
		Key PublicKey `json:"key"`
	}](t, call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"device"}`))
	var in Input
	if err := json.Unmarshal([]byte(`{"name":"device"}`), &in); err != nil {
		t.Fatalf("decode: %v", err)
	}
	key, _, err := keys.Create("u", in, "u", &App{Name: "Device App", Scopes: []string{"chat"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if key.App == nil || key.App.Name != "Device App" || key.App.Origin != nil {
		t.Fatalf("app = %+v", key.App)
	}
	var stored sql.NullString
	if err := keys.DB.R.QueryRowContext(context.Background(), `SELECT app_origin FROM router_keys WHERE id = ?`, key.ID).Scan(&stored); err != nil {
		t.Fatalf("read app_origin: %v", err)
	}
	if stored.Valid {
		t.Fatalf("an absent origin was stored as %q", stored.String)
	}
	encoded := mustJSONKey(t, key)
	if !strings.Contains(encoded, `"app":{"name":"Device App","origin":null,"scopes":["chat"]}`) {
		t.Fatalf("key json = %s", encoded)
	}
	// An app that does have an origin keeps it.
	withOrigin, _, err := keys.Create("u", in, "u", &App{Name: "Web App", Origin: stringPtr("https://app.test"), Scopes: []string{}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if withOrigin.App == nil || withOrigin.App.Origin == nil || *withOrigin.App.Origin != "https://app.test" {
		t.Fatalf("app = %+v", withOrigin.App)
	}
	_ = created
}

func mustJSONKey(t *testing.T, key *PublicKey) string {
	t.Helper()
	encoded, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}

func TestAuthenticateRejectsForeignTokens(t *testing.T) {
	keys := newKeys(t)
	for _, token := range []string{"", "nrs_abc", "nrt_abc", "nrt_" + strings.Repeat("a", 8) + "_" + strings.Repeat("b", 43)} {
		row, err := keys.Authenticate(token)
		if err != nil {
			t.Fatalf("Authenticate(%q): %v", token, err)
		}
		if row != nil {
			t.Fatalf("Authenticate(%q) = %+v", token, row)
		}
	}
	// A disabled owner's key does not authenticate.
	created := decode[struct {
		Key    PublicKey `json:"key"`
		Secret string    `json:"secret"`
	}](t, call(t, keys, http.MethodPost, "/api/v1/keys", `{"name":"owner"}`))
	if _, err := keys.DB.W.ExecContext(context.Background(), `UPDATE users SET disabled_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = 'u'`); err != nil {
		t.Fatalf("disable the owner: %v", err)
	}
	if row, _ := keys.Authenticate(created.Secret); row != nil {
		t.Fatalf("a disabled owner's key authenticated: %+v", row)
	}
}
