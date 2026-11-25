package routing

import (
	"math"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// Feature scaling ranges (log scale). Centralized so they can be tuned.
var (
	tpsRange     = [2]float64{10, 1500}
	ttftRange    = [2]float64{150, 15000}
	priceRange   = [2]float64{0.05, 60}
	latencyRange = [2]float64{200, 120_000}
)

const (
	defaultIntelligence = 0.4
	// A score with little behind it counts for less: below this confidence it
	// is pulled towards what an unknown model is typically worth, so a model
	// nobody has measured cannot rank above ones that have been.
	trustedConfidence     = 0.5
	unknownIntelligence   = 0.25
	unknownPriceCost      = 0.3
	reliabilityPriorAlpha = 2
	reliabilityPriorBeta  = 1
	penaltyImpact         = 0.6
	defaultCapacity       = 4
	// FeatureFloor is the floor for each feature, so one zero cannot wipe out a
	// geometric mean.
	FeatureFloor = 0.02
)

// unitPriceRanges are cheap to expensive, in USD per unit.
var unitPriceRanges = map[string][2]float64{
	"image":  {0.002, 0.2},
	"second": {0.01, 0.5},
	"minute": {0.001, 0.1},
	"mchar":  {1, 100},
	"search": {0.0005, 0.03},
	"call":   {0.00005, 0.01},
}

// TrustedIntelligence is a model's intelligence, pulled towards what an
// unknown model is worth when the measurement behind it is weak.
func TrustedIntelligence(entry *Entry) float64 {
	value := defaultIntelligence
	if entry.Intelligence != nil {
		value = *entry.Intelligence
	}
	confidence := entry.IntelligenceConfidence
	if entry.IntelligenceSource == "manual" || confidence == nil || *confidence >= trustedConfidence {
		return value
	}
	share := maxf(0, *confidence) / trustedConfidence
	return share*value + (1-share)*unknownIntelligence
}

func (s *Service) trustedIntelligence(entry *Entry) float64 { return TrustedIntelligence(entry) }

// sampleGamma is the Marsaglia–Tsang gamma sampler, used for Beta sampling
// below.
func sampleGamma(shape float64, random func() float64) float64 {
	if shape < 1 {
		return sampleGamma(shape+1, random) * math.Pow(random(), 1/shape)
	}
	d := shape - 1.0/3
	c := 1 / math.Sqrt(9*d)
	for {
		var x, v float64
		for {
			u1 := random()
			if u1 == 0 {
				u1 = 1e-12
			}
			u2 := random()
			x = math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
			v = 1 + c*x
			if v > 0 {
				break
			}
		}
		v = v * v * v
		u := random()
		if u < 1-0.0331*x*x*x*x || math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}

// sampleBeta draws from a Beta distribution.
func sampleBeta(alpha, beta float64, random func() float64) float64 {
	if random == nil {
		random = randFloat64
	}
	x := sampleGamma(alpha, random)
	y := sampleGamma(beta, random)
	return x / (x + y)
}

// Reliability is Thompson sampling: models with little evidence get a wide
// distribution and are explored now and then; proven models get a tight, high
// estimate.
func Reliability(snapshot StatsRow, sample bool, random func() float64) float64 {
	alpha := float64(reliabilityPriorAlpha) + snapshot.Successes
	beta := float64(reliabilityPriorBeta) + snapshot.Failures
	if sample {
		return sampleBeta(alpha, beta, random)
	}
	return alpha / (alpha + beta)
}

func (s *Service) reliabilityOf(snapshot StatsRow, sample bool) float64 {
	if !sample {
		return Reliability(snapshot, false, nil)
	}
	return Reliability(snapshot, true, s.opts.Random)
}

// SpeedOf is the speed feature of a model from what was measured.
func SpeedOf(entry *Entry, snapshot StatsRow) float64 {
	var tps *float64
	if snapshot.TPS != nil {
		tps = snapshot.TPS
	} else if entry.SpeedPriorTps != nil {
		tps = entry.SpeedPriorTps
	}
	var tpsScore *float64
	if tps != nil {
		if scaled, ok := logScale(*tps, tpsRange[0], tpsRange[1]); ok {
			tpsScore = &scaled
		}
	}
	var ttftScore *float64
	if snapshot.TTFTMs != nil && *snapshot.TTFTMs != 0 {
		scaled, _ := logScale(*snapshot.TTFTMs, ttftRange[0], ttftRange[1])
		value := 1 - scaled
		ttftScore = &value
	}
	if tpsScore == nil && ttftScore == nil {
		// Media calls (embeddings, images, audio) have no token stream: total
		// latency it is.
		if snapshot.LatencyMs != nil && *snapshot.LatencyMs != 0 {
			scaled, ok := logScale(*snapshot.LatencyMs, latencyRange[0], latencyRange[1])
			if !ok {
				scaled = 0
			}
			return 1 - scaled
		}
		return 0.5
	}
	if tpsScore == nil {
		return *ttftScore
	}
	if ttftScore == nil {
		return *tpsScore
	}
	return 0.6**tpsScore + 0.4**ttftScore
}

// SpeedScore is a model's speed feature right now (the models page shows it).
func (s *Service) SpeedScore(entry *Entry, now time.Time) float64 {
	return SpeedOf(entry, s.stats.snapshot(entry.Ref, now))
}

// BlendedPrice is a model's input and output price as one number (input
// counts three times), nil when unknown.
func BlendedPrice(entry *Entry) *float64 {
	if entry.IsFree || entry.IsPlan {
		return float64Ptr(0)
	}
	if entry.PriceIn == nil || entry.PriceOut == nil {
		return nil
	}
	return float64Ptr((*entry.PriceIn*3 + *entry.PriceOut) / 4)
}

// costOf is the price feature: 1 for free, less as a model gets pricier, and a
// plan model priced by what its allowance costs.
func (s *Service) costOf(entry *Entry, now time.Time) float64 {
	// Plan models spend an allowance instead of money (plans.go).
	if entry.IsPlan {
		return 1 - s.planAllowanceCost(entry, now)
	}
	// Media models priced per image, second, minute or million characters.
	if !entry.IsFree && entry.UnitPrice != nil {
		if unitRange, ok := unitPriceRanges[entry.PriceUnit]; ok {
			if *entry.UnitPrice == 0 {
				return 1
			}
			scaled, ok := logScale(*entry.UnitPrice, unitRange[0], unitRange[1])
			if !ok {
				scaled = 0
			}
			return 1 - scaled
		}
	}
	price := BlendedPrice(entry)
	if price == nil {
		return unknownPriceCost
	}
	if *price == 0 {
		return 1
	}
	scaled, ok := logScale(*price, priceRange[0], priceRange[1])
	if !ok {
		scaled = 0
	}
	return 1 - scaled
}

// CostScore is a model's price feature right now.
func (s *Service) CostScore(entry *Entry, now time.Time) float64 { return s.costOf(entry, now) }

// Difficulty-aware weights (when the profile is adaptive): hard requests lean
// towards intelligence, easy ones ("hi", a quick translation) towards price,
// since an easy prompt is not worth paying more for. Between the two bands the
// profile's weights stand.
const (
	easyBelow    = 0.3
	hardAbove    = 0.5
	maxEasyShift = 0.5
)

// AdaptWeights shifts a profile's weights with the request's difficulty.
func AdaptWeights(weights Weights, difficulty *float64, adaptive bool) Weights {
	if !adaptive || difficulty == nil {
		return weights
	}
	value := *difficulty
	if value < easyBelow {
		moved := weights.Intelligence * maxEasyShift * ((easyBelow - value) / easyBelow)
		return Weights{
			Intelligence: weights.Intelligence - moved,
			Speed:        weights.Speed,
			Reliability:  weights.Reliability,
			Cost:         weights.Cost + moved,
		}
	}
	if value <= hardAbove {
		return weights
	}
	shift := 0.3 * (value - hardAbove)
	others := weights.Speed + weights.Reliability + weights.Cost
	if others == 0 {
		return weights
	}
	scale := maxf(0, 1-shift/others)
	return Weights{
		Intelligence: weights.Intelligence + shift,
		Speed:        weights.Speed * scale,
		Reliability:  weights.Reliability * scale,
		Cost:         weights.Cost * scale,
	}
}

// scoreOptions is what scoring.score takes.
type scoreOptions struct {
	Weights     Weights
	QuotaFactor float64
	Random      func() float64
	Sample      bool
	Now         time.Time
	Task        *task.Result
	Need        *float64
}

// score is scoring.score: one model's score, with the features and factors that
// went into it.
func (s *Service) score(entry *Entry, opts scoreOptions) ScoreDetails {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if opts.QuotaFactor == 0 {
		opts.QuotaFactor = 1
	}
	random := opts.Random
	if random == nil {
		random = s.opts.Random
	}
	snapshot := s.stats.snapshot(entry.Ref, now)
	learnedReliability := Reliability(snapshot, opts.Sample, random)
	quality := 1.0
	if opts.Task != nil {
		learnedReliability, quality = s.experience.Adjust(entry.Ref, opts.Task.Primary, learnedReliability, now)
	}
	totalWeight := opts.Weights.total()
	var capability float64
	if opts.Task != nil {
		capability = minf(1, task.TaskIntelligence(TrustedIntelligence(entry), entry.Skills, opts.Task.SkillWeights())*quality)
	} else {
		capability = minf(1, task.TaskIntelligence(TrustedIntelligence(entry), entry.Skills, nil)*quality)
	}
	headroom := 0.0
	if totalWeight != 0 {
		headroom = opts.Weights.Intelligence / totalWeight
	}
	features := Features{
		Intelligence: task.EnoughIntelligence(capability, opts.Need, headroom),
		Speed:        SpeedOf(entry, snapshot),
		Reliability:  learnedReliability,
		Cost:         s.costOf(entry, now),
	}
	base := 0.0
	if totalWeight != 0 {
		base = (opts.Weights.Intelligence*features.Intelligence + opts.Weights.Speed*features.Speed +
			opts.Weights.Reliability*features.Reliability + opts.Weights.Cost*features.Cost) / totalWeight
	}
	penaltyFactor := 1 - penaltyImpact*minf(snapshot.Penalty, maxPenalty)/maxPenalty
	load := s.load.inflight(entry)
	// A known concurrency limit is the account's real capacity.
	accountCapacity := float64(defaultCapacity * 4)
	if limit := s.conc.limitFor(entry, now); limit != nil {
		accountCapacity = float64(limit.Limit)
	}
	loadFactor := 1 / (1 + float64(load.Model)/defaultCapacity + float64(load.Account)/accountCapacity)
	return ScoreDetails{
		Score:      base * opts.QuotaFactor * penaltyFactor * loadFactor,
		Base:       base,
		Capability: capability,
		Features:   features,
		Factors: Factors{
			Quota:   opts.QuotaFactor,
			Penalty: penaltyFactor,
			Load:    loadFactor,
		},
	}
}

// WorthFeatures is what a model is worth apart from its price and from what is
// going on right now.
type WorthFeatures struct {
	Intelligence float64
	Speed        float64
	Reliability  float64
}

// WorthParts is the per-axis part of worth(): benchmarks and ratings, measured
// speed, and the model's track record for the task.
func (s *Service) WorthParts(entry *Entry, taskPrimary string, taskSkills map[string]float64, now time.Time) WorthFeatures {
	snapshot := s.stats.snapshot(entry.Ref, now)
	reliability, quality := s.experience.Adjust(entry.Ref, taskPrimary, Reliability(snapshot, false, nil), now)
	return WorthFeatures{
		Intelligence: minf(1, task.TaskIntelligence(TrustedIntelligence(entry), entry.Skills, taskSkills)*quality),
		Speed:        SpeedOf(entry, snapshot),
		Reliability:  reliability,
	}
}

// CombineWorth is a weighted geometric mean of the worth features, so a model
// has to be decent on every axis.
func CombineWorth(features WorthFeatures, weights Weights) float64 {
	total := weights.Intelligence + weights.Speed + weights.Reliability
	if total == 0 {
		return 0
	}
	return math.Exp((weights.Intelligence/total)*math.Log(maxf(features.Intelligence, FeatureFloor)) +
		(weights.Speed/total)*math.Log(maxf(features.Speed, FeatureFloor)) +
		(weights.Reliability/total)*math.Log(maxf(features.Reliability, FeatureFloor)))
}

// Worth is what a model is worth for a kind of task apart from its price and
// from what is going on right now.
func (s *Service) Worth(entry *Entry, weights Weights, taskPrimary string, taskSkills map[string]float64, now time.Time) (float64, WorthFeatures) {
	features := s.WorthParts(entry, taskPrimary, taskSkills, now)
	return CombineWorth(features, weights), features
}
