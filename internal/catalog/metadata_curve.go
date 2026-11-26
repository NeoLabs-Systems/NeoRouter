package catalog

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// Heuristic weights when no benchmark index is available. Each signal is
// normalized to [0, 1]; missing signals drop out and lower the confidence.
const (
	weightPrice     = 0.45
	weightParams    = 0.3
	weightRecency   = 0.15
	weightReasoning = 0.1
)

var (
	priceRange   = [2]float64{0.1, 75}
	paramsRange  = [2]float64{1, 1000}
	recencyStart = dateMillis("2023-01-01")
)

// aaIndexScale is the fixed fallback scale before any benchmark data is
// loaded; the real scale makes the best model in the data 1.0.
const aaIndexScale = 75

// heuristicPrior is the neutral value missing signals count as.
const heuristicPrior = 0.4

// minCalibrationPairs is how many model/benchmark pairs a calibration needs.
const minCalibrationPairs = 12

// heuristicRange bounds a calibrated guess.
var heuristicRange = [2]float64{0.05, 0.9}

// Derived index: each benchmark score is translated to the index by a
// monotone mapping learned on the models that have both. A model's estimate is
// the average over its benchmarks, weighted by how well each benchmark's
// ranking agrees with the index (Spearman ρ²).
const (
	minDerivedBenchmarks = 2
	minRankAgreement     = 0.3
	minFitPairs          = 8
)

// variantShare is what a new variant of a benchmarked model typically keeps
// of its base model's score.
var variantShare = map[string]float64{"turbo": 0.9, "flash": 0.8, "air": 0.75, "lite": 0.7,
	"mini": 0.7, "small": 0.6, "nano": 0.55, "tiny": 0.5}

var (
	variantPattern = regexp.MustCompile(`(?i)[-_ .](turbo|flash|air|lite|mini|small|nano|tiny)`)
	effortSuffix   = regexp.MustCompile(`-(xhigh|high|medium|low|minimal|non-reasoning)$`)
	paramToken     = regexp.MustCompile(`^([ae]?\d+(\.\d+)?b|\d+x\d+b)$`)
	parenthesised  = regexp.MustCompile(`\([^)]*\)`)
	nonReasoning   = regexp.MustCompile(`non-reasoning`)
	tierSuffix     = regexp.MustCompile(`:(free|beta|thinking|extended|nitro)$`)
	alphaSuffix    = regexp.MustCompile(`(?i):[a-z]+$`)
)

var effortTokens = map[string]bool{"xhigh": true, "high": true, "medium": true, "low": true,
	"minimal": true, "default": true, "fallback": true, "nonreasoning": true}

var servingTokens = map[string]bool{"instruct": true, "it": true, "chat": true, "preview": true,
	"latest": true, "free": true, "exp": true, "versatile": true, "instant": true, "fp8": true, "bf16": true}

// benchmarkFit is one benchmark's learned mapping onto the intelligence scale.
type benchmarkFit struct {
	Curve  [][2]float64
	Weight float64
}

// benchmarkKeyIndex finds a benchmark record by its model name.
type benchmarkKeyIndex struct {
	exact map[string]*Fact
	// A nil value marks a size-less key two different models collapsed onto.
	loose map[string]*Fact
}

// openRouterIndex is OpenRouter's benchmark results, findable like the others.
type openRouterIndex struct {
	exact     map[string]*BenchmarkModel
	loose     map[string]*BenchmarkModel
	curves    map[string][][2]float64
	best      map[string]float64
	agreement map[string]float64
}

// calibration is the least-squares line from a raw heuristic guess to the
// benchmark score.
type calibration struct {
	Slope     float64
	Intercept float64
	Pairs     int
}

// heuristicGuess is a 0..1 capability guess and how much of it was measured.
type heuristicGuess struct {
	Value     float64
	WeightSum float64
}

// ranks returns the average rank of each value, ties sharing a rank.
func ranks(values []float64) []float64 {
	order := make([]int, len(values))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return values[order[a]] < values[order[b]] })
	result := make([]float64, len(values))
	for i := 0; i < len(order); {
		j := i
		for j+1 < len(order) && values[order[j+1]] == values[order[i]] {
			j++
		}
		for k := i; k <= j; k++ {
			result[order[k]] = float64(i+j) / 2
		}
		i = j + 1
	}
	return result
}

// spearman is the Spearman rank correlation of paired values.
func spearman(pairs [][2]float64) float64 {
	xs := make([]float64, len(pairs))
	ys := make([]float64, len(pairs))
	for i, pair := range pairs {
		xs[i] = pair[0]
		ys[i] = pair[1]
	}
	rx := ranks(xs)
	ry := ranks(ys)
	n := len(pairs)
	mean := float64(n-1) / 2
	sxy, sxx, syy := 0.0, 0.0, 0.0
	for i := 0; i < n; i++ {
		sxy += (rx[i] - mean) * (ry[i] - mean)
		sxx += (rx[i] - mean) * (rx[i] - mean)
		syy += (ry[i] - mean) * (ry[i] - mean)
	}
	if sxx == 0 || syy == 0 {
		return 0
	}
	return sxy / math.Sqrt(sxx*syy)
}

// isotonic is the pool-adjacent-violators fit: the best non-decreasing fit of
// y over sorted x.
func isotonic(pairs [][2]float64) [][2]float64 {
	sorted := append([][2]float64(nil), pairs...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a][0] < sorted[b][0] })
	type block struct {
		xs    []float64
		sum   float64
		count float64
	}
	var blocks []block
	for _, pair := range sorted {
		blocks = append(blocks, block{xs: []float64{pair[0]}, sum: pair[1], count: 1})
		for len(blocks) > 1 {
			previous := blocks[len(blocks)-2]
			last := blocks[len(blocks)-1]
			if previous.sum/previous.count <= last.sum/last.count {
				break
			}
			blocks = blocks[:len(blocks)-1]
			merged := blocks[len(blocks)-1]
			merged.xs = append(merged.xs, last.xs...)
			merged.sum += last.sum
			merged.count += last.count
			blocks[len(blocks)-1] = merged
		}
	}
	curve := make([][2]float64, 0, len(blocks))
	for _, item := range blocks {
		sum := 0.0
		for _, x := range item.xs {
			sum += x
		}
		curve = append(curve, [2]float64{sum / float64(len(item.xs)), item.sum / item.count})
	}
	return curve
}

// mapScore maps a benchmark score through a fitted curve. Below the known
// range it scales down in proportion instead of extrapolating a line into
// nonsense.
func mapScore(curve [][2]float64, x float64) float64 {
	if len(curve) == 0 {
		return x
	}
	x0, y0 := curve[0][0], curve[0][1]
	xn, yn := curve[len(curve)-1][0], curve[len(curve)-1][1]
	if x <= x0 {
		if x0 > 0 {
			return y0 * math.Max(0, x/x0)
		}
		return y0
	}
	if x >= xn {
		return yn
	}
	for i := 1; i < len(curve); i++ {
		xa, ya := curve[i-1][0], curve[i-1][1]
		xb, yb := curve[i][0], curve[i][1]
		if x <= xb {
			if xb == xa {
				return yb
			}
			return ya + ((x-xa)/(xb-xa))*(yb-ya)
		}
	}
	return yn
}

// deriveIndexes fits every benchmark onto the index and estimates the index of
// records that have benchmarks but none, returning the per-benchmark fits.
func deriveIndexes(records []*Fact) map[string]*benchmarkFit {
	indexed := make([]*Fact, 0, len(records))
	for _, record := range records {
		if record != nil && truthyFloat(record.IntelligenceIndex) && !record.IndexDerived && record.Benchmarks != nil {
			indexed = append(indexed, record)
		}
	}
	fits := map[string]*benchmarkFit{}
	for _, name := range aaBenchmarks {
		pairs := [][2]float64{}
		for _, record := range indexed {
			value, ok := record.Benchmarks[name]
			if !ok {
				continue
			}
			pairs = append(pairs, [2]float64{value, *record.IntelligenceIndex})
		}
		if len(pairs) < minFitPairs {
			continue
		}
		rho := spearman(pairs)
		if rho*rho >= minRankAgreement && rho > 0 {
			fits[name] = &benchmarkFit{Curve: isotonic(pairs), Weight: rho * rho}
		}
	}
	for _, record := range records {
		if record == nil {
			continue
		}
		// Estimates are worked out again: the index they derive from may have moved.
		if record.IndexDerived {
			record.IntelligenceIndex = nil
			record.IndexDerived = false
		}
		if truthyFloat(record.IntelligenceIndex) || record.Benchmarks == nil {
			continue
		}
		weight, total := 0.0, 0.0
		used := 0
		for _, name := range aaBenchmarks {
			fit, fitted := fits[name]
			if !fitted {
				continue
			}
			value, ok := record.Benchmarks[name]
			if !ok {
				continue
			}
			total += fit.Weight * mapScore(fit.Curve, value)
			weight += fit.Weight
			used++
		}
		if used >= minDerivedBenchmarks && weight > 0 {
			value := math.Max(1, total/weight)
			record.IntelligenceIndex = &value
			record.IndexDerived = true
		}
	}
	return fits
}

// medianOrNil is the middle of the given numbers, null when there are none.
func medianOrNil(values []float64) *float64 {
	sorted := make([]float64, 0, len(values))
	for _, value := range values {
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			sorted = append(sorted, value)
		}
	}
	if len(sorted) == 0 {
		return nil
	}
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return &sorted[middle]
	}
	value := (sorted[middle-1] + sorted[middle]) / 2
	return &value
}

// logScale maps a positive value onto [0, 1] on a log scale.
func logScale(value *float64, limits [2]float64) *float64 {
	if value == nil || *value <= 0 || math.IsNaN(*value) {
		return nil
	}
	scaled := (math.Log10(*value) - math.Log10(limits[0])) / (math.Log10(limits[1]) - math.Log10(limits[0]))
	clamped := math.Min(1, math.Max(0, scaled))
	return &clamped
}

// firstTruthyString returns the first record field that is set.
func firstTruthyString(records []*Fact, field func(*Fact) string) string {
	for _, record := range records {
		if value := field(record); value != "" {
			return value
		}
	}
	return ""
}

// firstTruthyBool returns the first record flag that is set.
func firstTruthyBool(records []*Fact, field func(*Fact) *bool) *bool {
	for _, record := range records {
		if value := field(record); value != nil {
			return value
		}
	}
	return nil
}

// truthyFloat is truthiness for a nullable number.
func truthyFloat(value *float64) bool { return value != nil && *value != 0 }

// sameFloat compares two nullable numbers.
func sameFloat(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func ptrFloat(value float64) *float64 { return &value }

// roundTo rounds a number to the given number of decimals, as toFixed does.
func roundTo(value float64, digits int) float64 {
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(value, 'f', digits, 64), 64)
	if err != nil {
		return value
	}
	return rounded
}

// dateMillis parses a date into milliseconds since the epoch, 0 when the text
// cannot be read.
func dateMillis(text string) float64 {
	millis, ok := parseDateMillis(text)
	if !ok {
		return 0
	}
	return millis
}

// parseDateMillis parses the date shapes the metadata sources publish.
func parseDateMillis(text string) (float64, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02", "2006-01"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return float64(parsed.UnixMilli()), true
		}
	}
	return 0, false
}
