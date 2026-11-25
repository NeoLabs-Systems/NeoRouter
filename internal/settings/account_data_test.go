package settings

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

const (
	testPassword    = "correct-horse-battery"
	testSecret      = "good-key"
	testExportedAt  = "2026-02-01T12:00:00.000Z"
	testTwoFactorOK = "123456"
)

// accountFixture is an AccountData with the other domains' hooks recorded.
type accountFixture struct {
	*AccountData
	twoFactorOn map[string]bool
	audits      []AuditEvent
	leftPools   []string
	leaveErr    error
	groups      json.RawMessage
}

func newAccountFixture(t *testing.T) *accountFixture {
	t.Helper()
	fixture := &accountFixture{
		twoFactorOn: map[string]bool{},
		groups:      json.RawMessage(`[{"id":"g1","name":"fast"}]`),
	}
	fixture.AccountData = &AccountData{
		DB:     testDB(t),
		Log:    slog.New(slog.DiscardHandler),
		Sealer: testCrypt(t),
		Now:    func() time.Time { return time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC) },
	}
	// The password (and code) confirmation of auth two_factor_service.confirm.
	fixture.TwoFactorConfirm = func(_ context.Context, userID string, creds Credentials, requireFactor bool) error {
		if creds.CurrentPassword != testPassword {
			return httpserver.New(http.StatusUnauthorized, "INVALID_PASSWORD", "The current password is incorrect.")
		}
		if !fixture.twoFactorOn[userID] {
			if requireFactor {
				return httpserver.New(http.StatusForbidden, "TWO_FACTOR_REQUIRED", "Turn on two-factor authentication first.")
			}
			return nil
		}
		if creds.Code != testTwoFactorOK {
			return httpserver.New(http.StatusUnauthorized, "INVALID_TWO_FACTOR", "The code is not valid.")
		}
		return nil
	}
	fixture.TwoFactorEnabled = func(_ context.Context, userID string) bool { return fixture.twoFactorOn[userID] }
	fixture.RoutingGroups = func(context.Context, string) (json.RawMessage, error) { return fixture.groups, nil }
	fixture.RecordAudit = func(_ context.Context, event AuditEvent) { fixture.audits = append(fixture.audits, event) }
	fixture.LeavePool = func(_ context.Context, _ string, poolID string) error {
		fixture.leftPools = append(fixture.leftPools, poolID)
		return fixture.leaveErr
	}
	fixture.ProviderLabel = func(definitionID string) string {
		if definitionID == "custom_openai" {
			return "OpenAI compatible"
		}
		return ""
	}
	return fixture
}

// seed fills the fixture's database with one account that has something in
// every table the export reads.
func seed(t *testing.T, db *dbx.DB) {
	t.Helper()
	exec(t, db, `INSERT INTO users (id, username, email, display_name, password_hash, role, created_at, last_login_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"u-admin", "admin", "admin@example.test", nil, "hash-admin", "admin", "2026-01-01T00:00:00.000Z", nil)
	exec(t, db, `INSERT INTO users (id, username, email, display_name, password_hash, role, created_at, last_login_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"u-ada", "ada", "ada@example.test", "Ada", "hash-ada", "user", "2026-01-02T00:00:00.000Z", "2026-01-03T00:00:00.000Z")
	exec(t, db, `INSERT INTO team_members (member_user_id, owner_user_id, created_at) VALUES (?, ?, ?)`,
		"u-ada", "u-admin", "2026-01-04T00:00:00.000Z")

	box := testCrypt(t)
	sealed, err := box.Seal(testSecret)
	if err != nil {
		t.Fatalf("seal the test key: %v", err)
	}
	exec(t, db, `INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, base_url, secret_encrypted,
		options_json, billing_mode, enabled, status, created_at, overflow_policy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"acc-1", "u-ada", "custom_openai", "custom-openai", "My mock", "http://127.0.0.1:9", sealed,
		`{"headers":{"x-extra":"1"},"region":"eu"}`, "free_tier", 1, "healthy", "2026-01-05T00:00:00.000Z", "auto")
	// A pool partner connection is NeoRouter's own, not the person's.
	exec(t, db, `INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, secret_encrypted,
		options_json, billing_mode, prepaid)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"acc-peer", "u-ada", "neorouter_peer", "neorouter-peer", "Partner", "sealed-peer", "{}", "paid", 1)

	exec(t, db, `INSERT INTO models (account_id, model_id, display_name, canonical_id, capability, is_free, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "acc-1", "gpt-x", "GPT X", "gpt-x", "chat", 0, 1)
	exec(t, db, `INSERT INTO models (account_id, model_id, display_name, canonical_id, capability, is_free, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "acc-1", "a-model", nil, "a-model", "image", 1, 0)
	exec(t, db, `INSERT INTO account_limits (account_id, metric, value, updated_at) VALUES (?, ?, ?, ?)`,
		"acc-1", "concurrent", 3, 1767225600000)

	exec(t, db, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)`, "u-ada", "smoothing", "true")
	exec(t, db, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)`, "u-ada", "processing", `{"shield":true}`)
	exec(t, db, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)`, "u-ada", "broken", "not json")
	exec(t, db, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)`, "u-ada", "empty", "")

	exec(t, db, `INSERT INTO routing_profiles (id, user_id, name, weights_json, constraints_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, "p1", "u-ada", "Balanced", `{"cost":1}`, `{}`, "2026-01-06T00:00:00.000Z")

	exec(t, db, `INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode, budget_usd, budget_period,
		rpm_limit, allowed_models_json, profile, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"k1", "u-ada", "laptop", "nrs_abc", "key-hash", "both", 5.5, "monthly", 60, `["a"]`, "balanced",
		"2026-01-07T00:00:00.000Z")

	exec(t, db, `INSERT INTO pools (user_id, pool_id, name, is_host, host_member_id, host_public_key, host_url,
		self_url, self_name, share_json, paused, joined_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"u-ada", "pool-1", "Home", 0, "member-host", "host-key", "http://host.test", "http://self.test",
		"ada-laptop", `{"free":true}`, 0, "2026-01-08T00:00:00.000Z")

	exec(t, db, `INSERT INTO requests (id, user_id, created_at, api_format, requested_model, model_label, status,
		http_status, input_tokens, output_tokens, cost_usd, latency_ms, stream)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"r1", "u-ada", 1767225600000, "openai", "auto", "GPT X", "success", 200, 10, 20, 0.0012, 900, 1)

	exec(t, db, `INSERT INTO media_jobs (id, user_id, capability, upstream_id, status, request_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"m1", "u-ada", "image", "up-1", "done", `{"prompt":"x"}`, 1767225600000, 1767225600000)
}

// wantExport is the bundle exportData() must produce, in order.
const wantExport = `{` +
	`"format":1,` +
	`"kind":"neorouter-account",` +
	`"exportedAt":"` + testExportedAt + `",` +
	`"profile":{"id":"u-ada","username":"ada","email":"ada@example.test","displayName":"Ada","role":"user",` +
	`"createdAt":"2026-01-02T00:00:00.000Z","lastLoginAt":"2026-01-03T00:00:00.000Z"},` +
	`"twoFactor":{"enabled":false},` +
	`"settings":{"broken":"not json","empty":"","processing":{"shield":true},"smoothing":true},` +
	`"connections":[{"id":"acc-1","provider":"OpenAI compatible","definitionId":"custom_openai","label":"My mock",` +
	`"slug":"custom-openai","baseUrl":"http://127.0.0.1:9","options":{"headers":{"x-extra":"1"},"region":"eu"},` +
	`"billing":"free_tier","prepaid":false,"enabled":true,"status":"healthy","createdAt":"2026-01-05T00:00:00.000Z",` +
	`"models":[{"id":"a-model","name":null,"capability":"image","enabled":false,"free":true},` +
	`{"id":"gpt-x","name":"GPT X","capability":"chat","enabled":true,"free":false}]}],` +
	`"routingGroups":[{"id":"g1","name":"fast"}],` +
	`"routingProfiles":[{"id":"p1","name":"Balanced","weights":{"cost":1},"constraints":{},` +
	`"createdAt":"2026-01-06T00:00:00.000Z"}],` +
	`"routerKeys":[{"id":"k1","name":"laptop","prefix":"nrs_abc","mode":"both","budget_usd":5.5,` +
	`"budget_period":"monthly","rpm_limit":60,"concurrent_limit":null,"profile":"balanced","routing_group":null,` +
	`"expires_at":null,"revoked_at":null,"last_used_at":null,"created_at":"2026-01-07T00:00:00.000Z",` +
	`"app_name":null,"token_limit":null,"token_period":"monthly","priority":"normal","allowedModels":["a"]}],` +
	`"team":{"memberOf":{"owner_user_id":"u-admin","created_at":"2026-01-04T00:00:00.000Z"},"members":[]},` +
	`"pools":[{"id":"pool-1","name":"Home","host":false,"myName":"ada-laptop","share":{"free":true},` +
	`"paused":false,"joinedAt":"2026-01-08T00:00:00.000Z"}],` +
	`"requests":[{"id":"r1","created_at":1767225600000,"capability":"chat","task":null,"requested_model":"auto",` +
	`"model_label":"GPT X","status":"success","http_status":200,"error_code":null,"input_tokens":10,` +
	`"output_tokens":20,"units":0,"routing_group":null,"cached_input_tokens":0,"cache_creation_tokens":0,` +
	`"cost_usd":0.0012,"saved_usd":0,"is_free":0,"latency_ms":900,"stream":1}],` +
	`"mediaJobs":[{"id":"m1","capability":"image","model_label":null,"status":"done",` +
	`"created_at":1767225600000,"request":{"prompt":"x"},"result":null}]}`

func TestExportData(t *testing.T) {
	fixture := newAccountFixture(t)
	seed(t, fixture.DB)

	data, err := fixture.ExportData(context.Background(), "u-ada", Credentials{CurrentPassword: testPassword})
	if err != nil {
		t.Fatalf("export data: %v", err)
	}
	encoded, err := marshalNoEscape(data)
	if err != nil {
		t.Fatalf("encode export: %v", err)
	}
	if string(encoded) != wantExport {
		t.Fatalf("export =\n%s\nwant\n%s", encoded, wantExport)
	}
	// No secret leaves the server in the data export: the saved key, the
	// password hash, the key hash and every encrypted column stay behind.
	for _, secret := range []string{testSecret, "password_hash", "key_hash", "secret_encrypted", "token_hash",
		"sealed-peer", "hash-ada"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("the export leaks %q", secret)
		}
	}
}

func TestExportDataNeedsThePassword(t *testing.T) {
	fixture := newAccountFixture(t)
	seed(t, fixture.DB)
	ctx := context.Background()

	_, err := fixture.ExportData(ctx, "u-ada", Credentials{CurrentPassword: "wrong-password-1"})
	if httpErr := httpError(t, err); httpErr.Status != http.StatusUnauthorized || httpErr.Code != "INVALID_PASSWORD" {
		t.Fatalf("wrong password: %d %s", httpErr.Status, httpErr.Code)
	}

	_, err = fixture.ExportData(ctx, "u-nobody", Credentials{CurrentPassword: testPassword})
	httpErr := httpError(t, err)
	if httpErr.Status != http.StatusNotFound || httpErr.Code != "USER_NOT_FOUND" ||
		httpErr.Message != "This account does not exist." {
		t.Fatalf("unknown account: %d %s %q", httpErr.Status, httpErr.Code, httpErr.Message)
	}
}

func TestExportDataWithTwoFactorNeedsACode(t *testing.T) {
	fixture := newAccountFixture(t)
	seed(t, fixture.DB)
	fixture.twoFactorOn["u-ada"] = true
	ctx := context.Background()

	_, err := fixture.ExportData(ctx, "u-ada", Credentials{CurrentPassword: testPassword})
	if httpErr := httpError(t, err); httpErr.Status != http.StatusUnauthorized || httpErr.Code != "INVALID_TWO_FACTOR" {
		t.Fatalf("missing code: %d %s", httpErr.Status, httpErr.Code)
	}
	data, err := fixture.ExportData(ctx, "u-ada", Credentials{CurrentPassword: testPassword, Code: testTwoFactorOK})
	if err != nil {
		t.Fatalf("export with a code: %v", err)
	}
	if !data.TwoFactor.Enabled {
		t.Fatalf("twoFactor.enabled = false, want true")
	}
}

// wantKeys is the key file exportKeys() must produce, in order.
const wantKeys = `{` +
	`"format":1,` +
	`"kind":"neorouter-keys",` +
	`"exportedAt":"` + testExportedAt + `",` +
	`"keys":[{"provider":"OpenAI compatible","definitionId":"custom_openai","label":"My mock",` +
	`"baseUrl":"http://127.0.0.1:9","secret":"` + testSecret + `","billingMode":"free_tier",` +
	`"classification":"auto","overflowPolicy":"auto","slotWaitMs":null,"concurrent":3,` +
	`"options":{"region":"eu"},"headers":{"x-extra":"1"}}]}`

func TestExportKeys(t *testing.T) {
	fixture := newAccountFixture(t)
	seed(t, fixture.DB)
	fixture.twoFactorOn["u-ada"] = true
	ctx := context.Background()
	creds := Credentials{CurrentPassword: testPassword, Code: testTwoFactorOK}

	// The keys leave the server in plain text, so two-factor is required even
	// when the account has it off.
	fixture.twoFactorOn["u-ada"] = false
	_, err := fixture.ExportKeys(ctx, "u-ada", creds)
	if httpErr := httpError(t, err); httpErr.Status != http.StatusForbidden || httpErr.Code != "TWO_FACTOR_REQUIRED" {
		t.Fatalf("without two-factor: %d %s", httpErr.Status, httpErr.Code)
	}
	fixture.twoFactorOn["u-ada"] = true

	// Every step confirms the password and the code again.
	_, err = fixture.ExportKeys(ctx, "u-ada", Credentials{CurrentPassword: testPassword, Code: "000000"})
	if httpErr := httpError(t, err); httpErr.Status != http.StatusUnauthorized || httpErr.Code != "INVALID_TWO_FACTOR" {
		t.Fatalf("wrong code: %d %s", httpErr.Status, httpErr.Code)
	}
	_, err = fixture.ExportKeys(ctx, "u-ada", Credentials{CurrentPassword: "wrong-password-1", Code: testTwoFactorOK})
	if httpErr := httpError(t, err); httpErr.Status != http.StatusUnauthorized || httpErr.Code != "INVALID_PASSWORD" {
		t.Fatalf("wrong password: %d %s", httpErr.Status, httpErr.Code)
	}

	exported, err := fixture.ExportKeys(ctx, "u-ada", creds)
	if err != nil {
		t.Fatalf("export keys: %v", err)
	}
	encoded, err := marshalNoEscape(exported)
	if err != nil {
		t.Fatalf("encode keys: %v", err)
	}
	if string(encoded) != wantKeys {
		t.Fatalf("keys =\n%s\nwant\n%s", encoded, wantKeys)
	}
	if len(fixture.audits) != 1 {
		t.Fatalf("audit events = %v, want one", fixture.audits)
	}
	event := fixture.audits[0]
	if event.Action != "account.keys_exported" {
		t.Fatalf("audit action = %q", event.Action)
	}
	if event.ActorUserID == nil || *event.ActorUserID != "u-ada" ||
		event.SubjectUserID == nil || *event.SubjectUserID != "u-ada" {
		t.Fatalf("audit actor/subject = %v/%v", event.ActorUserID, event.SubjectUserID)
	}
	if detail, ok := event.Detail.(map[string]int); !ok || detail["count"] != 1 {
		t.Fatalf("audit detail = %v", event.Detail)
	}
}

func TestExportKeysSkipsConnectionsWithoutSecrets(t *testing.T) {
	fixture := newAccountFixture(t)
	exec(t, fixture.DB, `INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, ?, ?)`,
		"u-bob", "bob", "bob@example.test", "hash-bob", "user")
	exec(t, fixture.DB, `INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, options_json, billing_mode)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "acc-2", "u-bob", "custom_openai", "custom-openai", "No key", `{"region":"eu"}`, "paid")
	fixture.twoFactorOn["u-bob"] = true
	exported, err := fixture.ExportKeys(context.Background(), "u-bob", Credentials{CurrentPassword: testPassword, Code: testTwoFactorOK})
	if err != nil {
		t.Fatalf("export keys: %v", err)
	}
	if exported.Keys == nil || len(exported.Keys) != 0 {
		t.Fatalf("keys = %v, want an empty list", exported.Keys)
	}
	encoded, _ := marshalNoEscape(exported)
	if !strings.Contains(string(encoded), `"keys":[]`) {
		t.Fatalf("an empty export still carries the keys member: %s", encoded)
	}
}

func TestExportKeysUsesPublicBillingForPaidPlans(t *testing.T) {
	fixture := newAccountFixture(t)
	exec(t, fixture.DB, `INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, ?, ?)`,
		"u-bob", "bob", "bob@example.test", "hash-bob", "user")
	const planSecret = "bob-key"
	sealed, err := fixture.Sealer.Seal(planSecret)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	exec(t, fixture.DB, `INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, secret_encrypted,
		options_json, billing_mode, prepaid, classification, overflow_policy, slot_wait_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"acc-2", "u-bob", "custom_openai", "custom-openai", "Plan", sealed, `{}`, "paid", 3, "free", "queue", 1500)
	fixture.twoFactorOn["u-bob"] = true

	exported, err := fixture.ExportKeys(context.Background(), "u-bob",
		Credentials{CurrentPassword: testPassword, Code: testTwoFactorOK})
	if err != nil {
		t.Fatalf("export keys: %v", err)
	}
	want := `{"provider":"OpenAI compatible","definitionId":"custom_openai","label":"Plan","baseUrl":null,` +
		`"secret":"` + planSecret + `","billingMode":"flat_rate","classification":"free","overflowPolicy":"queue",` +
		`"slotWaitMs":1500,"concurrent":null}`
	encoded, _ := marshalNoEscape(exported.Keys[0])
	if string(encoded) != want {
		t.Fatalf("key =\n%s\nwant\n%s", encoded, want)
	}
}

func TestDeleteAccount(t *testing.T) {
	fixture := newAccountFixture(t)
	seed(t, fixture.DB)
	fixture.leaveErr = errors.New("partner unreachable")
	ctx := context.Background()

	if err := fixture.DeleteAccount(ctx, "u-ada", Credentials{CurrentPassword: "wrong-password-1"}); err == nil {
		t.Fatalf("the password must be confirmed")
	}
	if err := fixture.DeleteAccount(ctx, "u-ada", Credentials{CurrentPassword: testPassword}); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if len(fixture.leftPools) != 1 || fixture.leftPools[0] != "pool-1" {
		t.Fatalf("the pools were not left: %v", fixture.leftPools)
	}
	if len(fixture.audits) != 1 {
		t.Fatalf("audit events = %v, want one", fixture.audits)
	}
	event := fixture.audits[0]
	if event.Action != "account.deleted" || event.ActorUserID != nil || event.SubjectUserID != nil {
		t.Fatalf("audit event = %+v", event)
	}
	if detail, ok := event.Detail.(map[string]string); !ok || detail["username"] != "ada" {
		t.Fatalf("audit detail = %v", event.Detail)
	}
	var users, settings, keys int
	if err := fixture.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = 'u-ada'`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if err := fixture.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_settings WHERE user_id = 'u-ada'`).Scan(&settings); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if err := fixture.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM router_keys WHERE user_id = 'u-ada'`).Scan(&keys); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if users != 0 || settings != 0 || keys != 0 {
		t.Fatalf("the account and its rows must be gone: users=%d settings=%d keys=%d", users, settings, keys)
	}
}

func TestDeleteAccountLastAdmin(t *testing.T) {
	fixture := newAccountFixture(t)
	seed(t, fixture.DB)
	ctx := context.Background()

	// u-admin is the only active admin, so the server would have nobody to run
	// it if they left.
	err := fixture.DeleteAccount(ctx, "u-admin", Credentials{CurrentPassword: testPassword})
	httpErr := httpError(t, err)
	if httpErr.Status != http.StatusConflict || httpErr.Code != "LAST_ADMIN" ||
		httpErr.Message != "You are the only admin. Make someone else an admin first." {
		t.Fatalf("last admin: %d %s %q", httpErr.Status, httpErr.Code, httpErr.Message)
	}
	var users int
	if err := fixture.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 2 {
		t.Fatalf("nobody should be deleted, users=%d", users)
	}

	// A disabled admin does not count, and with a second active admin the first
	// one may go.
	exec(t, fixture.DB, `UPDATE users SET role = 'admin', disabled_at = '2026-01-09T00:00:00.000Z' WHERE id = 'u-ada'`)
	if err := fixture.DeleteAccount(ctx, "u-admin", Credentials{CurrentPassword: testPassword}); httpError(t, err).Code != "LAST_ADMIN" {
		t.Fatalf("a disabled admin must not count as one")
	}
	exec(t, fixture.DB, `UPDATE users SET disabled_at = NULL WHERE id = 'u-ada'`)
	if err := fixture.DeleteAccount(ctx, "u-admin", Credentials{CurrentPassword: testPassword}); err != nil {
		t.Fatalf("delete with two admins: %v", err)
	}
}

func TestDeleteAccountUnknownUser(t *testing.T) {
	fixture := newAccountFixture(t)
	seed(t, fixture.DB)
	err := fixture.DeleteAccount(context.Background(), "u-nobody", Credentials{CurrentPassword: testPassword})
	httpErr := httpError(t, err)
	if httpErr.Status != http.StatusNotFound || httpErr.Code != "USER_NOT_FOUND" {
		t.Fatalf("unknown account: %d %s", httpErr.Status, httpErr.Code)
	}
}

func TestDeleteAccountWithTwoFactorNeedsACode(t *testing.T) {
	fixture := newAccountFixture(t)
	seed(t, fixture.DB)
	fixture.twoFactorOn["u-ada"] = true
	ctx := context.Background()

	err := fixture.DeleteAccount(ctx, "u-ada", Credentials{CurrentPassword: testPassword})
	if httpErr := httpError(t, err); httpErr.Status != http.StatusUnauthorized || httpErr.Code != "INVALID_TWO_FACTOR" {
		t.Fatalf("missing code: %d %s", httpErr.Status, httpErr.Code)
	}
	if err := fixture.DeleteAccount(ctx, "u-ada",
		Credentials{CurrentPassword: testPassword, Code: testTwoFactorOK}); err != nil {
		t.Fatalf("delete with a code: %v", err)
	}
}

func TestExportDataNeedsItsWiring(t *testing.T) {
	fixture := newAccountFixture(t)
	exec(t, fixture.DB, `INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, ?, ?)`,
		"u-bob", "bob", "bob@example.test", "hash-bob", "user")
	ctx := context.Background()
	fixture.TwoFactorConfirm = nil
	if _, err := fixture.ExportData(ctx, "u-bob", Credentials{CurrentPassword: testPassword}); err == nil ||
		!strings.Contains(err.Error(), "TwoFactorConfirm") {
		t.Fatalf("a missing confirmation must be reported, got %v", err)
	}
}
