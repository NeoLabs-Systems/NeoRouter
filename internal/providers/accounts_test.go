package providers

// The provider account service, ported case for case from
// test/api/providers_custom.test.js, provider_guard.test.js,
// multi_account.test.js and the account parts of plans.test.js.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

func TestCreateStoresAConnection(t *testing.T) {
	service, factory := testService(t)
	const secret = "SI_SECRET_SECRET_STORE1"
	created := createAccount(t, service, "admin", "custom_openai", withSecret(secret))

	account := created.Account
	if created.Validation.Status != ValidationHealthy {
		t.Fatalf("validation = %v", created.Validation)
	}
	if account.DefinitionID != "custom_openai" || account.Slug != "custom-openai" || account.Label != "Custom OpenAI-compatible" {
		t.Fatalf("identity = %+v", account)
	}
	if account.ProviderKey != "custom_openai:http://127.0.0.1:9/v1" || account.ProviderName != "custom-openai" {
		t.Fatalf("provider identity = %s / %s", account.ProviderKey, account.ProviderName)
	}
	if account.KeyPreview == nil || *account.KeyPreview != stringValue(maskSecret(secret)) {
		t.Fatalf("key preview = %q", stringValue(account.KeyPreview))
	}
	if account.BillingMode != BillingPaid || account.Status != "healthy" || !account.Enabled {
		t.Fatalf("state = %+v", account)
	}
	if account.BaseURL == nil || *account.BaseURL != "http://127.0.0.1:9/v1" {
		t.Fatalf("base url = %v", account.BaseURL)
	}
	if account.CreatedBy == nil || account.CreatedBy.Username != "admin" || account.CreatedBy.DisplayName != "admin" {
		t.Fatalf("createdBy = %+v", account.CreatedBy)
	}
	if account.Models != (AccountModels{}) || len(account.SharedInPools) != 0 || len(account.Limits) != 0 || account.Concurrency != nil {
		t.Fatalf("empty facts = %+v", account)
	}
	// The key is sealed, never stored as it was typed.
	row := accountRow(t, service, account.ID)
	if row.SecretEncrypted == nil || !strings.HasPrefix(*row.SecretEncrypted, SealedPrefix) {
		t.Fatalf("stored secret = %v", row.SecretEncrypted)
	}
	opened, err := service.Secrets.Open(*row.SecretEncrypted)
	if err != nil || opened != secret {
		t.Fatalf("opened secret = %q, %v", opened, err)
	}
	built := factory.builtAccounts()
	if len(built) == 0 || built[0].UserID != "admin" || built[0].Secret != secret {
		t.Fatalf("adapter account = %+v", built)
	}
}

func TestCreateNumbersAndNamesSiblings(t *testing.T) {
	service, _ := testService(t)
	first := createAccount(t, service, "admin", "custom_openai")
	second := createAccount(t, service, "admin", "custom_openai")
	if first.Account.Slug == second.Account.Slug {
		t.Fatalf("each account gets its own model-id prefix: %s", first.Account.Slug)
	}
	if second.Account.Slug != "custom-openai-2" {
		t.Fatalf("second slug = %s", second.Account.Slug)
	}
	if !strings.HasSuffix(second.Account.Label, " 2") {
		t.Fatalf("second label = %s", second.Account.Label)
	}
	slug := "nolist"
	label := "Mine"
	named, err := service.Create(context.Background(), "admin", Input{
		DefinitionID: Optional[string]{Set: true, Value: strPtr("custom_openai")},
		Slug:         Optional[string]{Set: true, Value: &slug},
		Label:        Optional[string]{Set: true, Value: &label},
		BaseURL:      Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:9/missing")},
		Secret:       Optional[string]{Set: true, Value: strPtr("k")},
	}, "admin", nil)
	if err != nil {
		t.Fatalf("Create named: %v", err)
	}
	if named.Account.Slug != "nolist" || named.Account.Label != "Mine" {
		t.Fatalf("named account = %s / %s", named.Account.Slug, named.Account.Label)
	}
}

func TestCreateRejectsARefusedKey(t *testing.T) {
	service, factory := testService(t)
	factory.validate = func(Account) (Validation, error) { return InvalidKey("bad key"), nil }
	definition := "custom_openai"
	_, err := service.Create(context.Background(), "admin", Input{
		DefinitionID: Optional[string]{Set: true, Value: &definition},
		BaseURL:      Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:9/v1")},
		Secret:       Optional[string]{Set: true, Value: strPtr("nope")},
	}, "admin", nil)
	if err == nil || errorCode(t, err) != "KEY_REJECTED" {
		t.Fatalf("err = %v", err)
	}
	if want := "Custom OpenAI-compatible rejected this key: bad key"; err.Error() != want {
		t.Fatalf("message = %q", err.Error())
	}
	if n := countRows(t, service.DB.R, `SELECT COUNT(*) FROM provider_accounts`); n != 0 {
		t.Fatalf("a refused key stored %d accounts", n)
	}
}

func TestCreateInputChecks(t *testing.T) {
	cases := []struct {
		name     string
		in       Input
		wantCode string
		wantText string
	}{
		{
			name:     "a key is required",
			in:       Input{DefinitionID: Optional[string]{Set: true, Value: strPtr("strict")}, Options: optionsWith("accountId", "a")},
			wantCode: "KEY_REQUIRED",
			wantText: "Strict Provider requires an API key.",
		},
		{
			name:     "a base URL is required",
			in:       Input{DefinitionID: Optional[string]{Set: true, Value: strPtr("custom_openai")}, Secret: Optional[string]{Set: true, Value: strPtr("k")}},
			wantCode: "BASE_URL_REQUIRED",
			wantText: "A base URL is required for this provider.",
		},
		{
			name:     "the base URL must parse",
			in:       Input{DefinitionID: Optional[string]{Set: true, Value: strPtr("custom_openai")}, BaseURL: Optional[string]{Set: true, Value: strPtr("ht!tp://x")}, Secret: Optional[string]{Set: true, Value: strPtr("k")}},
			wantCode: "INVALID_BASE_URL",
			wantText: "The base URL is not a valid URL.",
		},
		{
			name:     "the base URL scheme must be allowed",
			in:       Input{DefinitionID: Optional[string]{Set: true, Value: strPtr("custom_openai")}, BaseURL: Optional[string]{Set: true, Value: strPtr("ftp://127.0.0.1/v1")}, Secret: Optional[string]{Set: true, Value: strPtr("k")}},
			wantCode: "INVALID_BASE_URL",
			wantText: "The base URL must use http or https.",
		},
		{
			name:     "the models path stays on the base URL",
			in:       Input{DefinitionID: Optional[string]{Set: true, Value: strPtr("custom_openai")}, BaseURL: Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:9/v1")}, Secret: Optional[string]{Set: true, Value: strPtr("k")}, Options: optionsWith("modelsPath", "http://127.0.0.1:9/steal")},
			wantCode: "INVALID_MODELS_PATH",
			wantText: "The models path must start with / and stay on the base URL.",
		},
		{
			name:     "a required field of the provider",
			in:       Input{DefinitionID: Optional[string]{Set: true, Value: strPtr("strict")}, Secret: Optional[string]{Set: true, Value: strPtr("k")}},
			wantCode: "FIELD_REQUIRED",
			wantText: "Account ID is required.",
		},
		{
			name:     "an unknown concurrency plan",
			in:       Input{DefinitionID: Optional[string]{Set: true, Value: strPtr("flat")}, Secret: Optional[string]{Set: true, Value: strPtr("k")}, Options: optionsWith("slotPlan", "gold")},
			wantCode: "UNKNOWN_SLOT_PLAN",
			wantText: "This concurrency plan does not exist.",
		},
		{
			name:     "an unknown provider",
			in:       Input{DefinitionID: Optional[string]{Set: true, Value: strPtr("nope")}, Secret: Optional[string]{Set: true, Value: strPtr("k")}},
			wantCode: "UNKNOWN_PROVIDER",
			wantText: `Unknown provider "nope".`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service, _ := testService(t)
			_, err := service.Create(context.Background(), "admin", test.in, "admin", nil)
			if err == nil {
				t.Fatalf("Create accepted %+v", test.in)
			}
			if code := errorCode(t, err); code != test.wantCode {
				t.Fatalf("code = %s, want %s", code, test.wantCode)
			}
			if err.Error() != test.wantText {
				t.Fatalf("message = %q, want %q", err.Error(), test.wantText)
			}
		})
	}
}

func TestCreateBillingModes(t *testing.T) {
	service, _ := testService(t)
	// A free tier stays the stored mode.
	groq := createAccount(t, service, "admin", "groq", withSecret("gsk"))
	if groq.Account.BillingMode != BillingFreeTier {
		t.Fatalf("groq billing = %s", groq.Account.BillingMode)
	}
	// A plan is stored as a paid connection with its prepaid marker.
	plan := createAccount(t, service, "admin", "custom_openai", withBilling("plan_paid"))
	if plan.Account.BillingMode != BillingPlanPaid {
		t.Fatalf("plan billing = %s", plan.Account.BillingMode)
	}
	row := accountRow(t, service, plan.Account.ID)
	if row.BillingMode != "paid" || row.Prepaid != PrepaidPlanPaid {
		t.Fatalf("stored billing = %s/%d", row.BillingMode, row.Prepaid)
	}
	// The free tier is only a choice for a provider that has free models, and a
	// provider that does not trust its listing falls back to its own default.
	strict := createAccount(t, service, "admin", "strict", withSecret("k"), withOption("accountId", "acct"), withBilling("free_tier"))
	if strict.Account.BillingMode != BillingPaid {
		t.Fatalf("strict billing = %s", strict.Account.BillingMode)
	}
}

func TestUpdateKeepsWhatItDoesNotSend(t *testing.T) {
	service, _ := testService(t)
	created := createAccount(t, service, "admin", "custom_openai")
	label := "Renamed"
	updated, err := service.Update(context.Background(), "admin", created.Account.ID, Input{
		Label: Optional[string]{Set: true, Value: &label},
	}, nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Label != "Renamed" {
		t.Fatalf("label = %s", updated.Label)
	}
	if updated.KeyPreview == nil || stringValue(updated.KeyPreview) != stringValue(created.Account.KeyPreview) {
		t.Fatalf("the key was lost: %q", stringValue(updated.KeyPreview))
	}
	if updated.BaseURL == nil || *updated.BaseURL != "http://127.0.0.1:9/v1" {
		t.Fatalf("base url = %v", updated.BaseURL)
	}
}

func TestUpdateRefusesMovingAMembersKey(t *testing.T) {
	service, _ := testService(t)
	created := createAccount(t, service, "admin", "custom_openai")
	member := &Actor{Role: "user", IsOwner: false}
	_, err := service.Update(context.Background(), "admin", created.Account.ID,
		Input{BaseURL: Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:8/steal")}}, member)
	if err == nil || errorCode(t, err) != "SECRET_REQUIRED" {
		t.Fatalf("err = %v", err)
	}
	if want := "Enter the API key again to change where this provider is reached."; err.Error() != want {
		t.Fatalf("message = %q", err.Error())
	}
	// The owner may move it, and so may a member who types the key again.
	if _, err := service.Update(context.Background(), "admin", created.Account.ID,
		Input{BaseURL: Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:8/moved")}}, &Actor{Role: "admin", IsOwner: true}); err != nil {
		t.Fatalf("owner update: %v", err)
	}
	if _, err := service.Update(context.Background(), "admin", created.Account.ID,
		Input{BaseURL: Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:8/typed")}, Secret: Optional[string]{Set: true, Value: strPtr("typed")}}, member); err != nil {
		t.Fatalf("member update with the key: %v", err)
	}
}

func TestUpdateReplacesOptionsAndKeepsHeaders(t *testing.T) {
	service, _ := testService(t)
	created := createAccount(t, service, "admin", "custom_openai",
		withOption("authHeader", "x-api-key"),
		func(in *Input) {
			in.Headers = Optional[map[string]string]{Set: true, Value: &map[string]string{"x-extra": "1"}}
		})
	if created.Account.Options["authHeader"] != "x-api-key" {
		t.Fatalf("options = %+v", created.Account.Options)
	}
	headers, _ := created.Account.Options["headers"].(map[string]any)
	if headers["x-extra"] != "1" {
		t.Fatalf("headers = %+v", created.Account.Options["headers"])
	}
	updated, err := service.Update(context.Background(), "admin", created.Account.ID,
		Input{Options: optionsWith("manualModels", []any{"ok-extra"})}, nil)
	if err != nil {
		t.Fatalf("Update options: %v", err)
	}
	if _, kept := updated.Options["authHeader"]; kept {
		t.Fatalf("cleared option survived: %+v", updated.Options)
	}
	keptHeaders, _ := updated.Options["headers"].(map[string]any)
	if keptHeaders["x-extra"] != "1" {
		t.Fatalf("headers were lost: %+v", updated.Options)
	}
	models, _ := updated.Options["manualModels"].([]any)
	if len(models) != 1 || models[0] != "ok-extra" {
		t.Fatalf("manual models = %+v", updated.Options)
	}
}

func TestUpdateSlugConflict(t *testing.T) {
	service, _ := testService(t)
	first := createAccount(t, service, "admin", "custom_openai")
	createAccount(t, service, "admin", "custom_openai")
	_, err := service.Update(context.Background(), "admin", first.Account.ID,
		Input{Slug: Optional[string]{Set: true, Value: strPtr("custom-openai-2")}}, nil)
	if err == nil || errorCode(t, err) != "SLUG_TAKEN" {
		t.Fatalf("err = %v", err)
	}
	if want := "Another provider already uses this name in model ids."; err.Error() != want {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestUpdateNoFreeTier(t *testing.T) {
	service, _ := testService(t)
	created := createAccount(t, service, "admin", "strict", withSecret("k"), withOption("accountId", "acct"))
	_, err := service.Update(context.Background(), "admin", created.Account.ID,
		Input{BillingMode: Optional[string]{Set: true, Value: strPtr("free_tier")}}, nil)
	if err == nil || errorCode(t, err) != "NO_FREE_TIER" {
		t.Fatalf("err = %v", err)
	}
	if want := "Strict Provider has no free models."; err.Error() != want {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestUpdateClearsFailuresAndReEnables(t *testing.T) {
	service, _ := testService(t)
	created := createAccount(t, service, "admin", "custom_openai")
	failing := &fakeFactory{validate: func(Account) (Validation, error) { return InvalidKey("nope"), nil }}
	service.Registry, _ = Parse([]byte(testDefinitions), failing.factory, func(string) string { return "" })
	for i := 0; i < AuthFailuresBeforeDisable; i++ {
		if _, err := service.ValidateAccount(context.Background(), created.Account.ID); err != nil {
			t.Fatalf("ValidateAccount: %v", err)
		}
	}
	row := accountRow(t, service, created.Account.ID)
	if row.Status != "disabled" || row.Enabled || row.AuthFailures != AuthFailuresBeforeDisable {
		t.Fatalf("row after failures = %+v", row)
	}
	healthy := &fakeFactory{}
	service.Registry, _ = Parse([]byte(testDefinitions), healthy.factory, func(string) string { return "" })
	enabled := true
	updated, err := service.Update(context.Background(), "admin", created.Account.ID, Input{
		Secret:  Optional[string]{Set: true, Value: strPtr("fresh")},
		Enabled: Optional[bool]{Set: true, Value: &enabled},
	}, nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	// A changed key resets the status: the next check decides what it is.
	if !updated.Enabled || updated.Status != "unknown" {
		t.Fatalf("account = %+v", updated)
	}
	if updated.BillingState != "unknown" {
		t.Fatalf("billing state = %s", updated.BillingState)
	}
	row = accountRow(t, service, created.Account.ID)
	if row.AuthFailures != 0 || row.Status != "unknown" {
		t.Fatalf("row after update = %+v", row)
	}
}

func TestRecordValidationVerdicts(t *testing.T) {
	service, _ := testService(t)
	created := createAccount(t, service, "admin", "custom_openai")
	ctx := context.Background()
	for i := 1; i <= AuthFailuresBeforeDisable; i++ {
		if err := service.RecordValidation(ctx, created.Account.ID, InvalidKey("refused")); err != nil {
			t.Fatalf("RecordValidation: %v", err)
		}
		row := accountRow(t, service, created.Account.ID)
		if row.AuthFailures != i {
			t.Fatalf("failures = %d after %d rejections", row.AuthFailures, i)
		}
	}
	row := accountRow(t, service, created.Account.ID)
	if row.Status != "disabled" || row.Enabled {
		t.Fatalf("a connection is disabled after %d rejections: %+v", AuthFailuresBeforeDisable, row)
	}
	// A transient failure never disables, and calling back on an unknown account
	// is not an error.
	if err := service.RecordValidation(ctx, created.Account.ID, UnknownKey("timed out")); err != nil {
		t.Fatalf("RecordValidation: %v", err)
	}
	row = accountRow(t, service, created.Account.ID)
	if row.Status != "error" || row.AuthFailures != 0 {
		t.Fatalf("row after an unknown verdict = %+v", row)
	}
	if err := service.RecordValidation(ctx, "gone", ValidKey()); err != nil {
		t.Fatalf("RecordValidation on a missing account: %v", err)
	}
}

func TestListAndRemove(t *testing.T) {
	service, _ := testService(t)
	first := createAccount(t, service, "admin", "custom_openai")
	second := createAccount(t, service, "admin", "groq", withSecret("gsk"))
	// A pool partner is managed on the pool page, not listed as a connection.
	if _, err := service.DB.W.Exec(`INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode)
		VALUES ('peer', 'admin', 'neorouter_peer', 'peer', 'Peer', 'free_tier')`); err != nil {
		t.Fatalf("insert peer: %v", err)
	}
	accounts, err := service.List(context.Background(), "admin")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(accounts) != 2 || accounts[0].ID != first.Account.ID || accounts[1].ID != second.Account.ID {
		t.Fatalf("accounts = %+v", accounts)
	}
	other, err := service.List(context.Background(), "member")
	if err != nil || len(other) != 0 {
		t.Fatalf("member accounts = %+v, %v", other, err)
	}
	if _, err := service.Get(context.Background(), "member", first.Account.ID); err == nil || errorCode(t, err) != "ACCOUNT_NOT_FOUND" {
		t.Fatalf("another person's account: %v", err)
	}
	if err := service.Remove(context.Background(), "admin", first.Account.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := service.Get(context.Background(), "admin", first.Account.ID); err == nil || errorCode(t, err) != "ACCOUNT_NOT_FOUND" {
		t.Fatalf("after Remove: %v", err)
	}
}

func TestPrivateAddressesAreForTheAdminOnly(t *testing.T) {
	service, _ := testServiceWith(t, func(service *Service) {
		service.Cfg = func() config.Config {
			cfg := testConfig()
			cfg.AllowPrivateProviderURLs = false
			return cfg
		}
	})

	definition := "custom_openai"
	body := Input{
		DefinitionID: Optional[string]{Set: true, Value: &definition},
		BaseURL:      Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:9/v1")},
		Secret:       Optional[string]{Set: true, Value: strPtr("good-key")},
	}
	// A member of the instance admin's team counts as a member, not an admin.
	if _, err := service.Probe(context.Background(), "admin", body, &Actor{Role: "user", IsOwner: false}); err == nil || errorCode(t, err) != "PRIVATE_ADDRESS" {
		t.Fatalf("member probe: %v", err)
	}
	_, err := service.Probe(context.Background(), "admin", body, &Actor{Role: "user", IsOwner: false})
	if want := "This address is on a local or private network. Only the instance admin can use those."; err == nil || err.Error() != want {
		t.Fatalf("message = %v", err)
	}
	if _, err := service.Create(context.Background(), "member", body, "member", nil); err == nil || errorCode(t, err) != "PRIVATE_ADDRESS" {
		t.Fatalf("member create: %v", err)
	}
	// The admin owns the instance, so local addresses are theirs to use.
	if _, err := service.Probe(context.Background(), "admin", body, &Actor{Role: "admin", IsOwner: true}); err != nil {
		t.Fatalf("admin probe: %v", err)
	}
}

func TestProbeListsModelsWithoutSaving(t *testing.T) {
	service, factory := testService(t)
	factory.models = []Model{{ID: "ok-listed", Name: "OK", Kind: KindChat}}
	definition := "custom_openai"
	probed, err := service.Probe(context.Background(), "admin", Input{
		DefinitionID: Optional[string]{Set: true, Value: &definition},
		BaseURL:      Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:9/v1")},
		Secret:       Optional[string]{Set: true, Value: strPtr("k")},
	}, nil)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probed.Validation.Status != ValidationHealthy {
		t.Fatalf("validation = %v", probed.Validation)
	}
	if len(probed.Models) != 1 || probed.Models[0].ID != "ok-listed" || probed.ModelsError != nil {
		t.Fatalf("models = %+v", probed)
	}
	if n := countRows(t, service.DB.R, `SELECT COUNT(*) FROM provider_accounts`); n != 0 {
		t.Fatalf("probe stored %d accounts", n)
	}
	// A listing that fails is reported, not thrown.
	factory.listErr = errFakeList
	probed, err = service.Probe(context.Background(), "admin", Input{
		DefinitionID: Optional[string]{Set: true, Value: &definition},
		BaseURL:      Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:9/v1")},
		Secret:       Optional[string]{Set: true, Value: strPtr("k")},
	}, nil)
	if err != nil {
		t.Fatalf("Probe with a failing listing: %v", err)
	}
	if probed.ModelsError == nil || *probed.ModelsError != "listing failed" || len(probed.Models) != 0 {
		t.Fatalf("probed = %+v", probed)
	}
}

func TestQuotaProbeAndStatedLimits(t *testing.T) {
	service, factory := testService(t)
	created := createAccount(t, service, "admin", "custom_openai")
	limit, remaining := 100.0, 42.5
	reset := time.Now().Add(time.Hour).UnixMilli()
	factory.quota = []QuotaWindow{{Metric: "balance", Remaining: &remaining}, {Metric: "plan_requests", Limit: &limit, Remaining: &remaining, ResetAt: &reset}}
	windows, err := service.ProbeQuota(context.Background(), created.Account.ID)
	if err != nil {
		t.Fatalf("ProbeQuota: %v", err)
	}
	if len(windows) != 2 {
		t.Fatalf("windows = %+v", windows)
	}
	quota := service.Quota.(*fakeQuota)
	if len(quota.states) != 2 || quota.states[0].Scope != "a:"+created.Account.ID || quota.states[0].Source != "probe" {
		t.Fatalf("stored states = %+v", quota.states)
	}
	if err := service.SetLimits(context.Background(), created.Account.ID, map[string]*float64{"rpd": ptrFloat(60), "concurrent": ptrFloat(4)}); err != nil {
		t.Fatalf("SetLimits: %v", err)
	}
	limits := service.Limits.Get(created.Account.ID)
	if limits["rpd"] != 60 || limits["concurrent"] != 4 {
		t.Fatalf("limits = %+v", limits)
	}
	account, err := service.Get(context.Background(), "admin", created.Account.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if account.Limits["rpd"] != 60 {
		t.Fatalf("account limits = %+v", account.Limits)
	}
}

func TestConfigureTrafficMergesLimitsAndWatches(t *testing.T) {
	service, _ := testService(t)
	var watched []string
	service.SlotWatch = func(accountID string) { watched = append(watched, accountID) }
	concurrent := 3.0
	overflow := "queue"
	wait := 1500.0
	created, err := service.Create(context.Background(), "admin", Input{
		DefinitionID:   Optional[string]{Set: true, Value: strPtr("custom_openai")},
		BaseURL:        Optional[string]{Set: true, Value: strPtr("http://127.0.0.1:9/v1")},
		Secret:         Optional[string]{Set: true, Value: strPtr("k")},
		Concurrent:     Optional[float64]{Set: true, Value: &concurrent},
		Limits:         map[string]*float64{"rpd": ptrFloat(90), "concurrent": ptrFloat(5)},
		OverflowPolicy: Optional[string]{Set: true, Value: &overflow},
		SlotWaitMS:     Optional[float64]{Set: true, Value: &wait},
	}, "admin", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The limits object wins over the concurrent shorthand, as in the JS spread.
	limits := service.Limits.Get(created.Account.ID)
	if limits["concurrent"] != 5 || limits["rpd"] != 90 {
		t.Fatalf("limits = %+v", limits)
	}
	row := accountRow(t, service, created.Account.ID)
	if row.OverflowPolicy != "queue" || row.SlotWaitMS == nil || *row.SlotWaitMS != 1500 {
		t.Fatalf("traffic row = %+v", row)
	}
	if len(watched) == 0 || watched[0] != created.Account.ID {
		t.Fatalf("slot watch = %+v", watched)
	}
	if created.Account.SlotWaitMS != 1500 {
		t.Fatalf("reported slot wait = %d", created.Account.SlotWaitMS)
	}
}

func TestCanServeEnabledAndPrepaidIDs(t *testing.T) {
	service, _ := testService(t)
	created := createAccount(t, service, "admin", "custom_openai", withBilling("plan"))
	serves, err := service.CanServe(context.Background(), created.Account.ID, CapChat)
	if err != nil || !serves {
		t.Fatalf("CanServe chat = %v, %v", serves, err)
	}
	if serves, err := service.CanServe(context.Background(), created.Account.ID, CapSpeech); err != nil || serves {
		t.Fatalf("CanServe speech = %v, %v", serves, err)
	}
	ids, err := service.AllEnabledIDs(context.Background())
	if err != nil || len(ids) != 1 || ids[0] != created.Account.ID {
		t.Fatalf("enabled ids = %+v, %v", ids, err)
	}
	prepaid, err := service.PrepaidIDs(context.Background())
	if err != nil || len(prepaid) != 1 || prepaid[0] != created.Account.ID {
		t.Fatalf("prepaid ids = %+v, %v", prepaid, err)
	}
}

func TestSealerReadsLegacyAndSealedValues(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	legacy, err := SealLegacy("SI_SECRET_SECRET_LEGACY", key)
	if err != nil {
		t.Fatalf("SealLegacy: %v", err)
	}
	sealer, err := NewSealerFromKey(key)
	if err != nil {
		t.Fatalf("NewSealerFromKey: %v", err)
	}
	opened, err := sealer.Open(legacy)
	if err != nil || opened != "SI_SECRET_SECRET_LEGACY" {
		t.Fatalf("legacy open = %q, %v", opened, err)
	}
	sealed, err := sealer.Seal("SI_SECRET_SECRET_NEW")
	if err != nil || !strings.HasPrefix(sealed, SealedPrefix) {
		t.Fatalf("seal = %q, %v", sealed, err)
	}
	if opened, err = sealer.Open(sealed); err != nil || opened != "SI_SECRET_SECRET_NEW" {
		t.Fatalf("sealed open = %q, %v", opened, err)
	}
	if _, err := sealer.Open("not-base64!"); err == nil {
		t.Fatalf("a malformed value opened")
	}
	if _, err := NewSealerFromKey([]byte("short")); err == nil {
		t.Fatalf("a wrong-sized key was accepted")
	}
}

func TestAccountJSONShape(t *testing.T) {
	service, factory := testService(t)
	concurrency := &ConcurrencyLimit{Limit: 4, Source: "manual"}
	service.Concurrency = func(accountID, definitionID, slotPlan string) *ConcurrencyLimit { return concurrency }
	factory.models = nil
	created := createAccount(t, service, "admin", "custom_openai")
	account, err := service.Get(context.Background(), "admin", created.Account.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	encoded, err := json.Marshal(account)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, name := range []string{"id", "definitionId", "providerLabel", "providerKey", "providerName", "category", "slug", "label", "baseUrl", "keyPreview", "options", "billingMode", "overflowPolicy", "slotWaitMs", "slotMaxWaitMs", "planNote", "billingState", "billingMessage", "billingCheckedAt", "billingNote", "billingVerifiable", "classification", "enabled", "status", "statusMessage", "lastValidatedAt", "lastSyncedAt", "createdAt", "createdBy", "models", "freeAvailable", "freeTierAll", "limits", "concurrency", "sharedInPools"} {
		if _, ok := decoded[name]; !ok {
			t.Fatalf("account JSON is missing %q: %s", name, encoded)
		}
	}
	if string(encoded)[:1] != "{" {
		t.Fatalf("account JSON = %s", encoded)
	}
	if decoded["planNote"] != nil || decoded["category"] != "custom" {
		t.Fatalf("account JSON = %s", encoded)
	}
	models, _ := decoded["models"].(map[string]any)
	if models["total"] != float64(0) || models["free"] != float64(0) || models["enabled"] != float64(0) {
		t.Fatalf("models JSON = %v", decoded["models"])
	}
	if decoded["classification"] != "auto" || decoded["overflowPolicy"] != "auto" {
		t.Fatalf("defaults = %s", encoded)
	}
	if decoded["concurrency"] == nil {
		t.Fatalf("concurrency = %s", encoded)
	}
}

func TestUpdateRemovesTheKeyWhenSentEmpty(t *testing.T) {
	service, _ := testService(t)
	created := createAccount(t, service, "admin", "strict", withSecret("SI_SECRET_SECRET_KEYKEE1"), withOption("accountId", "acct"))
	_, err := service.Update(context.Background(), "admin", created.Account.ID,
		Input{Secret: Optional[string]{Set: true, Value: strPtr("")}}, nil)
	if err == nil || errorCode(t, err) != "KEY_REQUIRED" {
		t.Fatalf("clearing the key of a provider that needs one: %v", err)
	}
}

func strPtr(value string) *string { return &value }

// optionsWith builds an options field with one member.
func optionsWith(name string, value any) Optional[map[string]any] {
	options := map[string]any{name: value}
	return Optional[map[string]any]{Set: true, Value: &options}
}

// errFakeList is the failure a fake listing answers with.
var errFakeList = errFakeListError{}

type errFakeListError struct{}

func (errFakeListError) Error() string { return "listing failed" }

func TestModelsPathOnBaseURL(t *testing.T) {
	// account_service.validateInput: /^\/(?!\/)/, the JS regex that keeps a
	// models path from being another host's URL. Go's RE2 has no lookahead, so
	// the check is written out.
	cases := map[string]bool{
		"/":             true,
		"/v1/models":    true,
		"//":            false,
		"//evil.test/x": false,
		"models":        false,
		"http://x/y":    false,
	}
	for path, want := range cases {
		if got := modelsPathOnBase(path); got != want {
			t.Fatalf("modelsPathOnBase(%q) = %v, want %v", path, got, want)
		}
	}
}
