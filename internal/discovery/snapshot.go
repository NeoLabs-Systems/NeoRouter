package discovery

import (
	"math"
	"sort"
	"strings"
)

// Catalog is the model-metadata surface the discovery pages read. The catalog
// domain satisfies it through an adapter built in internal/app; nothing here
// touches its tables or its source credentials.
type Catalog interface {
	// DiscoverySnapshot is the public metadata projection: measured results per
	// source and when each source was fetched. It must not carry credentials.
	DiscoverySnapshot() Snapshot
	// BenchmarkIdentity maps a provider model id onto the canonical id the
	// benchmark sources are keyed by.
	BenchmarkIdentity(modelID, capability string) string
	// VariantBase splits a variant id such as "vendor/hy4-flash:free" into its
	// measured base and the score share a variant keeps. ok is false when the id
	// carries no variant word.
	VariantBase(modelID string) (base string, share float64, ok bool)
}

// Snapshot is the read-only metadata projection measurements are built from.
type Snapshot struct {
	ArtificialAnalysis      []AAModel
	OpenRouter              []OpenRouterModel
	OpenRouterBenchmarks    []BenchmarkModel
	ArtificialAnalysisMedia map[string][]ArenaModel
	MTEB                    map[string]MTEBModel
	// FetchedAt is the ISO timestamp of the last successful fetch, per source id.
	FetchedAt map[string]string
}

// AAModel is one Artificial Analysis result.
type AAModel struct {
	Slug              string
	Name              string
	ReleaseDate       string
	Benchmarks        map[string]float64
	IntelligenceIndex *float64
	// IndexDerived marks a model whose index was derived from other sources
	// rather than measured, which must not count as a benchmark.
	IndexDerived bool
}

// OpenRouterModel is one OpenRouter listing: its free flag and published prices.
type OpenRouterModel struct {
	ID            string
	IsFree        bool
	PriceIn       *float64
	PriceOut      *float64
	ContextWindow *int
}

// BenchmarkModel is one OpenRouter benchmark result.
type BenchmarkModel struct {
	Slug       string
	Name       string
	Benchmarks map[string]float64
}

// ArenaModel is one Artificial Analysis media arena result.
type ArenaModel struct {
	Slug        string
	Name        string
	ReleaseDate string
	Elo         float64
}

// MTEBModel is one MTEB embedding result.
type MTEBModel struct {
	Model string
	Score float64
}

// SourceInfo identifies one benchmark source as the API reports it.
type SourceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Record is one measured result from one source, before aggregation.
type Record struct {
	ID          string
	Name        string
	Capability  string
	ReleaseDate *string
	Metrics     map[string]float64
	SourceID    string
}

// source is one adapter: it emits measured results only, so a new source can be
// added without touching aggregation, comparisons or news selection.
type source struct {
	info    SourceInfo
	records func(Snapshot) []Record
}

var sources = []source{
	{
		info: SourceInfo{ID: "artificialAnalysis", Name: "Artificial Analysis", URL: "https://artificialanalysis.ai/"},
		records: func(snapshot Snapshot) []Record {
			out := make([]Record, 0, len(snapshot.ArtificialAnalysis))
			for _, model := range snapshot.ArtificialAnalysis {
				metrics := make(map[string]float64, len(model.Benchmarks)+1)
				for name, value := range model.Benchmarks {
					metrics[name] = value
				}
				if !model.IndexDerived && model.IntelligenceIndex != nil {
					metrics["intelligence_index"] = *model.IntelligenceIndex
				}
				out = append(out, Record{
					ID: model.Slug, Name: model.Name, Capability: "chat",
					ReleaseDate: optional(model.ReleaseDate), Metrics: metrics,
				})
			}
			return out
		},
	},
	{
		info: SourceInfo{ID: "openrouterBenchmarks", Name: "OpenRouter", URL: "https://openrouter.ai/"},
		records: func(snapshot Snapshot) []Record {
			out := make([]Record, 0, len(snapshot.OpenRouterBenchmarks))
			for _, model := range snapshot.OpenRouterBenchmarks {
				out = append(out, Record{
					ID: model.Slug, Name: model.Name, Capability: "chat",
					Metrics: copyMetrics(model.Benchmarks),
				})
			}
			return out
		},
	},
	{
		info: SourceInfo{ID: "artificialAnalysisMedia", Name: "Artificial Analysis · Arena", URL: "https://artificialanalysis.ai/"},
		records: func(snapshot Snapshot) []Record {
			capabilities := make([]string, 0, len(snapshot.ArtificialAnalysisMedia))
			for capability := range snapshot.ArtificialAnalysisMedia {
				capabilities = append(capabilities, capability)
			}
			sort.Strings(capabilities)
			var out []Record
			for _, capability := range capabilities {
				for _, model := range snapshot.ArtificialAnalysisMedia[capability] {
					out = append(out, Record{
						ID: model.Slug, Name: model.Name, Capability: capability,
						ReleaseDate: optional(model.ReleaseDate),
						Metrics:     map[string]float64{"arena_elo": model.Elo},
					})
				}
			}
			return out
		},
	},
	{
		info: SourceInfo{ID: "mteb", Name: "MTEB", URL: "https://huggingface.co/spaces/mteb/leaderboard"},
		records: func(snapshot Snapshot) []Record {
			ids := make([]string, 0, len(snapshot.MTEB))
			for id := range snapshot.MTEB {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			out := make([]Record, 0, len(ids))
			for _, id := range ids {
				model := snapshot.MTEB[id]
				name := model.Model
				if name == "" {
					name = id
				}
				out = append(out, Record{
					ID: id, Name: name, Capability: "embedding",
					Metrics: map[string]float64{"mteb": model.Score},
				})
			}
			return out
		},
	},
}

// Measurements flattens the snapshot into measured results: records with an id
// and at least one finite metric, tagged with their source.
func Measurements(snapshot Snapshot) []Record {
	var out []Record
	for _, source := range sources {
		for _, record := range source.records(snapshot) {
			if record.ID == "" {
				continue
			}
			record.SourceID = source.info.ID
			record.Metrics = finiteMetrics(record.Metrics)
			if len(record.Metrics) == 0 {
				continue
			}
			out = append(out, record)
		}
	}
	return out
}

// finiteMetrics keeps the metrics that are real numbers.
func finiteMetrics(metrics map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(metrics))
	for name, value := range metrics {
		if isFinite(value) {
			out[name] = value
		}
	}
	return out
}

func copyMetrics(metrics map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(metrics))
	for name, value := range metrics {
		out[name] = value
	}
	return out
}

// optional turns a possibly empty string into the nullable string the API emits.
func optional(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

// isFinite reports whether value is a real number (not NaN or an infinity).
func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
