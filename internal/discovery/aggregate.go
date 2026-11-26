package discovery

import (
	"sort"
	"strings"
)

// Identity maps a provider model id onto the canonical id benchmarks are keyed
// by; it is the catalog's benchmarkIdentity.
type Identity func(modelID, capability string) string

// SourceScore is one source's average percentile for a model.
type SourceScore struct {
	SourceID string  `json:"sourceId"`
	Score    float64 `json:"score"`
}

// Score is one measured metric of a model, ranked against its peers.
type Score struct {
	ID         string  `json:"id"`
	SourceID   string  `json:"sourceId"`
	Metric     string  `json:"metric"`
	Value      float64 `json:"value"`
	Percentile float64 `json:"percentile"`
	Peers      int     `json:"peers"`
}

// Model is one aggregated model: every measurement of it, one score per source,
// and the mean of those scores.
type Model struct {
	ID           string        `json:"id"`
	CanonicalID  string        `json:"canonicalId"`
	Name         string        `json:"name"`
	Capability   string        `json:"capability"`
	ReleaseDate  *string       `json:"releaseDate"`
	Score        float64       `json:"score"`
	SourceCount  int           `json:"sourceCount"`
	MetricCount  int           `json:"metricCount"`
	SourceScores []SourceScore `json:"sourceScores"`
	Scores       []Score       `json:"scores"`
}

// Source is one benchmark source as the API reports it: when it was last
// fetched and how many models it measured.
type Source struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	URL        string  `json:"url"`
	UpdatedAt  *string `json:"updatedAt"`
	ModelCount int     `json:"modelCount"`
}

// Aggregate is the leaderboard: every measured model, best first, and the
// sources it was built from.
type Aggregate struct {
	Models  []Model  `json:"models"`
	Sources []Source `json:"sources"`
}

// percentiles ranks values with midrank percentiles: ties share a rank and a
// lone measurement sits at the midpoint (50). Raw scales stay separate because
// only values of the same metric are ever ranked together.
func percentiles(values []float64) map[float64]float64 {
	distinct := make(map[float64]bool, len(values))
	counts := make(map[float64]int, len(values))
	unique := make([]float64, 0, len(values))
	for _, value := range values {
		if !distinct[value] {
			distinct[value] = true
			unique = append(unique, value)
		}
		counts[value]++
	}
	sort.Float64s(unique)
	ranks := make(map[float64]float64, len(unique))
	below := 0
	for _, value := range unique {
		ties := counts[value]
		if len(values) == 1 {
			ranks[value] = 50
		} else {
			ranks[value] = 100 * (float64(below) + float64(ties-1)/2) / float64(len(values)-1)
		}
		below += ties
	}
	return ranks
}

// observation is one source metric of one model, averaged over its aliases.
type observation struct {
	id       string
	sourceID string
	metric   string
	values   map[float64]bool
	order    []float64
	value    float64
}

// group is one model being aggregated.
type group struct {
	id           string
	canonicalID  string
	name         string
	capability   string
	releaseDate  *string
	observations map[string]*observation
	order        []string
}

// Build is server/services/discovery/benchmarks/aggregate.js: every model with
// its per-source percentiles and a mean score, best first.
func Build(snapshot Snapshot, identity Identity) Aggregate {
	groups := make(map[string]*group)
	var groupOrder []string
	for _, record := range Measurements(snapshot) {
		canonical := identity(record.ID, record.Capability)
		key := record.Capability + ":" + canonical
		entry, ok := groups[key]
		if !ok {
			name := record.Name
			if name == "" {
				name = record.ID
			}
			entry = &group{
				id: key, canonicalID: canonical, name: name,
				capability: record.Capability, releaseDate: record.ReleaseDate,
				observations: make(map[string]*observation),
			}
			groups[key] = entry
			groupOrder = append(groupOrder, key)
		}
		for _, metric := range sortedKeys(record.Metrics) {
			metricID := record.SourceID + ":" + metric
			obs, ok := entry.observations[metricID]
			if !ok {
				// Host aliases in one source must not multiply that source's vote.
				obs = &observation{
					id: metricID, sourceID: record.SourceID, metric: metric,
					values: make(map[float64]bool),
				}
				entry.observations[metricID] = obs
				entry.order = append(entry.order, metricID)
			}
			if !obs.values[record.Metrics[metric]] {
				obs.values[record.Metrics[metric]] = true
				obs.order = append(obs.order, record.Metrics[metric])
			}
		}
	}

	peerValues := make(map[string][]float64)
	for _, key := range groupOrder {
		entry := groups[key]
		for _, metricID := range entry.order {
			obs := entry.observations[metricID]
			obs.value = mean(obs.order)
			peerKey := entry.capability + ":" + obs.id
			peerValues[peerKey] = append(peerValues[peerKey], obs.value)
		}
	}
	rankings := make(map[string]map[float64]float64, len(peerValues))
	for key, values := range peerValues {
		rankings[key] = percentiles(values)
	}

	models := make([]Model, 0, len(groupOrder))
	for _, key := range groupOrder {
		entry := groups[key]
		sourceScores := []SourceScore{}
		bySource := make(map[string][]float64)
		var sourceOrder []string
		scores := make([]Score, 0, len(entry.order))
		for _, metricID := range entry.order {
			obs := entry.observations[metricID]
			peerKey := entry.capability + ":" + obs.id
			percentile := rankings[peerKey][obs.value]
			if _, ok := bySource[obs.sourceID]; !ok {
				sourceOrder = append(sourceOrder, obs.sourceID)
			}
			bySource[obs.sourceID] = append(bySource[obs.sourceID], percentile)
			scores = append(scores, Score{
				ID: obs.id, SourceID: obs.sourceID, Metric: obs.metric, Value: obs.value,
				Percentile: percentile, Peers: len(peerValues[peerKey]),
			})
		}
		totals := make([]float64, 0, len(sourceOrder))
		for _, sourceID := range sourceOrder {
			score := mean(bySource[sourceID])
			totals = append(totals, score)
			sourceScores = append(sourceScores, SourceScore{SourceID: sourceID, Score: score})
		}
		models = append(models, Model{
			ID: entry.id, CanonicalID: entry.canonicalID, Name: entry.name,
			Capability: entry.capability, ReleaseDate: entry.releaseDate,
			Score: mean(totals), SourceCount: len(sourceScores), MetricCount: len(scores),
			SourceScores: sourceScores, Scores: scores,
		})
	}
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Score != models[j].Score {
			return models[i].Score > models[j].Score
		}
		if models[i].SourceCount != models[j].SourceCount {
			return models[i].SourceCount > models[j].SourceCount
		}
		return strings.Compare(models[i].Name, models[j].Name) < 0
	})

	rows := make([]Source, 0, len(sources))
	for _, source := range sources {
		fetched, ok := snapshot.FetchedAt[source.info.ID]
		var updatedAt *string
		if ok && fetched != "" {
			updatedAt = &fetched
		}
		count := 0
		for _, model := range models {
			for _, score := range model.SourceScores {
				if score.SourceID == source.info.ID {
					count++
					break
				}
			}
		}
		rows = append(rows, Source{
			ID: source.info.ID, Name: source.info.Name, URL: source.info.URL,
			UpdatedAt: updatedAt, ModelCount: count,
		})
	}
	return Aggregate{Models: models, Sources: rows}
}

// mean is the JS sources' mean(); an empty slice has no mean.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

// sortedKeys orders a metric map, so aggregation does not depend on Go's map
// order (the JS order comes from the remote JSON and is not a contract).
func sortedKeys(metrics map[string]float64) []string {
	keys := make([]string, 0, len(metrics))
	for name := range metrics {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}
