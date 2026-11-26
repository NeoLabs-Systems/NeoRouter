package catalog

import (
	"encoding/json"
)

// aaSetting is the global setting holding the Artificial Analysis API key.

// ComputeCanonical combines every known source into one canonical metadata
// record. capability defaults to "chat".
func (m *Metadata) ComputeCanonical(canonical string, sampleModelIDs []string, capability string) Record {
	var record Record
	m.withView(func(v *metadataView) { record = m.computeCanonical(v, canonical, sampleModelIDs, capability) })
	return record
}

// computeCanonical is ComputeCanonical for an already loaded view.
func (m *Metadata) computeCanonical(v *metadataView, canonical string, sampleModelIDs []string, capability string) Record {
	if capability == "" {
		capability = "chat"
	}
	records := v.idx.byCanonical[canonical]
	// Media models are scored by their arena ELO, never by the chat index.
	var arena *mediaArenaScore
	if capability != "chat" {
		arena = lookupMedia(v.idx.media, capability, sampleModelIDs)
	}
	var aa *Fact
	var orSkills map[string]float64
	if capability == "chat" {
		aa = v.lookupBenchmark(canonical, sampleModelIDs)
		orSkills = v.openRouterSkills(v.openRouterMatch(canonical, sampleModelIDs))
	}
	paidPrices := make([]float64, 0, len(records))
	for _, record := range records {
		if record.PriceOut != nil && *record.PriceOut > 0 {
			paidPrices = append(paidPrices, *record.PriceOut)
		}
	}
	marketPrice := medianOrNil(paidPrices)
	params := sampleParameterCount(sampleModelIDs)
	releaseDate := ""
	if aa != nil {
		releaseDate = aa.ReleaseDate
	}
	if releaseDate == "" {
		releaseDate = firstTruthyString(records, func(f *Fact) string { return f.ReleaseDate })
	}
	if releaseDate == "" {
		releaseDate = firstTruthyString(records, func(f *Fact) string { return f.Created })
	}
	reasoning := firstTruthyBool(records, func(f *Fact) *bool { return f.Reasoning })

	intelligence := 0.0
	source := ""
	confidence := 0.0
	switch {
	case arena != nil:
		intelligence = arena.Score
		source = "artificial_analysis_arena"
		confidence = 0.85
	case capability == "embedding" && hasMTEBScore(v.st.MTEB, sampleModelIDs):
		// MTEB, the embedding benchmark (average over a fixed set of English tasks).
		score := firstMTEBScore(v.st.MTEB, sampleModelIDs)
		intelligence = mtebIntelligence(*score)
		source = "mteb"
		confidence = 0.8
	case capability == "search" && len(sampleModelIDs) > 0 && searchQuality[sampleModelIDs[0]] > 0:
		// No public index compares search APIs; a curated ranking from agent
		// search evaluations (accuracy of answers built on the results).
		intelligence = searchQuality[sampleModelIDs[0]]
		source = "curated"
		confidence = 0.5
	case capability != "chat":
		// Without arena data, size and age within the kind are the best guide;
		// the chat calibration means nothing for embedders or guards.
		guess := kindHeuristic(capability, params, releaseDate)
		intelligence = guess.Value
		if guess.WeightSum > 0 {
			source = "heuristic"
		} else {
			source = "default"
		}
		confidence = mathMin(0.5, guess.WeightSum*0.5)
	case aa != nil && truthyFloat(aa.IntelligenceIndex):
		measured := reconcileWithSkills(mathMin(1, *aa.IntelligenceIndex/v.scale()), skillsOf(v, m.SkillsFromBenchmarks, aa))
		// Its overall index covers far more ground than OpenRouter's two benchmarks.
		intelligence = measured
		if overall := v.openRouterOverall(orSkills); overall != nil {
			intelligence = aaShare*measured + (1-aaShare)*(*overall)
		}
		if aa.IndexDerived {
			source = "artificial_analysis_benchmarks"
		} else {
			source = "artificial_analysis"
		}
		if aa.IndexDerived {
			confidence = 0.8
		} else {
			confidence = 0.9
		}
	case orSkills != nil:
		if overall := v.openRouterOverall(orSkills); overall != nil {
			intelligence = *overall
		} else {
			intelligence = firstMapValue(orSkills)
		}
		source = "openrouter_benchmarks"
		confidence = 0.8
	default:
		guess := v.heuristicIntelligence(records, sampleModelIDs, aa)
		cal := v.idx.calibration
		if cal != nil {
			intelligence = mathMin(heuristicRange[1], mathMax(heuristicRange[0], cal.Slope*guess.Value+cal.Intercept))
			if guess.WeightSum > 0 {
				source = "calibrated"
			} else {
				source = "default"
			}
		} else {
			intelligence = guess.Value
			if guess.WeightSum > 0 {
				source = "heuristic"
			} else {
				source = "default"
			}
		}
		confidence = mathMin(0.7, guess.WeightSum*0.7)
		// Only lifts a guess: the family score fixes cheap new variants that the
		// price-led guess undersells, it never pushes a model further down.
		if family := v.familyBenchmark(canonical, sampleModelIDs); family != nil {
			base := m.computeCanonical(v, family.Base, []string{family.BaseID}, "chat").Intelligence
			if base*family.Share > intelligence {
				intelligence = base * family.Share
				source = "family"
				confidence = 0.6
			}
		}
	}

	skills := mergeSkills(skillsOf(v, m.SkillsFromBenchmarks, aa), orSkills)
	record := Record{
		CanonicalID:            canonical,
		Intelligence:           roundTo(intelligence, 4),
		IntelligenceSource:     source,
		IntelligenceConfidence: roundTo(confidence, 3),
		SpeedTps:               nilIfMissing(aa, func(f *Fact) *float64 { return f.OutputTps }),
		ContextWindow:          firstTruthyInt(records, func(f *Fact) *int64 { return f.ContextWindow }),
		MaxOutput:              firstTruthyInt(records, func(f *Fact) *int64 { return f.MaxOutput }),
		PriceIn:                medianOrNil(positivePrices(records, func(f *Fact) *float64 { return f.PriceIn })),
		PriceOut:               marketPrice,
		SupportsTools:          toFlag(firstTruthyBool(records, func(f *Fact) *bool { return f.Tools })),
		SupportsVision:         toFlag(firstTruthyBool(records, func(f *Fact) *bool { return f.Vision })),
		SupportsReasoning:      toFlag(reasoning),
		ParamsB:                params,
	}
	release := releaseDate
	if release == "" && arena != nil {
		release = arena.ReleaseDate
	}
	if release != "" {
		record.ReleaseDate = &release
	}
	record.SourcesJSON = sourcesJSON(records, aa, arena)
	if skills != nil {
		if encoded, err := json.Marshal(skills); err == nil {
			text := string(encoded)
			record.SkillsJSON = &text
		}
	}
	return record
}

// skillsOf turns a record's benchmark scores into per-skill scores on the
// intelligence scale, using the same fit that derives the index.
func skillsOf(v *metadataView, fromBenchmarks func(map[string]float64) map[string]float64, record *Fact) map[string]float64 {
	if record == nil || record.Benchmarks == nil || fromBenchmarks == nil {
		return nil
	}
	mapped := map[string]float64{}
	for _, name := range aaBenchmarks {
		fit, fitted := v.idx.fits[name]
		value, present := record.Benchmarks[name]
		if !fitted || !present {
			continue
		}
		mapped[name] = mapScore(fit.Curve, value) / v.scale()
	}
	skills := fromBenchmarks(mapped)
	if len(skills) == 0 {
		return nil
	}
	return skills
}

// sourcesJSON lists the sources that spoke for a model, in a stable order.
func sourcesJSON(records []*Fact, aa *Fact, arena *mediaArenaScore) string {
	sources := make([]string, 0, len(records)+2)
	seen := map[string]bool{}
	add := func(source string) {
		if source == "" || seen[source] {
			return
		}
		seen[source] = true
		sources = append(sources, source)
	}
	for _, record := range records {
		add(record.Source)
	}
	if aa != nil {
		add("artificial_analysis")
	}
	if arena != nil {
		add("artificial_analysis_arena")
	}
	encoded, err := json.Marshal(sources)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// sampleParameterCount is the parameter count in billions from the model ids,
// preferring the first id that names one.
func sampleParameterCount(sampleModelIDs []string) *float64 {
	if len(sampleModelIDs) > 0 {
		if value := parameterCount(sampleModelIDs[0]); value != nil {
			return value
		}
	}
	for _, id := range sampleModelIDs {
		if value := parameterCount(id); value != nil && *value != 0 {
			return value
		}
	}
	return nil
}

// hasMTEBScore reports whether any of the sample models has an embedding score.
func hasMTEBScore(scores map[string]MTEBScore, sampleModelIDs []string) bool {
	for _, id := range sampleModelIDs {
		if score, ok := scores[id]; ok && score.Score != nil && *score.Score != 0 {
			return true
		}
	}
	return false
}

// firstMTEBScore returns the first sample model's embedding score.
func firstMTEBScore(scores map[string]MTEBScore, sampleModelIDs []string) *float64 {
	for _, id := range sampleModelIDs {
		score, ok := scores[id]
		if !ok || score.Score == nil || *score.Score == 0 {
			continue
		}
		return score.Score
	}
	return nil
}

// positivePrices collects the positive values of one price field.
func positivePrices(records []*Fact, field func(*Fact) *float64) []float64 {
	values := make([]float64, 0, len(records))
	for _, record := range records {
		if value := field(record); value != nil && *value > 0 {
			values = append(values, *value)
		}
	}
	return values
}

// firstTruthyInt returns the first record number that is set.
func firstTruthyInt(records []*Fact, field func(*Fact) *int64) *int64 {
	for _, record := range records {
		if value := field(record); value != nil {
			return value
		}
	}
	return nil
}

// toFlag stores a tri-state flag as the metadata columns do: 1, 0 or NULL.
func toFlag(value *bool) *int64 {
	if value == nil {
		return nil
	}
	flag := int64(0)
	if *value {
		flag = 1
	}
	return &flag
}

// nilIfMissing returns the field of an optional record, or nil.
func nilIfMissing(record *Fact, field func(*Fact) *float64) *float64 {
	if record == nil {
		return nil
	}
	return field(record)
}

// nilIfNil returns the value, nil when the pointer is nil, for SQL binding.
func nilIfNil[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func firstMapValue(values map[string]float64) float64 {
	for _, value := range values {
		return value
	}
	return 0
}

func floatOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func max64(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func mathMin(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func mathMax(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
