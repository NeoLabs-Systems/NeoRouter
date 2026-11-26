package routing

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// RankOptions tunes Rank.
type RankOptions struct {
	IncludeRejected bool
	Sample          bool
	Now             time.Time
}

// Rank scores every entry the plan may use, highest first. Rejected entries are
// appended (only with IncludeRejected) with their reason.
func (p *RoutingPlan) Rank(opts RankOptions) []*Candidate {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	ctx := p.Context()
	slotPriority := false
	if p.parsed.Auto {
		for _, entry := range p.Entries {
			if entry.IsFlatRate && (p.Group == nil || p.svc.Contains(p.Group, entry)) {
				slotPriority = true
				break
			}
		}
	}
	var ranked []*Candidate
	for _, entry := range p.Entries {
		if p.tried[entry.Ref] {
			continue
		}
		if reason := p.svc.Rejection(entry, ctx, now); reason != "" {
			if opts.IncludeRejected {
				ranked = append(ranked, &Candidate{Entry: entry, Rejected: reason})
			}
			continue
		}
		quotaState := p.svc.quotaEvaluate(entry, p.InputTokens, now)
		details := p.svc.score(entry, scoreOptions{
			Weights:     p.Weights,
			QuotaFactor: quotaState.Factor,
			Random:      p.random,
			Sample:      opts.Sample,
			Now:         now,
			Task:        p.Task,
			Need:        p.Need,
		})
		ranked = append(ranked, &Candidate{Entry: entry, Details: &details, Quota: quotaState})
	}
	// Absolute intelligence, speed and cost estimates sit close together for
	// most models. Blending in each candidate's position within this field lets
	// a profile's preference actually separate the candidates.
	var scored []*Candidate
	for _, item := range ranked {
		if item.Rejected == "" {
			scored = append(scored, item)
		}
	}
	span := map[string][2]float64{}
	for _, feature := range relativeFeatures {
		min, max := math.Inf(1), math.Inf(-1)
		for _, item := range scored {
			value := item.Details.Features.factor(feature)
			min = math.Min(min, value)
			max = math.Max(max, value)
		}
		if math.IsInf(min, 1) {
			min, max = 0, 0
		}
		span[feature] = [2]float64{min, max}
	}
	totalWeight := p.Weights.total()
	if totalWeight == 0 {
		totalWeight = 1
	}
	qualityWeight := totalWeight - p.Weights.Cost
	if qualityWeight == 0 {
		qualityWeight = 1
	}
	qualityProfile := p.Weights.Intelligence/totalWeight >= qualityProfileShare
	bestIntelligence := 0.0
	for _, item := range scored {
		bounds := span["intelligence"]
		value := item.Details.Features.Intelligence
		relative := 0.5
		if bounds[1] > bounds[0] {
			relative = (value - bounds[0]) / (bounds[1] - bounds[0])
		}
		bestIntelligence = math.Max(bestIntelligence, (1-relativeBlend)*value+relativeBlend*relative)
	}
	for _, item := range scored {
		features := item.Details.Features
		for _, feature := range relativeFeatures {
			bounds := span[feature]
			value := features.factor(feature)
			relative := 0.5
			if bounds[1] > bounds[0] {
				relative = (value - bounds[0]) / (bounds[1] - bounds[0])
			}
			features.setFactor(feature, (1-relativeBlend)*value+relativeBlend*relative)
		}
		// Weighted geometric mean: a model has to be decent on every axis that
		// matters; one very weak axis cannot be made up by the others.
		baseLog := 0.0
		for _, feature := range []string{"intelligence", "speed", "reliability", "cost"} {
			weight := p.Weights.get(feature) / totalWeight
			baseLog += weight * math.Log(math.Max(features.factor(feature), FeatureFloor))
		}
		base := math.Exp(baseLog)
		// What the model is worth apart from its price, from its own (not
		// field-relative) values, so small differences stay small. Reliability as
		// its average, not this request's random sample.
		own := item.Details.Features
		snapshot := p.svc.stats.snapshot(item.Entry.Ref, now)
		own.Reliability = Reliability(snapshot, false, nil)
		qualityLog := 0.0
		for _, feature := range []string{"intelligence", "speed", "reliability"} {
			weight := p.Weights.get(feature) / qualityWeight
			qualityLog += weight * math.Log(math.Max(own.factor(feature), FeatureFloor))
		}
		quality := math.Exp(qualityLog)
		item.Quality = quality
		item.OwnCost = own.Cost
		value := base * item.Details.Factors.Quota * item.Details.Factors.Penalty * item.Details.Factors.Load
		if qualityProfile && features.Intelligence < bestIntelligence-qualityGap {
			value *= qualityGateFactor
		}
		value *= p.svc.planFactor(item.Entry, now)
		if p.svc.isPeerEntry(item.Entry) {
			value *= peerFactor
		}
		// Far too weak for this task: taken only when nothing better can serve.
		if p.Task != nil && item.Details.Capability < weakBelow(p.Need) {
			value *= weakForTaskFactor
		}
		if p.parsed.Auto {
			item.Preference = groupPreference(p.Group, item.Entry, p.AffinityRef)
		}
		// A custom style's preference between free, subscription and paid models.
		item.StyleTier = 0
		if p.Profile.Constraints.Kinds != nil {
			item.StyleTier = kindTierOf(p.Profile.Constraints.Kinds, item.Entry)
		}
		item.Tier = item.StyleTier
		// Explicit style kind tiers still win; within each tier, use paid-for
		// slots and expiring allowances before consuming other sources.
		if slotPriority {
			item.Tier = item.Tier*SourceTiers + p.svc.slotSourceTier(item.Entry, now)
		}
		// A partner's request uses shared subscription models only when no free
		// one can serve, and paid ones last.
		if p.Key.PeerLinkID != "" {
			item.Tier += partnerKindTier[ModelKind(item.Entry)]
		}
		if (p.Needs.Tools && item.Entry.Tools == nil) || (p.Needs.JSON && item.Entry.JSON != nil && !*item.Entry.JSON) {
			value *= unknownCapabilityFactor
		}
		// Far more than this request's fair share of the budget: last resort only.
		if p.pacing != nil && !item.Entry.IsFree && !item.Entry.IsPlan {
			var typical *float64
			if p.estimateCost != nil {
				typical = p.estimateCost(item.Entry)
			} else {
				typical = p.svc.estimateMaxCost(item.Entry, p.InputTokens, minInt(MaxOutputFor(p.Request, item.Entry), typicalOutputTokens))
			}
			if typical != nil && *typical > p.pacing.PerRequestCap {
				value *= overPaceFactor
			}
		}
		item.Score = value
		item.Details.Base = base
		item.Details.Features = features
	}
	kinds := p.Profile.Constraints.Kinds
	p.svc.spreadAcrossAccounts(scored, now)
	// Account spreading normalizes sibling scores; apply pacing afterward so an
	// account with little sustainable allowance keeps its penalty.
	for _, item := range scored {
		if p.smoothing {
			factor := p.svc.SmoothingFactor(item.Entry, SmoothingContext{UserID: p.UserID, InputTokens: p.InputTokens, MaxOutput: p.Request.MaxTokens, Priority: p.Key.Priority, Now: now})
			item.Score *= factor
			item.Details.Factors.Pacing = &factor
		}
	}
	p.svc.preferCheaperEquals(scored, func(entry *Entry) float64 {
		if kinds == nil {
			return 1
		}
		return kindWeightOf(kinds, entry)
	})
	for _, item := range scored {
		if p.AffinityRef != nil && item.Entry.Ref == *p.AffinityRef {
			bonus := p.svc.CacheBonus(item.Entry, p.cacheRates[item.Entry.Ref], p.InputTokens)
			item.Score += bonus
			item.Details.Factors.CacheAffinity = &bonus
			if bonus > 0 {
				item.PricierEqual = false
			}
		}
	}
	// The model (or account) the client asked for comes first.
	for _, item := range scored {
		if p.PrimaryRefs[item.Entry.Ref] {
			item.Score += 10
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return scoreKey(ranked[i]) > scoreKey(ranked[j]) })
	return ranked
}

func scoreKey(item *Candidate) float64 {
	if item.Rejected != "" || item.Details == nil {
		return -1
	}
	return item.Score
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// weakBelow is how far below what a task needs counts as "far too weak".
func weakBelow(need *float64) float64 {
	value := 0.0
	if need != nil {
		value = *need
	}
	return maxf(0.08, minf(0.12, value*0.3))
}

// kindWeightOf is kindWeight: relative to the kind the style likes most, so the
// favourite keeps its score.
func kindWeightOf(kinds *KindWeights, entry *Entry) float64 {
	top := maxf(kinds.Free, maxf(kinds.Plan, kinds.Paid))
	if top == 0 {
		top = 1
	}
	return kinds.get(ModelKind(entry)) / top
}

// kindTierOf is kindTier: the kinds a style weighs most are tier 0, a lower
// weight a later tier. A later tier is only drawn from once no model of an
// earlier one can take the request.
func kindTierOf(kinds *KindWeights, entry *Entry) int {
	var levels []float64
	for _, weight := range []float64{kinds.Free, kinds.Plan, kinds.Paid} {
		if weight <= 0 {
			continue
		}
		seen := false
		for _, level := range levels {
			if level == weight {
				seen = true
				break
			}
		}
		if !seen {
			levels = append(levels, weight)
		}
	}
	sort.SliceStable(levels, func(i, j int) bool { return levels[i] > levels[j] })
	kind := ModelKind(entry)
	for index, level := range levels {
		if level == kinds.get(kind) {
			return index
		}
	}
	return 0
}

// freeCapacity is how much room an account has for one more request.
func (s *Service) freeCapacity(entry *Entry, now time.Time) float64 {
	inflight := s.load.inflight(entry).Account
	if limit := s.conc.limitFor(entry, now); limit != nil {
		return maxf(0, 1-float64(inflight)/float64(limit.Limit))
	}
	return 1 / (1 + float64(inflight)/accountSoftCapacity)
}

// spreadAcrossAccounts keeps copies of one model on several accounts from
// hammering the same one: the score is normalized against the healthiest copy,
// weighted by allowance left and free capacity.
func (s *Service) spreadAcrossAccounts(scored []*Candidate, now time.Time) {
	var order []string
	groups := map[string][]*Candidate{}
	for _, item := range scored {
		price := "null"
		if blended := BlendedPrice(item.Entry); blended != nil {
			price = fmt.Sprintf("%v", *blended)
		}
		copyKey := item.Entry.CanonicalID + "|" + ModelKind(item.Entry) + "|" + price
		if _, ok := groups[copyKey]; !ok {
			order = append(order, copyKey)
		}
		groups[copyKey] = append(groups[copyKey], item)
	}
	for _, key := range order {
		group := groups[key]
		accounts := map[string]bool{}
		for _, item := range group {
			accounts[item.Entry.AccountID] = true
		}
		if len(accounts) < 2 {
			continue
		}
		// Small speed differences between copies are noise; health (penalties),
		// allowance left and free capacity are what tell them apart.
		top := math.Inf(-1)
		healthiest := math.Inf(-1)
		best := 0.0
		for _, item := range group {
			top = math.Max(top, item.Score)
			healthiest = math.Max(healthiest, item.Details.Factors.Penalty)
		}
		shares := make([]float64, len(group))
		for index, item := range group {
			shares[index] = item.Quota.Headroom * s.freeCapacity(item.Entry, now)
			best = math.Max(best, shares[index])
		}
		if best <= 0 {
			continue
		}
		for index, item := range group {
			factor := siblingFloor + (1-siblingFloor)*(shares[index]/best)
			penalty := item.Details.Factors.Penalty
			if healthiest == 0 {
				healthiest = 1
			}
			item.Score = top * (penalty / healthiest) * factor
			sibling := factor
			item.Details.Factors.Sibling = &sibling
		}
	}
}

// preferCheaperEquals: among the leading candidates, a model that is clearly
// more expensive than another of about the same quality drops behind it.
func (s *Service) preferCheaperEquals(scored []*Candidate, weightOf func(*Entry) float64) {
	leading := make([]*Candidate, len(scored))
	copy(leading, scored)
	sort.SliceStable(leading, func(i, j int) bool { return leading[i].Score > leading[j].Score })
	if len(leading) > cheaperCandidates {
		leading = leading[:cheaperCandidates]
	}
	type priced struct {
		item    *Candidate
		cost    float64
		quality float64
		weight  float64
	}
	prices := make([]priced, 0, len(leading))
	for _, item := range leading {
		prices = append(prices, priced{item: item, cost: item.OwnCost, quality: item.Quality, weight: weightOf(item.Entry)})
	}
	for _, candidate := range prices {
		cheaperEqual := false
		for _, other := range prices {
			if other.cost >= candidate.cost+clearlyCheaper && other.quality >= candidate.quality*(1-similarQuality) && other.weight >= candidate.weight {
				cheaperEqual = true
				break
			}
		}
		if cheaperEqual {
			candidate.item.Score *= pricierEqualFactor
			// Kept out of the random draw while a cheaper equal can answer.
			candidate.item.PricierEqual = true
		}
	}
}

// NextOptions tunes Next.
