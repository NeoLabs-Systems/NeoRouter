package catalog

import (
	"database/sql"
	"testing"
)

// Kourier's listing is bare — id, object, created, owned_by, no limits
// (api.kourier.sh/v1/models, 2026-11-20). The limits the provider documents
// (docs.kourier.sh/docs/models: 256K total context, 16K output cap per
// response) live in the definition as models.facts.
const kourierModel = "deepseek-v4.1-flash"

// storedLimits is what the models row holds for one model.
type storedLimits struct {
	ContextWindow *int64
	MaxOutput     *int64
}

// kourierLimits seeds one Kourier account with the given listing, syncs it and
// returns the stored limits per model id.
func (e *testEnv) kourierLimits(t *testing.T, listed ...ListedModel) map[string]storedLimits {
	t.Helper()
	e.seedAccount(t, "acc-1", "kourier", "kourier", "paid", nil)
	e.models.models["acc-1"] = listed
	e.syncAccount(t, "acc-1")
	rows, err := e.db.R.Query(`SELECT model_id, context_window, max_output FROM models WHERE account_id = 'acc-1'`)
	if err != nil {
		t.Fatalf("read model rows: %v", err)
	}
	defer rows.Close()
	stored := map[string]storedLimits{}
	for rows.Next() {
		var modelID string
		var window, output sql.NullInt64
		if err := rows.Scan(&modelID, &window, &output); err != nil {
			t.Fatalf("scan model row: %v", err)
		}
		stored[modelID] = storedLimits{ContextWindow: int64PtrIfValid(window), MaxOutput: int64PtrIfValid(output)}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read model rows: %v", err)
	}
	return stored
}

// wantLimits reports the difference between a stored row and the wanted limits;
// a zero want means the limit must be absent, as models.max_output is NULL.
func wantLimits(t *testing.T, stored map[string]storedLimits, modelID string, contextWindow, maxOutput int64) {
	t.Helper()
	row, ok := stored[modelID]
	if !ok {
		t.Fatalf("%s: no model row in %v", modelID, stored)
	}
	if contextWindow == 0 {
		if row.ContextWindow != nil {
			t.Errorf("%s contextWindow = %d, want none", modelID, *row.ContextWindow)
		}
	} else if row.ContextWindow == nil {
		t.Errorf("%s contextWindow = none, want %d", modelID, contextWindow)
	} else if *row.ContextWindow != contextWindow {
		t.Errorf("%s contextWindow = %d, want %d", modelID, *row.ContextWindow, contextWindow)
	}
	if maxOutput == 0 {
		if row.MaxOutput != nil {
			t.Errorf("%s maxOutput = %d, want none", modelID, *row.MaxOutput)
		}
	} else if row.MaxOutput == nil {
		t.Errorf("%s maxOutput = none, want %d", modelID, maxOutput)
	} else if *row.MaxOutput != maxOutput {
		t.Errorf("%s maxOutput = %d, want %d", modelID, *row.MaxOutput, maxOutput)
	}
}

func TestBareListingGetsTheLimitsTheProviderDocuments(t *testing.T) {
	env := newTestEnv(t)
	stored := env.kourierLimits(t, ListedModel{ID: kourierModel, Kind: "chat"})
	wantLimits(t, stored, kourierModel, 262144, 16384)
}

func TestLimitsTheListingReportsWinOverDocumentedOnes(t *testing.T) {
	env := newTestEnv(t)
	stored := env.kourierLimits(t, ListedModel{
		ID: kourierModel, Kind: "chat", ContextWindow: ip(500000), MaxOutput: ip(32000),
	})
	wantLimits(t, stored, kourierModel, 500000, 32000)
}

func TestDefinitionFactsWinOverModelsDevFactsForTheProvider(t *testing.T) {
	env := newTestEnv(t)
	state := newMetadataState()
	state.ModelsDev = map[string]map[string]*Fact{
		modelsDevProviderKey("kourier"): {
			// The canonical answer for this model, which overstates what Kourier
			// serves: the definition's documented limits must win per field.
			kourierModel: {ProviderFacts: ProviderFacts{ContextWindow: ip(1000000), MaxOutput: ip(128000)}},
			// A model the definition documents nothing about keeps the models.dev
			// limits, so the merge is per model and per field.
			"kourier-other": {ProviderFacts: ProviderFacts{ContextWindow: ip(65536)}},
		},
	}
	env.svc.Metadata.resetForTests(state)
	stored := env.kourierLimits(t,
		ListedModel{ID: kourierModel, Kind: "chat"},
		ListedModel{ID: "kourier-other", Kind: "chat"},
	)
	wantLimits(t, stored, kourierModel, 262144, 16384)
	wantLimits(t, stored, "kourier-other", 65536, 0)
}
