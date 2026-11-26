package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// openRouterBenchmarksURL publishes daily benchmark exports: GPQA Diamond
// (hard reasoning) and tau2-bench airline (agent work with tools), kept to the
// newest 90 days.
const openRouterBenchmarksURL = "https://openrouter.ai/api/v1/datasets/exports/benchmark-leaderboard"

// openRouterBenchmarkNames maps the export's benchmark ids onto the skill keys.
var openRouterBenchmarkNames = map[string]string{
	"gpqa_diamond":               "gpqa",
	"tau_bench_verified_airline": "tau",
}

// benchmarkMinQuestions: a host with few questions says little.
const benchmarkMinQuestions = 30

// BenchmarkModel is one model's benchmark results from one snapshot.
type BenchmarkModel struct {
	Slug       string             `json:"slug"`
	Name       string             `json:"name"`
	Benchmarks map[string]float64 `json:"benchmarks"`
	LastRun    string             `json:"lastRun"`
}

// BenchmarkSnapshot is one dated export.
type BenchmarkSnapshot struct {
	SnapshotDate string           `json:"snapshotDate"`
	Models       []BenchmarkModel `json:"models"`
}

type benchmarkRow struct {
	Benchmark        string  `json:"benchmark"`
	ModelSlug        string  `json:"model_slug"`
	ModelName        string  `json:"model_name"`
	ModelPermaslug   string  `json:"model_permaslug"`
	ProviderName     string  `json:"provider_name"`
	Accuracy         float64 `json:"accuracy"`
	TotalQuestions   float64 `json:"total_questions"`
	LastRunTimestamp string  `json:"last_run_timestamp"`
}

type benchmarkSource struct {
	baseURL string
	client  *http.Client
}

// benchmarkHost accumulates one benchmark's rows per provider host.
type benchmarkHost struct {
	permaslugs []string
	rows       map[string][]benchmarkRow
	lastRun    map[string]string
}

func (h *benchmarkHost) add(row benchmarkRow) {
	if h.rows == nil {
		h.rows = map[string][]benchmarkRow{}
		h.lastRun = map[string]string{}
	}
	if _, seen := h.rows[row.ModelPermaslug]; !seen {
		h.permaslugs = append(h.permaslugs, row.ModelPermaslug)
	}
	h.rows[row.ModelPermaslug] = append(h.rows[row.ModelPermaslug], row)
	if row.LastRunTimestamp > h.lastRun[row.ModelPermaslug] {
		h.lastRun[row.ModelPermaslug] = row.LastRunTimestamp
	}
}

// toBenchmarkModels reduces rows to one score per model and benchmark: its
// newest version, the middle host. A host serving a broken or heavily
// cut-down copy must not decide for the model, and the luckiest host must not
// either.
func toBenchmarkModels(rows []benchmarkRow) []BenchmarkModel {
	versions := map[string]*benchmarkHost{}
	order := []string{}
	for _, row := range rows {
		benchmark, known := openRouterBenchmarkNames[row.Benchmark]
		if !known || row.ModelSlug == "" || !(row.Accuracy > 0) || !(row.TotalQuestions >= benchmarkMinQuestions) {
			continue
		}
		key := row.ModelSlug + "|" + benchmark
		host, seen := versions[key]
		if !seen {
			host = &benchmarkHost{}
			versions[key] = host
			order = append(order, key)
		}
		host.add(row)
	}
	models := map[string]*BenchmarkModel{}
	modelOrder := []string{}
	for _, key := range order {
		host := versions[key]
		slug, benchmark := splitBenchmarkKey(key)
		newest := ""
		for _, permaslug := range host.permaslugs {
			if newest == "" || host.lastRun[permaslug] > host.lastRun[newest] {
				newest = permaslug
			}
		}
		model, seen := models[slug]
		if !seen {
			name := slug
			if len(host.rows[newest]) > 0 && host.rows[newest][0].ModelName != "" {
				name = host.rows[newest][0].ModelName
			}
			model = &BenchmarkModel{Slug: slug, Name: name, Benchmarks: map[string]float64{}}
			models[slug] = model
			modelOrder = append(modelOrder, slug)
		}
		accuracies := make([]float64, 0, len(host.rows[newest]))
		for _, row := range host.rows[newest] {
			accuracies = append(accuracies, row.Accuracy)
		}
		model.Benchmarks[benchmark] = medianOf(accuracies)
		if lastRun := host.lastRun[newest]; lastRun > model.LastRun {
			model.LastRun = lastRun
		}
	}
	result := make([]BenchmarkModel, 0, len(modelOrder))
	for _, slug := range modelOrder {
		result = append(result, *models[slug])
	}
	return result
}

func splitBenchmarkKey(key string) (slug, benchmark string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// fetch reads the newest snapshot: each UTC day has its own file, which may
// not be out yet.
func (s benchmarkSource) fetch(ctx context.Context, previous *BenchmarkSnapshot, now time.Time) (*BenchmarkSnapshot, error) {
	today := now.UTC().Format("2006-01-02")
	// Dated files never change, and the rest of the catalog refreshes hourly.
	if previous != nil && previous.SnapshotDate == today {
		return previous, nil
	}
	yesterday := now.Add(-24 * time.Hour).UTC().Format("2006-01-02")
	lastStatus := 0
	for _, name := range []string{today, yesterday, "latest"} {
		status, body, err := httpGet(ctx, s.client, s.baseURL+"/"+name+".json", nil)
		if err != nil {
			return nil, err
		}
		if status >= 200 && status <= 299 {
			var data struct {
				SnapshotDate string         `json:"snapshot_date"`
				Rows         []benchmarkRow `json:"rows"`
			}
			if err := json.Unmarshal(body, &data); err != nil {
				return nil, fmt.Errorf("catalog: openrouter benchmarks: %w", err)
			}
			models := toBenchmarkModels(data.Rows)
			if len(models) == 0 {
				return nil, fmt.Errorf("OpenRouter benchmarks had no usable rows")
			}
			snapshotDate := data.SnapshotDate
			if snapshotDate == "" {
				snapshotDate = name
			}
			return &BenchmarkSnapshot{SnapshotDate: snapshotDate, Models: models}, nil
		}
		lastStatus = status
	}
	return nil, fmt.Errorf("OpenRouter benchmarks returned HTTP %d", lastStatus)
}

// medianOf is the middle value of a sorted copy, the mean of the two middle
// values for an even count.
func medianOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}
