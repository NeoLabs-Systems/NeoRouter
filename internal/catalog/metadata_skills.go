package catalog

import (
	"math"
	"sort"
	"strings"
)

// Heuristic weights when no benchmark index is available. Each signal is
// normalized to [0, 1]; missing signals drop out and lower the confidence.
// benchmarkKeys reduces a benchmark or provider model name to matchable keys:
// both sides lose dates, reasoning-effort levels and serving suffixes. One key
// keeps the parameter sizes, a looser one drops them.
// buildOpenRouterIndex indexes OpenRouter's benchmark results and learns how
// each benchmark's accuracy translates to the intelligence scale.
func (v *metadataView) buildOpenRouterIndex(models []BenchmarkModel) *openRouterIndex {
	index := &openRouterIndex{
		exact: map[string]*BenchmarkModel{}, loose: map[string]*BenchmarkModel{},
		curves: map[string][][2]float64{}, best: map[string]float64{}, agreement: map[string]float64{},
	}
	for i := range models {
		model := &models[i]
		last := model.Slug
		if at := strings.LastIndexByte(last, '/'); at >= 0 {
			last = last[at+1:]
		}
		withParams, withoutParams := benchmarkKeys(last)
		index.exact[canonicalID(model.Slug)] = model
		if withParams != "" {
			if _, taken := index.exact[withParams]; !taken {
				index.exact[withParams] = model
			}
		}
		if withoutParams != "" {
			if _, taken := index.loose[withoutParams]; taken {
				index.loose[withoutParams] = nil
			} else {
				index.loose[withoutParams] = model
			}
		}
	}
	for _, name := range []string{"gpqa", "tau"} {
		pairs := [][2]float64{}
		top := 0.0
		for i := range models {
			model := &models[i]
			value, ok := model.Benchmarks[name]
			if !ok {
				continue
			}
			top = math.Max(top, value)
			record := v.lookupBenchmark(canonicalID(model.Slug), []string{model.Slug})
			if record != nil && truthyFloat(record.IntelligenceIndex) && !record.IndexDerived {
				pairs = append(pairs, [2]float64{value, *record.IntelligenceIndex})
			}
		}
		index.best[name] = top
		rho := 0.0
		if len(pairs) >= minFitPairs {
			rho = spearman(pairs)
		}
		if rho > 0 {
			index.curves[name] = isotonic(pairs)
			// How far this benchmark may speak for a model overall: by how well
			// it ranks models the way the fuller index does.
			index.agreement[name] = rho * rho
		}
	}
	return index
}

// openRouterMatch finds a model's OpenRouter benchmark record.
func (v *metadataView) openRouterMatch(canonical string, modelIDs []string) *BenchmarkModel {
	index := v.idx.openrouter
	if index == nil {
		return nil
	}
	if model, ok := index.exact[canonical]; ok {
		return model
	}
	for _, id := range modelIDs {
		text := id
		if at := strings.LastIndexByte(text, '/'); at >= 0 {
			text = text[at+1:]
		}
		withParams, withoutParams := benchmarkKeys(text)
		if model, ok := index.exact[withParams]; ok {
			return model
		}
		if match, ok := index.loose[withoutParams]; ok && match != nil && sameSize(text, &Fact{Slug: match.Slug, ProviderFacts: ProviderFacts{Name: match.Name}}) {
			return match
		}
	}
	return nil
}

// openRouterSkills is a model's OpenRouter results on the intelligence scale,
// as skills.
func (v *metadataView) openRouterSkills(model *BenchmarkModel) map[string]float64 {
	index := v.idx.openrouter
	if model == nil || index == nil {
		return nil
	}
	scaled := func(name string) *float64 {
		value, ok := model.Benchmarks[name]
		if !ok {
			return nil
		}
		curve, hasCurve := index.curves[name]
		if hasCurve {
			// Compared with the fuller index and found to rank models very
			// differently: its scale cannot be trusted, so it gets no say.
			if agreement, known := index.agreement[name]; known && agreement < minRankAgreement {
				return nil
			}
			scaledValue := math.Min(1, mapScore(curve, value)/v.scale())
			return &scaledValue
		}
		if index.best[name] > 0 {
			scaledValue := value / index.best[name]
			return &scaledValue
		}
		return nil
	}
	skills := map[string]float64{}
	if reasoning := scaled("gpqa"); reasoning != nil {
		skills["reasoning"] = *reasoning
	}
	if agentic := scaled("tau"); agentic != nil {
		skills["agentic"] = *agentic
	}
	if len(skills) == 0 {
		return nil
	}
	return skills
}

// openRouterOverall turns those skills into one overall number, each as far as
// its benchmark agrees with the fuller index. Without that index to compare
// with, they count equally.
func (v *metadataView) openRouterOverall(skills map[string]float64) *float64 {
	agreement := map[string]float64{}
	if v.idx.openrouter != nil {
		agreement = v.idx.openrouter.agreement
	}
	compared := len(agreement) > 0
	total, weight := 0.0, 0.0
	for _, pair := range [][2]string{{"reasoning", "gpqa"}, {"agentic", "tau"}} {
		value, ok := skills[pair[0]]
		if !ok {
			continue
		}
		share := 1.0
		if compared {
			share = agreement[pair[1]]
		}
		total += share * value
		weight += share
	}
	if weight <= 0 {
		return nil
	}
	overall := total / weight
	return &overall
}

// mergeSkills counts a skill both sources measured as their average. Where
// only one source measured it, that source is used.
func mergeSkills(first, second map[string]float64) map[string]float64 {
	if len(first) == 0 || len(second) == 0 {
		if len(first) > 0 {
			return first
		}
		if len(second) > 0 {
			return second
		}
		return nil
	}
	merged := make(map[string]float64, len(first)+len(second))
	for skill, value := range first {
		merged[skill] = value
	}
	for skill, value := range second {
		if known, ok := first[skill]; ok {
			merged[skill] = roundTo((known+value)/2, 4)
			continue
		}
		merged[skill] = roundTo(value, 4)
	}
	return merged
}

// lookupBenchmark finds the benchmark record for a model, by canonical id or
// by any of its ids.
func (v *metadataView) lookupBenchmark(canonical string, modelIDs []string) *Fact {
	direct := v.idx.aa[canonical]
	if direct != nil && truthyFloat(direct.IntelligenceIndex) {
		return direct
	}
	index := v.idx.benchmark
	if index == nil {
		return direct
	}
	for _, id := range modelIDs {
		candidates := []string{id}
		if at := strings.LastIndexByte(id, '/'); at >= 0 {
			candidates = append(candidates, id[at+1:])
		}
		for _, text := range candidates {
			withParams, _ := benchmarkKeys(text)
			if record, ok := index.exact[withParams]; ok {
				return record
			}
		}
		for _, text := range candidates {
			_, withoutParams := benchmarkKeys(text)
			if match, ok := index.loose[withoutParams]; ok && match != nil && sameSize(text, match) {
				return match
			}
		}
	}
	return direct
}

// fitCalibration is the least-squares line from the raw guess to the benchmark
// score, over every model that has both.
func (v *metadataView) fitCalibration() *calibration {
	pairs := [][2]float64{}
	keys := make([]string, 0, len(v.idx.byCanonical))
	for canonical := range v.idx.byCanonical {
		keys = append(keys, canonical)
	}
	sort.Strings(keys)
	for _, canonical := range keys {
		records := v.idx.byCanonical[canonical]
		ids := make([]string, 0, len(records))
		for _, record := range records {
			if id := firstNonEmpty(record.ModelID, record.ID); id != "" {
				ids = append(ids, id)
			}
		}
		record := v.lookupBenchmark(canonical, ids)
		if record == nil || !truthyFloat(record.IntelligenceIndex) {
			continue
		}
		guess := v.heuristicIntelligence(records, ids, record)
		if guess.WeightSum < 0.5 {
			continue
		}
		pairs = append(pairs, [2]float64{guess.Value, math.Min(1, *record.IntelligenceIndex/v.scale())})
	}
	if len(pairs) < minCalibrationPairs {
		return nil
	}
	meanX, meanY := 0.0, 0.0
	for _, pair := range pairs {
		meanX += pair[0]
		meanY += pair[1]
	}
	meanX /= float64(len(pairs))
	meanY /= float64(len(pairs))
	covariance, variance := 0.0, 0.0
	for _, pair := range pairs {
		covariance += (pair[0] - meanX) * (pair[1] - meanY)
		variance += (pair[0] - meanX) * (pair[0] - meanX)
	}
	if variance <= 0 || covariance <= 0 {
		return nil
	}
	slope := covariance / variance
	return &calibration{Slope: slope, Intercept: meanY - slope*meanX, Pairs: len(pairs)}
}
