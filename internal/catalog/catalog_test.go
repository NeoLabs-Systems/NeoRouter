package catalog

import (
	"context"
	"database/sql"
	"testing"
)

// syncAccount syncs one account and fails the test on error.
func (e *testEnv) syncAccount(t *testing.T, accountID string) *SyncResult {
	t.Helper()
	result, err := e.svc.SyncAccount(context.Background(), accountID)
	if err != nil {
		t.Fatalf("sync account: %v", err)
	}
	return result
}

func TestSyncAccountReconcilesTheCatalog(t *testing.T) {
	env := newTestEnv(t)
	env.seedAccount(t, "acc-1", "groq", "groq-main", "free_tier", func(seed *accountSeed) {
		seed.billingState = "free"
	})
	env.models.models["acc-1"] = []ListedModel{
		{ID: "llama-3.1-8b-instant", Name: "Llama 3.1 8B Instant", Kind: "chat", ContextWindow: ip(131072), MaxOutput: ip(8192), Tools: bp(true), Vision: bp(false), JSON: bp(true), Reasoning: bp(false)},
		{ID: "text-embedding-3-small", Kind: "embedding", ContextWindow: ip(8191)},
		{ID: ""}, // an entry without an id is skipped
	}
	result := env.syncAccount(t, "acc-1")
	if result == nil || result.Models != 2 {
		t.Fatalf("sync result = %+v, want 2 models", result)
	}
	entries, err := env.svc.ListForUser(context.Background(), "user-1", "chat", false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("chat entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ID != "groq-main/llama-3.1-8b-instant" || entry.Name != "Llama 3.1 8B Instant" {
		t.Errorf("entry = %+v", entry)
	}
	if entry.CanonicalID != "llama318b" {
		t.Errorf("canonicalId = %q", entry.CanonicalID)
	}
	if entry.ProviderKey != "groq" || entry.ProviderName != "Groq" {
		t.Errorf("provider identity = %q/%q, want groq/Groq", entry.ProviderKey, entry.ProviderName)
	}
	// Groq's free tier covers everything and the account billing was verified.
	if !entry.IsFree || entry.FreeReason == nil || *entry.FreeReason != string(ReasonFreeTierAccount) {
		t.Errorf("free = %v reason = %v, want free_tier_account", entry.IsFree, entry.FreeReason)
	}
	if entry.Tools == nil || !*entry.Tools || entry.JSON == nil || !*entry.JSON {
		t.Errorf("capability flags = tools %v json %v", entry.Tools, entry.JSON)
	}
	if entry.Vision == nil || *entry.Vision {
		t.Errorf("vision = %v, want false", entry.Vision)
	}
	if entry.ContextWindow == nil || *entry.ContextWindow != 131072 {
		t.Errorf("contextWindow = %v, want 131072", entry.ContextWindow)
	}
	if entry.PriceSource != nil {
		t.Errorf("priceSource = %v, want null for a listing without prices", *entry.PriceSource)
	}
	if entry.Stats.Successes != 0 || entry.Stats.Samples != 0 {
		t.Errorf("stats = %+v, want zeros", entry.Stats)
	}
	var synced sql.NullString
	if err := env.db.R.QueryRow(`SELECT last_synced_at FROM provider_accounts WHERE id = 'acc-1'`).Scan(&synced); err != nil {
		t.Fatalf("read last_synced_at: %v", err)
	}
	if !synced.Valid || synced.String == "" {
		t.Error("last_synced_at was not set")
	}
	embedding, err := env.svc.ListForUser(context.Background(), "user-1", "embedding", false)
	if err != nil {
		t.Fatalf("list embeddings: %v", err)
	}
	if len(embedding) != 1 || embedding[0].Capability != "embedding" {
		t.Errorf("embedding entries = %+v", embedding)
	}
}

func TestSyncAccountMarksDisappearedModelsUnavailable(t *testing.T) {
	env := newTestEnv(t)
	env.seedAccount(t, "acc-1", "groq", "groq-main", "paid", nil)
	env.models.models["acc-1"] = []ListedModel{{ID: "model-a", Kind: "chat"}, {ID: "model-b", Kind: "chat"}}
	env.syncAccount(t, "acc-1")
	env.models.models["acc-1"] = []ListedModel{{ID: "model-a", Kind: "chat"}}

	if result := env.syncAccount(t, "acc-1"); result.Models != 1 {
		t.Fatalf("second sync = %+v, want 1", result)
	}
	available, err := env.svc.ListForUser(context.Background(), "user-1", "chat", false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(available) != 1 || available[0].ModelID != "model-a" {
		t.Fatalf("available = %+v, want only model-a", available)
	}
	all, err := env.svc.ListForUser(context.Background(), "user-1", "chat", true)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %d, want 2 rows kept", len(all))
	}
}

func TestSyncAccountUsesModelsDevFactsForUnknownPrices(t *testing.T) {
	env := newTestEnv(t)
	env.seedAccount(t, "acc-1", "groq", "groq-main", "paid", nil)
	env.models.models["acc-1"] = []ListedModel{{ID: "llama-3.1-8b-instant", Kind: "chat"}}
	state := newMetadataState()
	state.ModelsDev = map[string]map[string]*Fact{
		"groq": {"llama-3.1-8b-instant": devFact("Llama 3.1 8B Instant", 0.05, 0.08, 131072, true, "2024-09-25")},
	}
	env.svc.Metadata.resetForTests(state)
	env.syncAccount(t, "acc-1")

	entries, err := env.svc.ListForUser(context.Background(), "user-1", "chat", false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	entry := entries[0]
	if entry.PriceIn == nil || *entry.PriceIn != 0.05 || entry.PriceOut == nil || *entry.PriceOut != 0.08 {
		t.Errorf("prices = %v/%v, want 0.05/0.08 from models.dev", entry.PriceIn, entry.PriceOut)
	}
	if entry.PriceSource == nil || *entry.PriceSource != "models.dev" {
		t.Errorf("priceSource = %v, want models.dev", entry.PriceSource)
	}
	if entry.Reasoning == nil || !*entry.Reasoning {
		t.Errorf("reasoning = %v, want true from the facts", entry.Reasoning)
	}
	// The sync recomputes the canonical metadata for what it saw.
	if entry.Intelligence == nil || *entry.Intelligence <= 0 {
		t.Errorf("intelligence = %v, want a computed score", entry.Intelligence)
	}
	if entry.IntelligenceSource == nil || *entry.IntelligenceSource != "heuristic" {
		t.Errorf("intelligenceSource = %v, want heuristic", entry.IntelligenceSource)
	}
}

func TestSyncAccountIsANoOpWithoutARuntime(t *testing.T) {
	env := newTestEnv(t)
	env.seedAccount(t, "acc-1", "groq", "groq-main", "paid", nil)
	env.models.known = map[string]bool{} // the providers side knows no runtime
	result, err := env.svc.SyncAccount(context.Background(), "acc-1")
	if err != nil || result != nil {
		t.Fatalf("sync = %+v, %v; want nil, nil", result, err)
	}
	if result, err := env.svc.SyncAccount(context.Background(), "missing"); err != nil || result != nil {
		t.Fatalf("unknown account sync = %+v, %v; want nil, nil", result, err)
	}
}

func TestReclassifyAccountFollowsTheBillingMode(t *testing.T) {
	env := newTestEnv(t)
	env.seedAccount(t, "acc-1", "groq", "groq-main", "free_tier", func(seed *accountSeed) {
		seed.billingState = "free"
	})
	env.models.models["acc-1"] = []ListedModel{{ID: "llama-3.1-8b-instant", Kind: "chat"}}
	env.syncAccount(t, "acc-1")

	if _, err := env.db.W.Exec(`UPDATE provider_accounts SET billing_mode = 'paid' WHERE id = 'acc-1'`); err != nil {
		t.Fatalf("update account: %v", err)
	}
	if err := env.svc.ReclassifyAccount(context.Background(), "acc-1"); err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	entries, err := env.svc.ListForUser(context.Background(), "user-1", "chat", false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if entries[0].IsFree {
		t.Errorf("a paid account is free: %+v", entries[0])
	}
	if entries[0].FreeReason == nil || *entries[0].FreeReason != string(ReasonPriceUnknown) {
		t.Errorf("freeReason = %v, want price_unknown", entries[0].FreeReason)
	}
}

func TestFlagModelRecordsBillingEvidenceOnceAndClearsIt(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.seedAccount(t, "acc-1", "groq", "groq-main", "free_tier", func(seed *accountSeed) {
		seed.billingState = "free"
	})
	env.models.models["acc-1"] = []ListedModel{{ID: "llama-3.1-8b-instant", Kind: "chat"}}
	env.syncAccount(t, "acc-1")
	entries, err := env.svc.ListForUser(ctx, "user-1", "chat", false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ref := entries[0].Ref
	if !entries[0].IsFree {
		t.Fatalf("the model starts free: %+v", entries[0])
	}

	changed, err := env.svc.FlagModel(ctx, ref, map[string]any{"billedAt": "2026-10-28"})
	if err != nil || !changed {
		t.Fatalf("flag = %v, %v; want true", changed, err)
	}
	entry, err := env.svc.GetModel(ctx, "user-1", ref)
	if err != nil {
		t.Fatalf("get model: %v", err)
	}
	if entry.IsFree || entry.FreeReason == nil || *entry.FreeReason != string(ReasonBilledObserved) {
		t.Errorf("after the evidence: free %v reason %v", entry.IsFree, entry.FreeReason)
	}
	if again, err := env.svc.FlagModel(ctx, ref, map[string]any{"billedAt": "2026-10-29"}); err != nil || again {
		t.Errorf("second flag = %v, %v; want false so callers log once", again, err)
	}
	if flagged, err := env.svc.FlagModel(ctx, 9999, map[string]any{"billedAt": "2026-10-28"}); err != nil || flagged {
		t.Errorf("unknown model flag = %v, %v; want false", flagged, err)
	}

	// Clearing the evidence, which a user may do, makes the model free again.
	if _, err := env.svc.UpdateModel(ctx, "user-1", ref, ModelUpdate{Overrides: map[string]any{"billedAt": nil}}); err != nil {
		t.Fatalf("clear evidence: %v", err)
	}
	entry, err = env.svc.GetModel(ctx, "user-1", ref)
	if err != nil {
		t.Fatalf("get model: %v", err)
	}
	if !entry.IsFree {
		t.Errorf("after clearing the evidence: %+v", entry)
	}
}

func TestUpdateModelAppliesOverridesAndEnabled(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.seedAccount(t, "acc-1", "groq", "groq-main", "paid", nil)
	env.models.models["acc-1"] = []ListedModel{{ID: "llama-3.1-8b-instant", Kind: "chat"}}
	env.syncAccount(t, "acc-1")
	entries, _ := env.svc.ListForUser(ctx, "user-1", "chat", false)
	ref := entries[0].Ref

	disabled := false
	entry, err := env.svc.UpdateModel(ctx, "user-1", ref, ModelUpdate{
		Enabled: &disabled,
		Overrides: map[string]any{
			"priceIn":       float64(0.5),
			"intelligence":  float64(0.75),
			"contextWindow": float64(200000),
			"priceUnit":     "second",
			"unknownKey":    "dropped",
		},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if entry.Enabled {
		t.Error("enabled was not cleared")
	}
	if entry.PriceIn == nil || *entry.PriceIn != 0.5 || entry.PriceSource == nil || *entry.PriceSource != "manual" {
		t.Errorf("price = %v source = %v, want the manual override", entry.PriceIn, entry.PriceSource)
	}
	if entry.Intelligence == nil || *entry.Intelligence != 0.75 {
		t.Errorf("intelligence = %v, want the manual override", entry.Intelligence)
	}
	if entry.IntelligenceSource == nil || *entry.IntelligenceSource != "manual" {
		t.Errorf("intelligenceSource = %v, want manual", entry.IntelligenceSource)
	}
	if entry.ContextWindow == nil || *entry.ContextWindow != 200000 {
		t.Errorf("contextWindow = %v, want 200000", entry.ContextWindow)
	}
	if entry.PriceUnit == nil || *entry.PriceUnit != "second" {
		t.Errorf("priceUnit = %v, want second", entry.PriceUnit)
	}
	if _, present := entry.Overrides["unknownKey"]; present {
		t.Error("an unknown override key was stored")
	}

	// A second update keeps the earlier overrides.
	enabled := true
	entry, err = env.svc.UpdateModel(ctx, "user-1", ref, ModelUpdate{Enabled: &enabled})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !entry.Enabled {
		t.Error("enabled was not set")
	}
	if entry.PriceIn == nil || *entry.PriceIn != 0.5 {
		t.Errorf("price = %v, want the earlier override kept", entry.PriceIn)
	}

	if _, err := env.svc.UpdateModel(ctx, "other-user", ref, ModelUpdate{}); err == nil {
		t.Error("another user must not see this model")
	} else if httpErr, ok := err.(interface{ Error() string }); !ok || httpErr.Error() != "This model does not exist." {
		t.Errorf("error = %v, want the MODEL_NOT_FOUND text", err)
	}
}

func TestBulkSetEnabledAndCapabilitySummary(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.seedAccount(t, "acc-1", "groq", "groq-main", "free_tier", func(seed *accountSeed) {
		seed.billingState = "free"
	})
	env.models.models["acc-1"] = []ListedModel{{ID: "model-a", Kind: "chat"}, {ID: "model-b", Kind: "chat"}, {ID: "embed-1", Kind: "embedding"}}
	env.syncAccount(t, "acc-1")

	summary, err := env.svc.CapabilitySummary(ctx, "user-1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(summary) != 2 {
		t.Fatalf("summary = %+v, want two capabilities", summary)
	}
	if summary[0].ID != "chat" || summary[0].Total != 2 || summary[0].Free != 2 {
		t.Errorf("chat summary = %+v, want 2 models, 2 free", summary[0])
	}

	result, err := env.svc.BulkSetEnabled(ctx, "user-1", nil, false, true)
	if err != nil {
		t.Fatalf("bulk: %v", err)
	}
	if result.Updated != 3 {
		t.Errorf("updated = %d, want 3 free models", result.Updated)
	}
	entries, _ := env.svc.ListForUser(ctx, "user-1", "chat", false)
	for _, entry := range entries {
		if entry.Enabled {
			t.Errorf("model %s is still enabled", entry.ID)
		}
	}

	accountID := "acc-1"
	enabled := true
	result, err = env.svc.BulkSetEnabled(ctx, "user-1", &accountID, enabled, false)
	if err != nil {
		t.Fatalf("bulk: %v", err)
	}
	if result.Updated != 3 {
		t.Errorf("updated = %d, want 3 models of the account", result.Updated)
	}
}

func TestTwoCustomEndpointsAreTwoProviders(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.seedAccount(t, "acc-alpha", "custom_openai", "alpha", "free_tier", func(seed *accountSeed) {
		seed.baseURL = "https://alpha.example.test/v1"
		seed.label = "Alpha host"
	})
	env.seedAccount(t, "acc-beta", "custom_openai", "beta", "free_tier", func(seed *accountSeed) {
		seed.baseURL = "https://beta.example.test/v1"
		seed.label = "Beta host"
	})
	env.models.models["acc-alpha"] = []ListedModel{{ID: "ok-shared", Kind: "chat"}}
	env.models.models["acc-beta"] = []ListedModel{{ID: "ok-shared", Kind: "chat"}}
	env.syncAccount(t, "acc-alpha")
	env.syncAccount(t, "acc-beta")

	entries, err := env.svc.ListForUser(ctx, "user-1", "chat", false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 copies of one model", len(entries))
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.ProviderName] = true
	}
	if len(names) != 2 {
		t.Errorf("provider names = %v, want two different names", names)
	}
	for _, label := range []string{"Alpha host", "Beta host"} {
		if !names[label] {
			t.Errorf("provider names = %v, want %q among them", names, label)
		}
	}
	if entries[0].ProviderKey == entries[1].ProviderKey {
		t.Errorf("provider keys are equal: %q", entries[0].ProviderKey)
	}
}

func TestGetModelUnknownRefIsNotFound(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.svc.GetModel(context.Background(), "user-1", 4242); err == nil {
		t.Fatal("an unknown model must not be returned")
	}
}
