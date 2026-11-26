package quota

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The free capacity estimate must not treat a minute limit stretched over a day
// as a daily cap.
func TestLimitsOfIgnoresAMinuteLimitStretchedOverADay(t *testing.T) {
	service := testService(t)
	if err := service.SetLimit("m:a", RPM, 450, SourceHeader); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}
	if err := service.SetLimit("m:a", RPD, 450*1440, SourceHeader); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}
	if err := service.SetLimit("m:a", TPM, 150_000, SourceHeader); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}
	if err := service.SetLimit("m:a", TPD, 150_000*1440, SourceHeader); err != nil {
		t.Fatalf("SetLimit: %v", err)
	}
	limits, err := service.LimitsOf(Scope{Key: "m:a", Kind: KindModel}, 3000)
	if err != nil {
		t.Fatalf("LimitsOf: %v", err)
	}
	if limits.PerDay != nil {
		t.Fatalf("perDay = %v", *limits.PerDay)
	}
	if limits.TokensPerDay != nil {
		t.Fatalf("tokensPerDay = %v", *limits.TokensPerDay)
	}
	if limits.TokensPerMinute == nil || *limits.TokensPerMinute != 150_000 {
		t.Fatalf("tokensPerMinute = %v", limits.TokensPerMinute)
	}
	if limits.PerMinute == nil || *limits.PerMinute != 450 {
		t.Fatalf("perMinute = %v", limits.PerMinute)
	}
}

func TestLimitsOfKeepsARealDailyLimit(t *testing.T) {
	service := testService(t)
	for metric, value := range map[string]float64{RPM: 30, RPD: 14_400, TPD: 500_000} {
		if err := service.SetLimit("m:b", metric, value, SourceHeader); err != nil {
			t.Fatalf("SetLimit: %v", err)
		}
	}
	limits, err := service.LimitsOf(Scope{Key: "m:b", Kind: KindModel}, 3000)
	if err != nil {
		t.Fatalf("LimitsOf: %v", err)
	}
	if limits.PerDay == nil || *limits.PerDay != 14_400 {
		t.Fatalf("perDay = %v", limits.PerDay)
	}
	if limits.TokensPerDay == nil || *limits.TokensPerDay != 500_000 {
		t.Fatalf("tokensPerDay = %v", limits.TokensPerDay)
	}
}

func TestLimitsOfCapsTheDayByWhatTheMinuteAllows(t *testing.T) {
	service := testService(t)
	for metric, value := range map[string]float64{RPM: 30, RPD: 14_400, TPM: 15_000} {
		if err := service.SetLimit("m:c", metric, value, SourceHeader); err != nil {
			t.Fatalf("SetLimit: %v", err)
		}
	}
	limits, err := service.LimitsOf(Scope{Key: "m:c", Kind: KindModel}, 3079)
	if err != nil {
		t.Fatalf("LimitsOf: %v", err)
	}
	if limits.TokensPerDay == nil || *limits.TokensPerDay != 15_000*1440 {
		t.Fatalf("tokensPerDay = %v", limits.TokensPerDay)
	}
	if limits.PerDay == nil || *limits.PerDay != 14_400 {
		t.Fatalf("perDay = %v", limits.PerDay)
	}
}

// catalog is a fixed set of entries.
type catalog map[string][]Entry

func (c catalog) ListForUser(ctx context.Context, userID, capability string, includeUnavailable bool) ([]Entry, error) {
	return c[userID+"/"+capability], nil
}

func TestFreeCapacityCountsPoolsOnceAndUnknownModelsApart(t *testing.T) {
	service := testService(t)
	service.defs = definitions{
		"pooled": {Limits: Limits{Pool: "account", Account: map[string]float64{RPM: 10}}},
		"loose":  {Limits: Limits{Defaults: map[string]float64{RPM: 30}}},
	}
	free := func(ref int64, account, model, definition string) Entry {
		return Entry{Ref: ref, AccountID: account, DefinitionID: definition, ModelID: model, IsFree: true, Enabled: true, AccountEnabled: true, Available: true}
	}
	service.SetCatalog(catalog{"u/chat": {
		free(1, "acctA", "one", "pooled"),
		free(2, "acctA", "two", "pooled"),
		free(3, "acctB", "three", "loose"),
		free(4, "acctC", "four", "unknown"),
	}})
	capacity, err := service.FreeCapacity("u", time.Now())
	if err != nil {
		t.Fatalf("FreeCapacity: %v", err)
	}
	if capacity.TokensPerMinuteOnly != 60_000 {
		t.Fatalf("tokensPerMinuteOnly = %v", capacity.TokensPerMinuteOnly)
	}
	if capacity.PerMinuteOnly != 40 {
		t.Fatalf("perMinuteOnly = %v", capacity.PerMinuteOnly)
	}
	if capacity.TokensPerDay != 0 || capacity.PerDay != 0 || capacity.PerHour != 0 {
		t.Fatalf("daily figures = %v/%v/%v", capacity.PerDay, capacity.PerHour, capacity.TokensPerDay)
	}
	if capacity.TokensPerRequest != DefaultTokensPerRequest {
		t.Fatalf("tokensPerRequest = %v", capacity.TokensPerRequest)
	}
	if capacity.ProvidersCounted != 2 {
		t.Fatalf("providersCounted = %v", capacity.ProvidersCounted)
	}
	if capacity.ModelsWithoutLimit != 1 || capacity.ProvidersWithoutLimit != 1 {
		t.Fatalf("without limit = %v models, %v providers", capacity.ModelsWithoutLimit, capacity.ProvidersWithoutLimit)
	}
}

func TestTokensPerRequestUsesThePersonsOwnAverage(t *testing.T) {
	service := testService(t)
	now := time.Now()
	if got, err := service.TokensPerRequest("u", now); err != nil || got != DefaultTokensPerRequest {
		t.Fatalf("TokensPerRequest = %v, %v", got, err)
	}
	insertRequest(t, service, "u", now.Add(-time.Hour), 1000, 500)
	insertRequest(t, service, "u", now.Add(-time.Hour), 2000, 1000)
	if got, err := service.TokensPerRequest("u", now); err != nil || got != 2250 {
		t.Fatalf("TokensPerRequest = %v, %v", got, err)
	}
}

func TestObservedFreeIsTheBestAndAverageFreeDay(t *testing.T) {
	service := testService(t)
	now := time.Now()
	if observed, err := service.ObservedFree("u", now); err != nil || observed.Days != 0 || observed.BestDay != 0 {
		t.Fatalf("nothing yet: %+v, %v", observed, err)
	}
	insertRequest(t, service, "u", now.Add(-48*time.Hour), 200, 300)
	insertRequest(t, service, "u", now.Add(-47*time.Hour), 100, 100)
	insertRequest(t, service, "u", now.Add(-24*time.Hour), 500, 1000)
	observed, err := service.ObservedFree("u", now)
	if err != nil {
		t.Fatalf("ObservedFree: %v", err)
	}
	if observed.Days != 2 || observed.BestDay != 1500 || observed.AverageDay != 1100 {
		t.Fatalf("ObservedFree = %+v", observed)
	}
}

var requestSeq int

// insertRequest writes one successful request row with the given tokens.
func insertRequest(t *testing.T, service *Service, user string, at time.Time, input, output int64) {
	t.Helper()
	_, err := service.db.W.ExecContext(t.Context(), `INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, 'x', 'user')
		ON CONFLICT(id) DO NOTHING`, user, user, user+"@example.test")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	requestSeq++
	id := fmt.Sprintf("%s-%d", user, requestSeq)
	_, err = service.db.W.ExecContext(t.Context(), `INSERT INTO requests (id, user_id, created_at, api_format, requested_model, status, input_tokens, output_tokens, is_free)
		VALUES (?, ?, ?, 'openai', 'auto', 'success', ?, ?, 1)`, id, user, at.UnixMilli(), input, output)
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}
}

// insertAccount writes one provider account row for the foreign keys.
func insertAccount(t *testing.T, service *Service, user, id, definition string) {
	t.Helper()
	_, err := service.db.W.ExecContext(t.Context(), `INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, 'x', 'user')
		ON CONFLICT(id) DO NOTHING`, user, user, user+"@example.test")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	_, err = service.db.W.ExecContext(t.Context(), `INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode)
		VALUES (?, ?, ?, ?, ?, 'free_tier') ON CONFLICT(id) DO NOTHING`, id, user, definition, id, id)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
}
