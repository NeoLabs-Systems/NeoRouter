package catalog

import (
	"context"
	"encoding/json"
	"fmt"
)

// aaSetting is the global setting holding the Artificial Analysis API key.

// ProviderFacts returns the models.dev facts for exactly this provider's
// model. They are used for prices, never borrowed from another host because
// budgets must reflect what this host charges.
func (m *Metadata) ProviderFacts(definitionID, modelID string) ProviderFacts {
	var facts ProviderFacts
	m.withView(func(v *metadataView) {
		models, known := v.st.ModelsDev[modelsDevProviderKey(definitionID)]
		if !known {
			return
		}
		record, ok := models[modelID]
		if !ok {
			record, ok = models[trimModelsPrefix(modelID)]
		}
		if ok && record != nil {
			facts = record.ProviderFacts
		}
	})
	return facts
}

func trimModelsPrefix(modelID string) string {
	const prefix = "models/"
	if len(modelID) > len(prefix) && modelID[:len(prefix)] == prefix {
		return modelID[len(prefix):]
	}
	return modelID
}

// RecomputeCanonical recomputes and stores one canonical record from the
// catalog rows that carry it.
func (m *Metadata) RecomputeCanonical(ctx context.Context, canonical string) error {
	var record Record
	m.withView(func(v *metadataView) {
		rows, err := m.db.R.QueryContext(ctx, `SELECT model_id, capability FROM models WHERE canonical_id = ? LIMIT 5`, canonical)
		if err != nil {
			return
		}
		defer rows.Close()
		ids := []string{}
		capability := "chat"
		for rows.Next() {
			var modelID string
			var kind string
			if err := rows.Scan(&modelID, &kind); err != nil {
				return
			}
			if len(ids) == 0 {
				capability = kind
			}
			ids = append(ids, modelID)
		}
		record = m.computeCanonical(v, canonical, ids, capability)
	})
	return m.upsertCanonical(ctx, record)
}

// RecomputeAll recomputes every canonical record the catalog knows.
func (m *Metadata) RecomputeAll(ctx context.Context) error {
	rows, err := m.db.R.QueryContext(ctx, `SELECT DISTINCT canonical_id FROM models`)
	if err != nil {
		return fmt.Errorf("catalog: recompute metadata: %w", err)
	}
	canonicals := []string{}
	for rows.Next() {
		var canonical string
		if err := rows.Scan(&canonical); err != nil {
			rows.Close()
			return fmt.Errorf("catalog: recompute metadata: %w", err)
		}
		canonicals = append(canonicals, canonical)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("catalog: recompute metadata: %w", err)
	}
	for _, canonical := range canonicals {
		if err := m.RecomputeCanonical(ctx, canonical); err != nil {
			return err
		}
	}
	return nil
}

const upsertCanonicalSQL = `INSERT INTO model_metadata
	(canonical_id, intelligence, intelligence_source, intelligence_confidence, speed_tps, context_window, max_output,
	 price_in, price_out, supports_tools, supports_vision, supports_reasoning, release_date, params_b, skills_json, sources_json, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	ON CONFLICT(canonical_id) DO UPDATE SET intelligence = excluded.intelligence, intelligence_source = excluded.intelligence_source,
	  intelligence_confidence = excluded.intelligence_confidence, speed_tps = excluded.speed_tps, context_window = excluded.context_window,
	  max_output = excluded.max_output, price_in = excluded.price_in, price_out = excluded.price_out,
	  supports_tools = excluded.supports_tools, supports_vision = excluded.supports_vision, supports_reasoning = excluded.supports_reasoning,
	  release_date = excluded.release_date, params_b = excluded.params_b, skills_json = excluded.skills_json,
	  sources_json = excluded.sources_json, updated_at = excluded.updated_at`

// upsertCanonical stores one canonical record.
func (m *Metadata) upsertCanonical(ctx context.Context, record Record) error {
	sources := record.SourcesJSON
	if sources == "" {
		sources = "[]"
	}
	if _, err := m.db.W.ExecContext(ctx, upsertCanonicalSQL,
		record.CanonicalID, record.Intelligence, record.IntelligenceSource, record.IntelligenceConfidence,
		nilIfNil(record.SpeedTps), nilIfNil(record.ContextWindow), nilIfNil(record.MaxOutput),
		nilIfNil(record.PriceIn), nilIfNil(record.PriceOut), nilIfNil(record.SupportsTools),
		nilIfNil(record.SupportsVision), nilIfNil(record.SupportsReasoning), nilIfNil(record.ReleaseDate),
		nilIfNil(record.ParamsB), nilIfNil(record.SkillsJSON), sources,
	); err != nil {
		return fmt.Errorf("catalog: store model metadata: %w", err)
	}
	return nil
}

// Status reports the metadata sources' state.
func (m *Metadata) Status() (MetadataStatus, error) {
	status := MetadataStatus{}
	m.withView(func(v *metadataView) {
		status.FetchedAt = v.st.FetchedAt
	})
	key, err := m.artificialAnalysisKey()
	if err != nil {
		return status, err
	}
	status.ArtificialAnalysisConfigured = key != ""
	if m.cfg != nil {
		status.SourcesEnabled = m.cfg().MetadataSourcesEnabled
	}
	return status, nil
}

// DiscoverySnapshot is the read-only projection the discovery service reads.
func (m *Metadata) DiscoverySnapshot() DiscoverySnapshot {
	var snapshot DiscoverySnapshot
	m.withView(func(v *metadataView) {
		fetchedAt := make(map[string]string, len(v.st.FetchedAt))
		for key, value := range v.st.FetchedAt {
			fetchedAt[key] = value
		}
		snapshot = DiscoverySnapshot{
			ArtificialAnalysis:      v.st.ArtificialAnalysis,
			OpenRouter:              v.st.OpenRouter,
			OpenRouterBenchmarks:    v.st.OpenRouterBenchmarks,
			ArtificialAnalysisMedia: v.st.ArtificialAnalysisMedia,
			MTEB:                    v.st.MTEB,
			FetchedAt:               fetchedAt,
		}
	})
	return snapshot
}

// BenchmarkIdentity is the benchmark id of a model: the benchmark record's
// canonical id when one matched, the model's own canonical id otherwise.
func (m *Metadata) BenchmarkIdentity(id, capability string) string {
	if capability == "" {
		capability = "chat"
	}
	canonical := canonicalID(id)
	if capability != "chat" {
		return canonical
	}
	var match *Fact
	m.withView(func(v *metadataView) { match = v.lookupBenchmark(canonical, []string{id}) })
	if match != nil && match.Slug != "" {
		return canonicalID(match.Slug)
	}
	return canonical
}

// VariantBase reports what a variant id keeps of its base model's score.
func (m *Metadata) VariantBase(id string) *variantMatch { return variantBase(id) }

// OpenRouterAgreement reports how well each OpenRouter benchmark agrees with
// the fuller index.
func (m *Metadata) OpenRouterAgreement() map[string]float64 {
	agreement := map[string]float64{}
	m.withView(func(v *metadataView) {
		if v.idx.openrouter != nil {
			agreement = v.idx.openrouter.agreement
		}
	})
	return agreement
}

// SameSize reports whether a size-less benchmark match may speak for a model.
func (m *Metadata) SameSize(modelText string, record *Fact) bool { return sameSize(modelText, record) }

// ReconcileWithSkills keeps an index and a model's skills telling one story.
func (m *Metadata) ReconcileWithSkills(index float64, skills map[string]float64) float64 {
	return reconcileWithSkills(index, skills)
}

// artificialAnalysisKey reads the admin's Artificial Analysis key.
func (m *Metadata) artificialAnalysisKey() (string, error) {
	if m.settings == nil {
		return "", nil
	}
	value, ok, err := m.settings.Global(aaSetting)
	if err != nil {
		return "", fmt.Errorf("catalog: read artificial analysis key: %w", err)
	}
	if !ok || len(value) == 0 {
		return "", nil
	}
	var sealed string
	if err := json.Unmarshal(value, &sealed); err != nil {
		return "", nil
	}
	if sealed == "" {
		return "", nil
	}
	plain, err := m.settings.Open(sealed)
	if err != nil {
		return "", fmt.Errorf("catalog: open artificial analysis key: %w", err)
	}
	return plain, nil
}

// SetArtificialAnalysisKey stores the admin's Artificial Analysis key, sealed.
func (m *Metadata) SetArtificialAnalysisKey(key string) error {
	if m.settings == nil {
		return nil
	}
	if key == "" {
		return m.settings.SetGlobal(aaSetting, nil)
	}
	sealed, err := m.settings.Seal(key)
	if err != nil {
		return fmt.Errorf("catalog: seal artificial analysis key: %w", err)
	}
	return m.settings.SetGlobal(aaSetting, sealed)
}

// artificialAnalysisDue reports whether the index is due for a refresh.
