package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// artificialAnalysisURL serves the intelligence index and measured output
// speed per model. Requires an API key configured by an admin: a Free key gets
// the /free lists (headline indexes and speed, no per-benchmark scores).
const artificialAnalysisURL = "https://artificialanalysis.ai/api/v2"

// aaBenchmarks are the per-benchmark scores that become skills.
var aaBenchmarks = []string{"gpqa", "hle", "mmlu_pro", "livecodebench", "scicode", "aime_25", "aime",
	"math_500", "ifbench", "lcr", "terminalbench_hard", "tau2"}

// aaBenchmarkAliases maps the names the current (Pro) API uses onto the ones
// the skills are built on.
var aaBenchmarkAliases = map[string]string{
	"gpqa_diamond":       "gpqa",
	"aa_lcr":             "lcr",
	"terminalbench_v4_0": "terminalbench_hard",
}

// aaMaxPages is a guard, not an expectation: the quota is what really limits us.
const aaMaxPages = 20

// aaTierMemory is how long a key is remembered as Free before the full list is
// asked for again (a key can be upgraded).
const aaTierMemory = 24 * time.Hour

// artificialAnalysisSource talks to the Artificial Analysis API.
type artificialAnalysisSource struct {
	baseURL string
	client  *http.Client
	// freeTierUntil remembers a key that was refused the full list.
	freeTierUntil time.Time
}

// aaItem is one entry of a list the API returns.
type aaItem struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	ReleaseDate  string   `json:"release_date"`
	Elo          *float64 `json:"elo"`
	ModelCreator struct {
		Name string `json:"name"`
	} `json:"model_creator"`
	Evaluations map[string]any `json:"evaluations"`
	Performance struct {
		MedianOutputTokensPerSecond   any `json:"median_output_tokens_per_second"`
		MedianTimeToFirstTokenSeconds any `json:"median_time_to_first_token_seconds"`
	} `json:"performance"`
}

// getPage fetches one page of a list.
func (s *artificialAnalysisSource) getPage(ctx context.Context, path string, page int, apiKey string) (int, []byte, error) {
	url := s.baseURL + "/" + path
	if page > 1 {
		url += fmt.Sprintf("?page=%d", page)
	}
	return httpGet(ctx, s.client, url, map[string]string{"x-api-key": apiKey})
}

// fetchList returns every item of a model list, from the full list or, on a
// Free key, its /free sibling. One failed page fails the whole list: half a
// list must not replace a complete one.
func (s *artificialAnalysisSource) fetchList(ctx context.Context, path, apiKey string, now time.Time) ([]aaItem, error) {
	listPath := path
	if now.Before(s.freeTierUntil) {
		listPath = path + "/free"
	}
	status, body, err := s.getPage(ctx, listPath, 1, apiKey)
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden && listPath == path {
		s.freeTierUntil = now.Add(aaTierMemory)
		listPath = path + "/free"
		status, body, err = s.getPage(ctx, listPath, 1, apiKey)
		if err != nil {
			return nil, err
		}
	}
	items := []aaItem{}
	for page := 1; page <= aaMaxPages; page++ {
		if page > 1 {
			status, body, err = s.getPage(ctx, listPath, page, apiKey)
			if err != nil {
				return nil, err
			}
		}
		if status < 200 || status > 299 {
			return nil, fmt.Errorf("Artificial Analysis (%s, page %d) returned HTTP %d", listPath, page, status)
		}
		var data struct {
			Data       []aaItem `json:"data"`
			Pagination struct {
				HasMore bool `json:"has_more"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, fmt.Errorf("catalog: artificial analysis: %w", err)
		}
		items = append(items, data.Data...)
		if !data.Pagination.HasMore {
			break
		}
	}
	return items, nil
}

// fetch reads the language model list on top of what was already known.
func (s *artificialAnalysisSource) fetch(ctx context.Context, apiKey string, previous []*Fact) ([]*Fact, error) {
	items, err := s.fetchList(ctx, "language/models", apiKey, time.Now())
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("Artificial Analysis returned no models")
	}
	records := make([]*Fact, 0, len(items))
	for _, item := range items {
		records = append(records, toArtificialAnalysisFact(item))
	}
	return mergeKnown(records, previous), nil
}

// toArtificialAnalysisFact normalizes one API entry. The current API calls the
// skills-improving benchmarks differently and publishes speed separately.
func toArtificialAnalysisFact(model aaItem) *Fact {
	record := &Fact{
		ID:                model.ID,
		Slug:              firstNonEmpty(model.Slug, model.Name),
		Name:              model.Name,
		OutputTps:         truthyNumber(model.Performance.MedianOutputTokensPerSecond),
		TtftSeconds:       truthyNumber(model.Performance.MedianTimeToFirstTokenSeconds),
		ReleaseDate:       model.ReleaseDate,
		IntelligenceIndex: truthyNumber(model.Evaluations["artificial_analysis_intelligence_index"]),
		Benchmarks:        map[string]float64{},
	}
	for name, value := range model.Evaluations {
		benchmark := name
		if alias, ok := aaBenchmarkAliases[name]; ok {
			benchmark = alias
		}
		if !containsString(aaBenchmarks, benchmark) || value == nil {
			continue
		}
		number, ok := finiteNumber(value)
		if !ok {
			continue
		}
		record.Benchmarks[benchmark] = *number
	}
	return record
}

// mergeKnown puts the new list on top of what was known. Per-benchmark scores
// a list no longer carries stay, and so do models it dropped, for those scores
// alone: an index is only ever taken from the current list, since its scale
// changes between versions.
func mergeKnown(records, previous []*Fact) []*Fact {
	known := make(map[string]*Fact, len(previous))
	for _, record := range previous {
		if record != nil && record.Slug != "" {
			known[record.Slug] = record
		}
	}
	merged := make([]*Fact, 0, len(records)+len(known))
	for _, record := range records {
		earlier := known[record.Slug]
		delete(known, record.Slug)
		merged = append(merged, mergeFact(record, earlier))
	}
	for _, earlier := range previous {
		if earlier == nil || known[earlier.Slug] != earlier {
			continue
		}
		if len(earlier.Benchmarks) > 0 {
			merged = append(merged, &Fact{
				ID: earlier.ID, Slug: earlier.Slug, Name: earlier.Name,
				Benchmarks: earlier.Benchmarks, OutputTps: earlier.OutputTps,
				TtftSeconds: earlier.TtftSeconds, ReleaseDate: earlier.ReleaseDate,
			})
		}
		delete(known, earlier.Slug)
	}
	return merged
}

// mergeFact overlays a record's benchmarks on the ones known from before.
func mergeFact(record, earlier *Fact) *Fact {
	merged := *record
	merged.Benchmarks = map[string]float64{}
	if earlier != nil {
		for name, value := range earlier.Benchmarks {
			merged.Benchmarks[name] = value
		}
	}
	for name, value := range record.Benchmarks {
		merged.Benchmarks[name] = value
	}
	return &merged
}

// MediaRecord is one arena entry: a media model with its blind-vote ELO.
type MediaRecord struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Creator     *string `json:"creator"`
	Elo         float64 `json:"elo"`
	ReleaseDate *string `json:"releaseDate"`
}

// aaMediaArenas are the arenas per media kind, in the order the JS fetches them.
var aaMediaArenas = []struct{ kind, arena string }{
	{"image", "text-to-image"},
	{"speech", "text-to-speech"},
	{"video", "text-to-video"},
}

// fetchMedia reads every media arena: the quality signal for image, speech and
// video models, the way the intelligence index serves chat models.
func (s *artificialAnalysisSource) fetchMedia(ctx context.Context, apiKey string) (map[string][]MediaRecord, error) {
	result := map[string][]MediaRecord{}
	for _, arena := range aaMediaArenas {
		items, err := s.fetchList(ctx, "media/"+arena.arena+"/models", apiKey, time.Now())
		if err != nil {
			return nil, err
		}
		records := []MediaRecord{}
		for _, item := range items {
			if item.Elo == nil {
				continue
			}
			record := MediaRecord{Slug: item.Slug, Name: item.Name, Elo: *item.Elo}
			if item.ModelCreator.Name != "" {
				creator := item.ModelCreator.Name
				record.Creator = &creator
			}
			if item.ReleaseDate != "" {
				release := item.ReleaseDate
				record.ReleaseDate = &release
			}
			records = append(records, record)
		}
		result[arena.kind] = records
	}
	return result, nil
}
