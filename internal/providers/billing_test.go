package providers

// Tests of the billing modes and the checks that prove a free tier cannot
// charge.

import (
	"context"
	"net/http"
	"testing"
)

func TestStoredAndPublicBilling(t *testing.T) {
	cases := []struct {
		mode     BillingMode
		stored   BillingMode
		prepaid  int
		reported BillingMode
	}{
		{BillingFreeTier, BillingFreeTier, PrepaidNone, BillingFreeTier},
		{BillingPaid, BillingPaid, PrepaidNone, BillingPaid},
		{BillingPlan, BillingPaid, PrepaidPlan, BillingPlan},
		{BillingPlanPaid, BillingPaid, PrepaidPlanPaid, BillingPlanPaid},
		{BillingFlatRate, BillingPaid, PrepaidFlatRate, BillingFlatRate},
	}
	for _, test := range cases {
		stored, prepaid := StoredBilling(test.mode)
		if stored != test.stored || prepaid != test.prepaid {
			t.Fatalf("StoredBilling(%s) = %s/%d, want %s/%d", test.mode, stored, prepaid, test.stored, test.prepaid)
		}
		if reported := PublicBillingMode(Row{BillingMode: string(stored), Prepaid: prepaid}); reported != test.reported {
			t.Fatalf("PublicBillingMode(%s/%d) = %s, want %s", stored, prepaid, reported, test.reported)
		}
	}
}

func TestJudgeHeaders(t *testing.T) {
	verify := &Verify{Type: VerifyRateLimitHeaders, MaxFreeRequestsPerDay: 14400}
	if verdict := judgeHeaders(verify, http.Header{}); verdict != nil {
		t.Fatalf("no header should decide nothing: %+v", verdict)
	}
	free := judgeHeaders(verify, http.Header{"X-Ratelimit-Limit-Requests": {"14400"}})
	if free == nil || free.State != "free" || free.Message != "Request limit 14400/day matches the free plan." {
		t.Fatalf("free verdict = %+v", free)
	}
	paid := judgeHeaders(verify, http.Header{"X-Ratelimit-Limit-Requests": {"100000"}})
	if paid == nil || paid.State != "paid" || paid.Message != "Request limit 100000/day is above the free plan (14400)." {
		t.Fatalf("paid verdict = %+v", paid)
	}
}

// verifyAccountFor creates an account of one provider and points it at url.
func verifyAccountFor(t *testing.T, service *Service, definitionID, url string, billingMode string) string {
	t.Helper()
	fields := []func(*Input){withSecret("test-key-1"), withBilling(billingMode)}
	if definitionID == "strict" {
		fields = append(fields, withOption("accountId", "acct"))
	}
	created := createAccount(t, service, "admin", definitionID, fields...)
	if _, err := service.DB.W.Exec(`UPDATE provider_accounts SET base_url = ? WHERE id = ?`, url, created.Account.ID); err != nil {
		t.Fatalf("point the account at the test server: %v", err)
	}
	if _, err := service.DB.W.Exec(`INSERT INTO models (account_id, model_id, canonical_id, capability, available)
		VALUES (?, 'gpt-oss-20b', 'gpt-oss-20b', 'chat', 1), (?, 'gemini-pro-latest', 'gemini-pro-latest', 'chat', 1)`,
		created.Account.ID, created.Account.ID); err != nil {
		t.Fatalf("insert models: %v", err)
	}
	return created.Account.ID
}

func TestVerifyAccountByHeaders(t *testing.T) {
	service, _ := testService(t)
	header := "100000"
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("the check called %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key-1" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("x-ratelimit-limit-requests", header)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	})
	accountID := verifyAccountFor(t, service, "groq", url, "free_tier")

	verdict, err := service.VerifyAccount(context.Background(), accountID)
	if err != nil {
		t.Fatalf("VerifyAccount: %v", err)
	}
	if verdict.State != "paid" {
		t.Fatalf("verdict = %+v", verdict)
	}
	row := accountRow(t, service, accountID)
	if row.BillingState != "paid" || row.BillingMessage == nil || *row.BillingMessage != "Request limit 100000/day is above the free plan (14400)." {
		t.Fatalf("stored state = %+v", row)
	}
	if row.BillingCheckedAt == nil {
		t.Fatalf("the check time was not recorded")
	}
	catalog := service.Catalog.(*fakeCatalog)
	if len(catalog.reclassified) != 1 || catalog.reclassified[0] != accountID {
		t.Fatalf("reclassified = %+v", catalog.reclassified)
	}
	// A plan limit that matches the free plan proves it is free.
	header = "14400"
	verdict, err = service.VerifyAccount(context.Background(), accountID)
	if err != nil || verdict.State != "free" {
		t.Fatalf("verdict = %+v, %v", verdict, err)
	}
	if row := accountRow(t, service, accountID); row.BillingState != "free" {
		t.Fatalf("stored state = %+v", row)
	}
}

func TestVerifyAccountByPaidModelProbe(t *testing.T) {
	service, _ := testService(t)
	status := http.StatusTooManyRequests
	body := `{"error":{"message":"free_tier requests limit: 0"}}`
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-pro-latest:generateContent" {
			t.Errorf("the check called %s", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "test-key-1" {
			t.Errorf("api key = %q", got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	accountID := verifyAccountFor(t, service, "google", url, "free_tier")

	verdict, err := service.VerifyAccount(context.Background(), accountID)
	if err != nil {
		t.Fatalf("VerifyAccount: %v", err)
	}
	if verdict.State != "free" || verdict.Message != "gemini-pro-latest is refused with a free-tier limit: no billing account." {
		t.Fatalf("verdict = %+v", verdict)
	}
	// A model that answers although it has no free tier means billing is on.
	status, body = http.StatusOK, `{"candidates":[]}`
	verdict, err = service.VerifyAccount(context.Background(), accountID)
	if err != nil || verdict.State != "paid" {
		t.Fatalf("verdict = %+v, %v", verdict, err)
	}
	if want := "gemini-pro-latest answered although it has no free tier: billing is enabled."; verdict.Message != want {
		t.Fatalf("message = %q", verdict.Message)
	}
	// An inconclusive answer keeps a known state instead of flapping to unknown.
	status, body = http.StatusInternalServerError, `{"error":"boom"}`
	verdict, err = service.VerifyAccount(context.Background(), accountID)
	if err != nil {
		t.Fatalf("VerifyAccount: %v", err)
	}
	if verdict.State != "paid" {
		t.Fatalf("verdict = %+v", verdict)
	}
	if want := "Inconclusive answer from gemini-pro-latest (HTTP 500)."; verdict.Message != want {
		t.Fatalf("message = %q", verdict.Message)
	}
}

func TestVerifyAccountOnlyTouchesFreeTierAccounts(t *testing.T) {
	service, _ := testService(t)
	called := false
	url := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})
	accountID := verifyAccountFor(t, service, "groq", url, "paid")
	verdict, err := service.VerifyAccount(context.Background(), accountID)
	if err != nil || verdict != nil {
		t.Fatalf("a paid connection is not checked: %+v, %v", verdict, err)
	}
	if called {
		t.Fatalf("a paid connection was checked")
	}
}

func TestObserveHeadersRecordsAPaidPlan(t *testing.T) {
	service, _ := testService(t)
	accountID := verifyAccountFor(t, service, "groq", "http://127.0.0.1:9/groq/v1", "free_tier")
	entry := Observable{AccountID: accountID, DefinitionID: "groq", BillingMode: BillingFreeTier}
	service.ObserveHeaders(context.Background(), entry, http.Header{"X-Ratelimit-Limit-Requests": {"100000"}})
	if row := accountRow(t, service, accountID); row.BillingState != "paid" {
		t.Fatalf("billing state = %s", row.BillingState)
	}
	// A free-plan reading, another provider or a paid connection changes nothing.
	other := verifyAccountFor(t, service, "groq", "http://127.0.0.1:9/groq/v1", "free_tier")
	service.ObserveHeaders(context.Background(), Observable{AccountID: other, DefinitionID: "groq", BillingMode: BillingPaid}, http.Header{"X-Ratelimit-Limit-Requests": {"100000"}})
	if row := accountRow(t, service, other); row.BillingState != "unknown" {
		t.Fatalf("billing state = %s", row.BillingState)
	}
}

func TestObserveCostAndFailureFlagAModel(t *testing.T) {
	service, _ := testService(t)
	catalog := service.Catalog.(*fakeCatalog)
	entry := Observable{Ref: 7, ID: "groq/llama", AccountID: "a", DefinitionID: "groq", BillingMode: BillingFreeTier, IsFree: true}
	service.ObserveCost(context.Background(), entry, 0.01)
	if len(catalog.flagged) != 1 || catalog.flagged[0] != 7 {
		t.Fatalf("flagged = %+v", catalog.flagged)
	}
	// A free response and a paid model learn nothing.
	service.ObserveCost(context.Background(), entry, 0)
	service.ObserveCost(context.Background(), Observable{Ref: 8, IsFree: false}, 0.02)
	if len(catalog.flagged) != 1 {
		t.Fatalf("flagged = %+v", catalog.flagged)
	}
	// "free_tier … limit: 0" says this model has no free quota here.
	failure := NewError(KindRateLimit, "free_tier requests limit: 0", ErrorOptions{Learned: &LearnedLimit{Metric: "rpd", Value: 0}})
	service.ObserveFailure(context.Background(), entry, failure)
	if len(catalog.flagged) != 2 {
		t.Fatalf("flagged after a failure = %+v", catalog.flagged)
	}
	// Another reason, another kind of failure, or a paid connection: nothing.
	service.ObserveFailure(context.Background(), entry, NewError(KindRateLimit, "slow down", ErrorOptions{Learned: &LearnedLimit{Metric: "rpm", Value: 10}}))
	service.ObserveFailure(context.Background(), Observable{Ref: 9, BillingMode: BillingPaid}, failure)
	if len(catalog.flagged) != 2 {
		t.Fatalf("flagged = %+v", catalog.flagged)
	}
}
