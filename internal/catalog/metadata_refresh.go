package catalog

import (
	"context"
	"fmt"
	"time"
)

// aaSetting is the global setting holding the Artificial Analysis API key.

func (m *Metadata) artificialAnalysisDue(now time.Time) bool {
	due := false
	m.withView(func(v *metadataView) {
		fetchedAt, ok := v.st.FetchedAt["artificialAnalysis"]
		due = true
		if ok {
			if parsed, err := time.Parse(time.RFC3339Nano, fetchedAt); err == nil && now.Sub(parsed) < aaRefresh {
				due = false
			}
		}
		if !due {
			return
		}
		for _, record := range v.st.ArtificialAnalysis {
			if record != nil && record.ID != "" {
				return
			}
		}
		// Data from before the current API (records without an id) is
		// replaced as soon as possible.
		due = true
	})
	return due
}

// RefreshSources refreshes every metadata source and re-stores the affected
// canonical records. It returns one status per source: "ok" or the error text.
func (m *Metadata) RefreshSources(ctx context.Context) (map[string]string, error) {
	key, err := m.artificialAnalysisKey()
	if err != nil {
		return nil, err
	}
	results := map[string]string{}
	m.mu.Lock()
	m.ensureLoaded()
	state := m.state
	type task struct {
		name string
		run  func() error
	}
	tasks := []task{
		{"modelsDev", func() error {
			records, err := m.modelsDevSource().fetch(ctx)
			if err != nil {
				return err
			}
			state.ModelsDev = records
			return nil
		}},
		{"openrouter", func() error {
			records, err := m.openRouterSource().fetch(ctx)
			if err != nil {
				return err
			}
			state.OpenRouter = records
			return nil
		}},
		{"openrouterBenchmarks", func() error {
			snapshot, err := m.benchmarkSource().fetch(ctx, state.OpenRouterBenchmarks, time.Now())
			if err != nil {
				return err
			}
			state.OpenRouterBenchmarks = snapshot
			return nil
		}},
	}
	if key != "" && m.artificialAnalysisDueLocked(state, time.Now()) {
		tasks = append(tasks,
			task{"artificialAnalysis", func() error {
				records, err := m.analysisSource().fetch(ctx, key, state.ArtificialAnalysis)
				if err != nil {
					return err
				}
				state.ArtificialAnalysis = records
				return nil
			}},
			task{"artificialAnalysisMedia", func() error {
				media, err := m.analysisSource().fetchMedia(ctx, key)
				if err != nil {
					return err
				}
				state.ArtificialAnalysisMedia = media
				return nil
			}},
		)
	}
	for _, item := range tasks {
		if err := item.run(); err != nil {
			results[item.name] = err.Error()
			m.log.Warn("Metadata source failed", "source", item.name, "error", err)
			continue
		}
		state.FetchedAt[item.name] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		results[item.name] = "ok"
	}
	m.rebuildIndexes()
	if err := m.saveState(); err != nil {
		results["cache"] = err.Error()
		m.log.Warn("Metadata cache not saved", "error", err)
	}
	m.mu.Unlock()
	if err := m.RecomputeAll(ctx); err != nil {
		return results, err
	}
	// MTEB is slow and sometimes flaky, so it runs after the other sources are
	// saved and applied: embedding models are rated once it is done.
	if err := m.refreshMTEB(ctx); err != nil {
		results["mteb"] = err.Error()
		m.log.Warn("Metadata source failed", "source", "mteb", "error", err)
	} else {
		results["mteb"] = "ok"
	}
	if m.observeNews != nil {
		m.observeNews()
	}
	return results, nil
}

// refreshMTEB refreshes the embedding benchmark scores.
func (m *Metadata) refreshMTEB(ctx context.Context) error {
	rows, err := m.db.R.QueryContext(ctx, `SELECT DISTINCT model_id FROM models WHERE capability = 'embedding' AND available = 1`)
	if err != nil {
		return fmt.Errorf("catalog: mteb: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var modelID string
		if err := rows.Scan(&modelID); err != nil {
			rows.Close()
			return fmt.Errorf("catalog: mteb: %w", err)
		}
		ids = append(ids, modelID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("catalog: mteb: %w", err)
	}
	previous := map[string]MTEBScore{}
	m.mu.Lock()
	previous = m.state.MTEB
	m.mu.Unlock()
	scores := m.mtebSource().fetchScores(ctx, ids, previous, time.Now())
	m.mu.Lock()
	m.state.MTEB = scores
	m.state.FetchedAt["mteb"] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	m.rebuildIndexes()
	err = m.saveState()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return m.RecomputeAll(ctx)
}

// artificialAnalysisDueLocked is artificialAnalysisDue for a held state.
func (m *Metadata) artificialAnalysisDueLocked(state *metadataState, now time.Time) bool {
	if fetchedAt, ok := state.FetchedAt["artificialAnalysis"]; ok {
		if parsed, err := time.Parse(time.RFC3339Nano, fetchedAt); err == nil && now.Sub(parsed) < aaRefresh {
			return false
		}
	}
	for _, record := range state.ArtificialAnalysis {
		if record != nil && record.ID != "" {
			return false
		}
	}
	return true
}

// modelsDevSource is the models.dev source with its defaults applied. A
// default is applied to a copy so concurrent refreshes never race.
func (m *Metadata) modelsDevSource() modelsDevSource {
	source := m.modelsDev
	if source.baseURL == "" {
		source.baseURL = modelsDevURL
	}
	if source.client == nil {
		source.client = m.client
	}
	return source
}

func (m *Metadata) openRouterSource() openRouterPublicSource {
	source := m.openRouter
	if source.baseURL == "" {
		source.baseURL = openRouterModelsURL
	}
	if source.client == nil {
		source.client = m.client
	}
	return source
}

func (m *Metadata) benchmarkSource() benchmarkSource {
	source := m.benchmarks
	if source.baseURL == "" {
		source.baseURL = openRouterBenchmarksURL
	}
	if source.client == nil {
		source.client = m.client
	}
	return source
}

// analysisSource keeps its memoized Free tier between calls, so it is the one
// source returned by pointer.
func (m *Metadata) analysisSource() *artificialAnalysisSource {
	if m.analysis.baseURL == "" || m.analysis.client == nil {
		m.analysis.baseURL = firstNonEmpty(m.analysis.baseURL, artificialAnalysisURL)
		if m.analysis.client == nil {
			m.analysis.client = m.client
		}
	}
	return &m.analysis
}

func (m *Metadata) mtebSource() *mtebSource {
	source := m.mteb
	if source.baseURL == "" {
		source.baseURL = mtebAPI
	}
	if source.client == nil {
		source.client = m.client
	}
	if source.sleep == nil {
		source.sleep = time.Sleep
	}
	return &source
}

// resetForTests replaces the cached state and rebuilds the indexes.
func (m *Metadata) resetForTests(state *metadataState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state == nil {
		state = newMetadataState()
	}
	m.state = state
	m.rebuildIndexes()
}
