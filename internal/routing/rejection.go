package routing

import "time"

// PlanContext is what every rejection and score sees (RoutingPlan.context()).
type PlanContext struct {
	StartedAt         time.Time
	Group             *Group
	Alias             *Group
	InitialGroup      *Group
	KeepPrivate       bool
	ShareReserve      *float64
	SharePoolID       string
	LongShot          bool
	LastResort        bool
	AccountFailures   map[string]int
	TokenLimitReached bool
	OnlyKind          string
	Key               RouterKey
	Needs             Needs
	InputTokens       int
	Request           RequestInfo
	Constraints       Constraints
	FreeOnly          bool
	PrimaryRefs       map[int64]bool
	EstimateCost      func(*Entry) *float64
	Smoothing         bool
	UserID            string
}

// Context is the plan's current view, for the rejections and scores of one
// attempt.
func (p *RoutingPlan) Context() *PlanContext {
	return &PlanContext{
		StartedAt:         p.startedAt,
		Group:             p.Group,
		Alias:             p.Alias,
		InitialGroup:      p.InitialGroup,
		KeepPrivate:       p.keepPrivate,
		ShareReserve:      p.shareReserve,
		SharePoolID:       p.sharePoolID,
		LongShot:          p.LongShot,
		LastResort:        p.LastResort,
		AccountFailures:   p.AccountFailures,
		TokenLimitReached: p.TokenLimitReached,
		OnlyKind:          p.parsed.Kind,
		Key:               p.Key,
		Needs:             p.Needs,
		InputTokens:       p.InputTokens,
		Request:           p.Request,
		Constraints:       p.Profile.Constraints,
		FreeOnly:          p.freeOnly,
		PrimaryRefs:       p.PrimaryRefs,
		EstimateCost:      p.estimateCost,
		Smoothing:         p.smoothing,
		UserID:            p.UserID,
	}
}

// Rejection is the hard filter: a reason string when the entry cannot serve,
// "" when it can.
func (s *Service) Rejection(entry *Entry, ctx *PlanContext, now time.Time) string {
	if ctx.Group != nil && (!ctx.LastResort || !allowsOutside(ctx.Alias, ctx.InitialGroup, ctx.Group)) && !s.Contains(ctx.Group, entry) {
		return "outside_routing_group"
	}
	if !ctx.LastResort && ctx.Group != nil && ctx.Group.MinIntelligence != nil {
		intelligence := 0.0
		if entry.Intelligence != nil {
			intelligence = *entry.Intelligence
		}
		if intelligence < *ctx.Group.MinIntelligence {
			return "below_min_intelligence"
		}
	}
	if !entry.Enabled {
		return "model_disabled"
	}
	if !entry.Available {
		return "model_unavailable"
	}
	if !entry.AccountEnabled || entry.AccountStatus == "disabled" || entry.AccountStatus == "invalid" {
		return "account_unavailable"
	}
	if !s.AllowedByKey(entry, ctx.Key, now) {
		return "not_allowed_for_key"
	}
	if ctx.AccountFailures[entry.AccountID] >= maxFailuresPerAccount && !ctx.PrimaryRefs[entry.Ref] {
		return "account_failing"
	}
	if (!ctx.LongShot || !s.longShotDue(entry.Ref, now)) && s.health.restingUntil(entry.AccountID, now) != 0 && !ctx.PrimaryRefs[entry.Ref] {
		return "account_resting"
	}
	// A partner's key has no cost choice of its own: what the owner shares
	// decides (below).
	if ctx.Key.PeerLinkID == "" {
		if kind := KindAllowed(entry, ctx.Key, ctx.FreeOnly); kind != "" {
			return kind
		}
	}
	// Pools: a partner's request only reaches what is shared with them, and is
	// never passed on; an own request reaches partners only with a pool key, and
	// not when it holds very sensitive data.
	if ctx.Key.PeerLinkID != "" {
		if s.opts.Pools != nil {
			shared := s.opts.Pools.ShareRejection(entry, ShareRequest{InputTokens: ctx.InputTokens, Reserve: ctx.ShareReserve, PoolID: ctx.SharePoolID}, now)
			if shared != "" {
				return shared
			}
		}
	} else if s.isPeerEntry(entry) {
		if ctx.Key.PoolAccess != "pool" {
			return "not_in_pool"
		}
		if ctx.KeepPrivate {
			return "kept_private"
		}
	}
	if ctx.TokenLimitReached && (entry.IsFree || entry.IsPlan) {
		return "token_limit"
	}
	if !allowsMetered(ctx.Alias, ctx.InitialGroup, ctx.Group) && (ModelKind(entry) == kindPaid || (entry.PlanOverflow && !entry.IsFree)) {
		return "metered_not_allowed"
	}
	// A connection on "Free" has no credits: its models that are not free fail.
	if NeedsCredits(entry) {
		return "needs_credits"
	}
	if ctx.OnlyKind != "" && ModelKind(entry) != ctx.OnlyKind {
		return "other_kind"
	}
	if ctx.Constraints.Kinds != nil && !(ctx.Constraints.Kinds.get(ModelKind(entry)) > 0) {
		return "kind_not_in_style"
	}
	if ctx.Needs.Tools && entry.Tools != nil && !*entry.Tools {
		return "no_tools"
	}
	if ctx.Needs.Vision && (entry.Vision == nil || !*entry.Vision) {
		return "no_vision"
	}
	reserve := minOutputReserve
	maxTokens := minOutputReserve
	if ctx.Request.MaxTokens != nil {
		maxTokens = *ctx.Request.MaxTokens
	}
	if maxTokens < minOutputReserve {
		reserve = minOutputReserve
	} else if maxTokens < 32000 {
		reserve = maxTokens
	} else {
		reserve = 32000
	}
	if entry.ContextWindow > 0 && ctx.InputTokens+reserve > entry.ContextWindow {
		return "context_too_small"
	}
	// An unknown window is not an infinite one: beyond this size only models
	// that publish their limit are tried.
	if entry.ContextWindow == 0 && ctx.InputTokens+reserve > UnknownContextLimit {
		return "context_unknown"
	}
	if len(ctx.Constraints.IncludeProviders) > 0 && !containsString(ctx.Constraints.IncludeProviders, entry.ProviderSlug) && !containsString(ctx.Constraints.IncludeProviders, entry.DefinitionID) {
		return "provider_not_included"
	}
	if containsString(ctx.Constraints.ExcludeProviders, entry.ProviderSlug) || containsString(ctx.Constraints.ExcludeProviders, entry.DefinitionID) {
		return "provider_excluded"
	}
	for _, pattern := range ctx.Constraints.ExcludeModels {
		if GlobToRegExp(pattern).MatchString(entry.ID) {
			return "model_excluded"
		}
	}
	if !ctx.LastResort && ctx.Constraints.MinIntelligence != nil {
		intelligence := 0.0
		if entry.Intelligence != nil {
			intelligence = *entry.Intelligence
		}
		if intelligence < *ctx.Constraints.MinIntelligence {
			return "below_min_intelligence"
		}
	}
	if ctx.Constraints.MaxPriceOut != nil && !entry.IsFree && !entry.IsPlan {
		if entry.PriceOut == nil || *entry.PriceOut > *ctx.Constraints.MaxPriceOut {
			return "too_expensive"
		}
	}
	if ctx.Key.MaxPricePerMillion != nil && !entry.IsFree && !entry.IsPlan {
		if entry.PriceOut == nil || *entry.PriceOut > *ctx.Key.MaxPricePerMillion {
			return "key_too_expensive"
		}
	}
	if !ctx.LastResort && ctx.Constraints.MaxTTFTMs != nil && entry.Stats.TTFTMs != nil && *entry.Stats.TTFTMs > float64(*ctx.Constraints.MaxTTFTMs) {
		return "too_slow"
	}
	if !entry.IsFree && !entry.IsPlan {
		hasBudget := ctx.Key.BudgetUSD != nil
		var maxCost *float64
		if ctx.EstimateCost != nil {
			maxCost = ctx.EstimateCost(entry)
		} else {
			maxCost = s.estimateMaxCost(entry, ctx.InputTokens, s.maxOutputFor(ctx.Request, entry))
		}
		if hasBudget && maxCost == nil {
			return "price_unknown"
		}
		if hasBudget {
			if remaining := s.budgetRemaining(ctx.Key, now); remaining != nil && *maxCost > *remaining {
				return "budget_exhausted"
			}
		}
	}
	// Reasoning models that already ran out of budget at this size are skipped.
	minOutput := s.stats.outputFloor(entry.Ref, now)
	if ctx.Request.MaxTokens != nil && minOutput > 0 && int64(*ctx.Request.MaxTokens) < minOutput &&
		s.BoostedMaxTokens(entry, ctx.Request, now) == nil && !ctx.PrimaryRefs[entry.Ref] && !ctx.LastResort {
		return "needs_more_tokens"
	}
	available := s.stats.availability(entry.Ref, now)
	if !available.Available && !(ctx.LongShot && longShotReasons[available.Reason] && s.longShotDue(entry.Ref, now)) {
		return available.Reason
	}
	if planReason := s.planRejection(entry, now); planReason != "" {
		return planReason
	}
	// An account at its (learned or documented) concurrency limit takes no more
	// work right now.
	if parallel := s.conc.limitFor(entry, now); parallel != nil && s.load.inflight(entry).Account >= parallel.Limit {
		return s.slotBusyReason(entry, ctx.StartedAt, now)
	}
	quotaState := s.quotaEvaluate(entry, ctx.InputTokens, now)
	if quotaState.Blocked != nil {
		return "quota_" + quotaState.Blocked.Metric
	}
	if s.waiting.heldBack(entry, ctx.Key.Priority) {
		return "held_for_priority"
	}
	if ctx.Smoothing && s.SmoothingHeld(entry, SmoothingContext{UserID: ctx.UserID, InputTokens: ctx.InputTokens, MaxOutput: ctx.Request.MaxTokens, Priority: ctx.Key.Priority, Now: now}) {
		return "paced"
	}
	return ""
}

// MaxOutputFor is maxOutputFor: the client's limit, or a small default.
func MaxOutputFor(request RequestInfo, entry *Entry) int {
	if request.MaxTokens != nil {
		return *request.MaxTokens
	}
	limit := defaultOutputTokens
	if entry.MaxOutput > 0 && entry.MaxOutput < defaultOutputTokens {
		limit = entry.MaxOutput
	}
	return limit
}

func (s *Service) maxOutputFor(request RequestInfo, entry *Entry) int {
	return MaxOutputFor(request, entry)
}

// BoostedMaxTokens is boostedMaxTokens: the room a reasoning model needs to
// answer, when raising the client's limit costs nothing extra.
func (s *Service) BoostedMaxTokens(entry *Entry, request RequestInfo, now time.Time) *int {
	if request.MaxTokens == nil || !(entry.IsFree || entry.IsPlan) {
		return nil
	}
	floor := s.stats.outputFloor(entry.Ref, now)
	if floor == 0 || floor <= int64(*request.MaxTokens) {
		return nil
	}
	cap := int64(maxBoostedOutput)
	if entry.MaxOutput > 0 {
		cap = int64(entry.MaxOutput)
	}
	if floor <= cap {
		value := int(floor)
		return &value
	}
	return nil
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
