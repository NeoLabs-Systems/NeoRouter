package catalog

import (
	"math"
	"strings"
	"time"
)

// Heuristic weights when no benchmark index is available. Each signal is
// normalized to [0, 1]; missing signals drop out and lower the confidence.
// benchmarkKeys reduces a benchmark or provider model name to matchable keys:
// both sides lose dates, reasoning-effort levels and serving suffixes. One key
// keeps the parameter sizes, a looser one drops them.
// familyMatch is a variant's benchmarked base model.
type familyMatch struct {
	Record *Fact
	Share  float64
	Base   string
	BaseID string
}

// familyBenchmark finds the benchmarked base model of a variant ("hy4-flash"
// next to "hy4"), which judges a cheap new variant better than its price does.
func (v *metadataView) familyBenchmark(canonical string, modelIDs []string) *familyMatch {
	candidates := append(append([]string{}, modelIDs...), canonical)
	for _, id := range candidates {
		base, share, ok := splitVariant(id)
		if !ok {
			continue
		}
		baseCanonical := CanonicalID(base)
		if baseCanonical == "" || baseCanonical == canonical {
			continue
		}
		baseLast := base
		if slash := strings.LastIndexByte(base, '/'); slash >= 0 {
			baseLast = base[slash+1:]
		}
		record := v.lookupBenchmark(baseCanonical, []string{base, baseLast})
		if record == nil || !truthyFloat(record.IntelligenceIndex) {
			continue
		}
		return &familyMatch{Record: record, Share: share, Base: baseCanonical, BaseID: base}
	}
	return nil
}

// variantMatchIn finds the variant word in a name: a separator, one of the
// variant words, then the end of the name or another separator. Go's regexp
// has no lookahead, so the trailing condition is checked here.
func variantMatchIn(last string) []int {
	for _, loc := range variantPattern.FindAllStringSubmatchIndex(last, -1) {
		end := loc[1]
		if end == len(last) || last[end] == '-' || last[end] == '_' || last[end] == ':' {
			return loc
		}
	}
	return nil
}

// variantMatch is what a variant id keeps of its base model's score.
type variantMatch struct {
	Base  string
	Share float64
}

// variantBase reports the base model of a variant id ("vendor/hy4-flash:free"
// becomes {base: "vendor/hy4", share: 0.8}), nil when the id names no variant.
func variantBase(id string) *variantMatch {
	base, share, ok := splitVariant(id)
	if !ok {
		return nil
	}
	return &variantMatch{Base: base, Share: share}
}

// splitVariant cuts the variant word out of a model id: the separator, the
// variant word and a trailing tier suffix do not belong to the base id.
func splitVariant(text string) (string, float64, bool) {
	prefix := 0
	if slash := strings.LastIndexByte(text, '/'); slash >= 0 {
		prefix = slash + 1
	}
	last := text[prefix:]
	loc := variantMatchIn(last)
	if loc == nil {
		return "", 0, false
	}
	share, known := variantShare[strings.ToLower(last[loc[2]:loc[3]])]
	if !known {
		return "", 0, false
	}
	base := text[:prefix+loc[0]] + text[prefix+loc[1]:]
	return alphaSuffix.ReplaceAllString(base, ""), share, true
}

// heuristicIntelligence scores price, size, recency and reasoning as a rough
// 0..1 guess of capability.
func (v *metadataView) heuristicIntelligence(records []*Fact, sampleModelIDs []string, aa *Fact) heuristicGuess {
	paidPrices := make([]float64, 0, len(records))
	for _, record := range records {
		if record.PriceOut != nil && *record.PriceOut > 0 {
			paidPrices = append(paidPrices, *record.PriceOut)
		}
	}
	marketPrice := medianOrNil(paidPrices)
	var params *float64
	if len(sampleModelIDs) > 0 {
		params = parameterCount(sampleModelIDs[0])
	}
	if params == nil {
		for _, id := range sampleModelIDs {
			if value := parameterCount(id); value != nil && *value != 0 {
				params = value
				break
			}
		}
	}
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

	signals := map[string]*float64{
		"price":     logScale(marketPrice, priceRange),
		"params":    logScale(params, paramsRange),
		"recency":   nil,
		"reasoning": nil,
	}
	if releaseDate != "" {
		if millis, ok := parseDateMillis(releaseDate); ok {
			span := float64(time.Now().UnixMilli()) - recencyStart
			if span != 0 {
				recency := math.Min(1, math.Max(0, (millis-recencyStart)/span))
				signals["recency"] = &recency
			}
		}
	}
	if reasoning != nil {
		flag := 0.0
		if *reasoning {
			flag = 1
		}
		signals["reasoning"] = &flag
	}
	weightSum, total := 0.0, 0.0
	weights := map[string]float64{"price": weightPrice, "params": weightParams, "recency": weightRecency, "reasoning": weightReasoning}
	for _, name := range []string{"price", "params", "recency", "reasoning"} {
		value := signals[name]
		if value == nil {
			continue
		}
		weightSum += weights[name]
		total += weights[name] * *value
	}
	// Missing signals count as the neutral prior, so a model known only to be
	// new and "reasoning" cannot outrank well-measured flagships.
	return heuristicGuess{Value: total + heuristicPrior*(1-weightSum), WeightSum: weightSum}
}

// kindParams are the parameter ranges (billions) from small to large within
// each non-chat kind.
var kindParams = map[string][2]float64{"embedding": {0.1, 10}, "moderation": {0.02, 20},
	"transcription": {0.05, 5}, "speech": {0.05, 5}, "image": {1, 30}, "video": {1, 30}}

// kindHeuristic scores a non-chat model by size and age within its kind.
func kindHeuristic(capability string, params *float64, releaseDate string) heuristicGuess {
	limits, known := kindParams[capability]
	if !known {
		limits = paramsRange
	}
	signals := [][2]*float64{{ptrFloat(0.6), logScale(params, limits)}, {ptrFloat(0.4), nil}}
	if releaseDate != "" {
		if millis, ok := parseDateMillis(releaseDate); ok {
			span := float64(time.Now().UnixMilli()) - recencyStart
			if span != 0 {
				recency := math.Min(1, math.Max(0, (millis-recencyStart)/span))
				signals[1][1] = &recency
			}
		}
	}
	weightSum, total := 0.0, 0.0
	for _, signal := range signals {
		if signal[1] == nil {
			continue
		}
		weightSum += *signal[0]
		total += *signal[0] * *signal[1]
	}
	return heuristicGuess{Value: math.Min(0.85, math.Max(0.1, total+0.5*(1-weightSum))), WeightSum: weightSum}
}

// searchQuality is a curated ranking of search APIs from agent search
// evaluations: no public index compares them.
var searchQuality = map[string]float64{
	"exa-search": 0.85, "parallel-search": 0.85, "tavily-search": 0.8, "linkup-standard": 0.8,
	"serper-google": 0.75, "brave-web-search": 0.75, "firecrawl-search": 0.7, "jina-search": 0.65,
	"searxng": 0.6,
}
