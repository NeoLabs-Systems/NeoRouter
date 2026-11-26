package catalog

import (
	"encoding/json"
	"fmt"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// aaSetting is the global setting holding the Artificial Analysis API key.

const aaSetting = "artificial_analysis_key"

// aaRefresh is the minimum interval between Artificial Analysis refreshes: a
// Free key gets 100 requests a day and one refresh takes about seven.
const aaRefresh = 6 * time.Hour

// metadataState is the cached metadata of every source, as metadata_cache.json
// holds it.
type metadataState struct {
	ModelsDev               map[string]map[string]*Fact `json:"modelsDev"`
	OpenRouter              []*Fact                     `json:"openrouter"`
	OpenRouterBenchmarks    *BenchmarkSnapshot          `json:"openrouterBenchmarks"`
	ArtificialAnalysis      []*Fact                     `json:"artificialAnalysis"`
	ArtificialAnalysisMedia map[string][]MediaRecord    `json:"artificialAnalysisMedia"`
	MTEB                    map[string]MTEBScore        `json:"mteb"`
	FetchedAt               map[string]string           `json:"fetchedAt"`
}

// newMetadataState is the empty state.
func newMetadataState() *metadataState {
	return &metadataState{
		ModelsDev:               map[string]map[string]*Fact{},
		OpenRouter:              []*Fact{},
		ArtificialAnalysis:      []*Fact{},
		ArtificialAnalysisMedia: map[string][]MediaRecord{},
		MTEB:                    map[string]MTEBScore{},
		FetchedAt:               map[string]string{},
	}
}

// metadataIndexes are the lookups the scoring reads, rebuilt whenever the
// state changes.
type metadataIndexes struct {
	byCanonical map[string][]*Fact
	aa          map[string]*Fact
	fits        map[string]*benchmarkFit
	aaScale     float64
	calibration *calibration
	benchmark   *benchmarkKeyIndex
	media       map[string]*mediaIndex
	openrouter  *openRouterIndex
}

// metadataView is one consistent look at the cached state and its indexes.
type metadataView struct {
	st  *metadataState
	idx *metadataIndexes
}

// scale is the benchmark scale in use: the best model in the data is 1.0, and
// the fixed constant is only a fallback before any benchmark data is loaded.
func (v *metadataView) scale() float64 {
	if v.idx.aaScale > 0 {
		return v.idx.aaScale
	}
	return aaIndexScale
}

// Metadata owns the shared model metadata: the models.dev facts, the
// OpenRouter price list and benchmarks, the Artificial Analysis index and
// media arenas, and the MTEB embedding scores. It turns them into one
// canonical record per model and stores it in model_metadata.
type Metadata struct {
	client      *http.Client
	dataDir     string
	settings    Settings
	cfg         func() config.Config
	log         *slog.Logger
	observeNews func()

	// SkillsFromBenchmarks turns benchmark scores already mapped to the
	// intelligence scale into per-skill scores. It is routing's
	// skillsFromBenchmarks, injected so the two domains keep one mapping.
	SkillsFromBenchmarks func(mapped map[string]float64) map[string]float64

	db *dbx.DB

	mu      sync.Mutex
	state   *metadataState
	indexes *metadataIndexes
	// sources are the HTTP sources, kept so a test can point them elsewhere.
	modelsDev  modelsDevSource
	openRouter openRouterPublicSource
	benchmarks benchmarkSource
	analysis   artificialAnalysisSource
	mteb       mtebSource
}

// Record is one canonical model's metadata, the row model_metadata stores.
type Record struct {
	CanonicalID            string   `json:"canonical_id"`
	Intelligence           float64  `json:"intelligence"`
	IntelligenceSource     string   `json:"intelligence_source"`
	IntelligenceConfidence float64  `json:"intelligence_confidence"`
	SpeedTps               *float64 `json:"speed_tps"`
	ContextWindow          *int64   `json:"context_window"`
	MaxOutput              *int64   `json:"max_output"`
	PriceIn                *float64 `json:"price_in"`
	PriceOut               *float64 `json:"price_out"`
	SupportsTools          *int64   `json:"supports_tools"`
	SupportsVision         *int64   `json:"supports_vision"`
	SupportsReasoning      *int64   `json:"supports_reasoning"`
	ReleaseDate            *string  `json:"release_date"`
	ParamsB                *float64 `json:"params_b"`
	SkillsJSON             *string  `json:"skills_json"`
	SourcesJSON            string   `json:"sources_json"`
}

// Metadata status reports what the sources have and whether they can refresh.
type MetadataStatus struct {
	FetchedAt                    map[string]string `json:"fetchedAt"`
	ArtificialAnalysisConfigured bool              `json:"artificialAnalysisConfigured"`
	SourcesEnabled               bool              `json:"sourcesEnabled"`
}

// DiscoverySnapshot is the read-only projection the discovery service reads:
// public model metadata, never source credentials.
type DiscoverySnapshot struct {
	ArtificialAnalysis      []*Fact                  `json:"artificialAnalysis"`
	OpenRouter              []*Fact                  `json:"openrouter"`
	OpenRouterBenchmarks    *BenchmarkSnapshot       `json:"openrouterBenchmarks"`
	ArtificialAnalysisMedia map[string][]MediaRecord `json:"artificialAnalysisMedia"`
	MTEB                    map[string]MTEBScore     `json:"mteb"`
	FetchedAt               map[string]string        `json:"fetchedAt"`
}

// cacheFile is where the metadata cache lives.
func (m *Metadata) cacheFile() string {
	return filepath.Join(m.dataDir, "metadata_cache.json")
}

// ensureLoaded reads the cache file once and builds the indexes.
func (m *Metadata) ensureLoaded() {
	if m.state != nil {
		return
	}
	loaded := newMetadataState()
	if m.dataDir != "" {
		if data, err := os.ReadFile(m.cacheFile()); err == nil {
			if err := json.Unmarshal(data, loaded); err != nil {
				m.log.Warn("Metadata cache unreadable; starting fresh", "error", err)
				loaded = newMetadataState()
			}
		}
	}
	m.state = loaded
	m.rebuildIndexes()
}

// rebuildIndexes recomputes every lookup from the current state.
func (m *Metadata) rebuildIndexes() {
	st := m.state
	byCanonical := map[string][]*Fact{}
	// Provider and model ids are walked in sorted order so the derived records
	// (their source list, and the first Fact to speak for a field) do not
	// depend on Go map iteration order.
	providers := make([]string, 0, len(st.ModelsDev))
	for providerID := range st.ModelsDev {
		providers = append(providers, providerID)
	}
	sort.Strings(providers)
	for _, providerID := range providers {
		models := st.ModelsDev[providerID]
		modelIDs := make([]string, 0, len(models))
		for modelID := range models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)
		for _, modelID := range modelIDs {
			record := models[modelID]
			if record == nil {
				continue
			}
			copied := *record
			copied.Source = "models.dev"
			copied.ProviderID = providerID
			copied.ModelID = modelID
			canonical := CanonicalID(modelID)
			byCanonical[canonical] = append(byCanonical[canonical], &copied)
		}
	}
	for _, record := range st.OpenRouter {
		if record == nil {
			continue
		}
		copied := *record
		copied.Source = "openrouter"
		canonical := CanonicalID(record.ID)
		byCanonical[canonical] = append(byCanonical[canonical], &copied)
	}
	aaMax := 0.0
	for _, record := range st.ArtificialAnalysis {
		if record == nil || record.IndexDerived {
			continue
		}
		aaMax = max64(aaMax, floatOrZero(record.IntelligenceIndex))
	}
	fits := deriveIndexes(st.ArtificialAnalysis)
	aa := map[string]*Fact{}
	for _, record := range st.ArtificialAnalysis {
		if record == nil {
			continue
		}
		for _, key := range []string{CanonicalID(record.Slug), CanonicalID(record.Name)} {
			if key == "" {
				continue
			}
			if _, taken := aa[key]; !taken {
				aa[key] = record
			}
		}
	}
	scale := aaMax
	if scale <= 0 {
		scale = aaIndexScale
	}
	m.indexes = &metadataIndexes{
		byCanonical: byCanonical,
		aa:          aa,
		fits:        fits,
		aaScale:     scale,
		benchmark:   buildBenchmarkKeys(st.ArtificialAnalysis),
		media:       buildMediaIndexes(st.ArtificialAnalysisMedia),
	}
	view := &metadataView{st: st, idx: m.indexes}
	if st.OpenRouterBenchmarks != nil {
		view.idx.openrouter = view.buildOpenRouterIndex(st.OpenRouterBenchmarks.Models)
	}
	view.idx.calibration = view.fitCalibration()
}

// saveState writes the cache file atomically with private permissions.
func (m *Metadata) saveState() error {
	if m.dataDir == "" {
		return nil
	}
	data, err := json.Marshal(m.state)
	if err != nil {
		return fmt.Errorf("catalog: save metadata cache: %w", err)
	}
	file := m.cacheFile()
	temporary := file + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("catalog: save metadata cache: %w", err)
	}
	if err := os.Rename(temporary, file); err != nil {
		return fmt.Errorf("catalog: save metadata cache: %w", err)
	}
	return nil
}

// withView runs fn over a consistent state and index snapshot.
func (m *Metadata) withView(fn func(*metadataView)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLoaded()
	fn(&metadataView{st: m.state, idx: m.indexes})
}
