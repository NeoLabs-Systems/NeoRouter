package routing

import (
	"math"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// NextOptions tunes Next.
type NextOptions struct {
	// Filter is what the caller can actually use (for media, adapters that speak
	// the kind).
	Filter    func(*Entry) bool
	Now       time.Time
	Advancing bool
	Sample    bool
}

// Next is the next candidate to try: the best eligible tier, with a softmax
// draw among the top-k so load spreads across good candidates instead of
// hammering the single best one. It returns nil when nothing can take the
// request (or the request should wait).
func (p *RoutingPlan) Next(opts NextOptions) *Candidate {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if p.InitialGroup != nil && !opts.Advancing {
		p.setGroup(p.InitialGroup)
		p.visitedGroups = map[string]bool{p.InitialGroup.Name: true}
	}
	all := p.Rank(RankOptions{IncludeRejected: true, Sample: opts.Sample, Now: now})
	if opts.Filter != nil {
		filtered := make([]*Candidate, 0, len(all))
		for _, item := range all {
			if opts.Filter(item.Entry) {
				filtered = append(filtered, item)
			}
		}
		all = filtered
	}
	var candidates []*Candidate
	for _, item := range all {
		if item.Rejected == "" {
			candidates = append(candidates, item)
		}
	}
	var busyEntries []*Entry
	for _, item := range all {
		if item.Entry.IsFlatRate && (p.parsed.Auto || p.PrimaryRefs[item.Entry.Ref]) && item.Rejected == "account_busy" {
			busyEntries = append(busyEntries, item.Entry)
		}
	}
	p.SlotWaitUntil = p.svc.slotWaitUntil(busyEntries, candidates, p.startedAt, now, p.parsed.Auto && p.Group != nil)
	if p.SlotWaitUntil != nil {
		// Full paid slots and nothing free in this group: a fallback group that
		// can answer now comes before waiting, unless a plan is set to queue.
		waitUntil := *p.SlotWaitUntil
		queueOnly := false
		for _, entry := range busyEntries {
			if entry.OverflowPolicy == "queue" {
				queueOnly = true
				break
			}
		}
		if len(candidates) == 0 && !queueOnly && p.advanceGroup() {
			if choice := p.Next(NextOptions{Filter: opts.Filter, Now: now, Advancing: true, Sample: opts.Sample}); choice != nil {
				return choice
			}
		}
		p.SlotWaitUntil = &waitUntil
		return nil
	}
	if len(candidates) == 0 {
		if p.advanceGroup() {
			return p.Next(NextOptions{Filter: opts.Filter, Now: now, Advancing: true, Sample: opts.Sample})
		}
		return nil
	}
	// Keep learned scores and exploration within the best eligible tier.
	firstTier := candidates[0]
	for _, item := range candidates[1:] {
		if compareRoutingPriority(item, firstTier) < 0 {
			firstTier = item
		}
	}
	ranked := make([]*Candidate, 0, len(candidates))
	for _, item := range candidates {
		if compareRoutingPriority(item, firstTier) == 0 {
			ranked = append(ranked, item)
		}
	}
	choice := ranked[0]
	// Among copies of the requested model (several accounts) the draw stays
	// within those copies; otherwise among the top-k.
	primary := p.PrimaryRefs[choice.Entry.Ref]
	var leading []*Candidate
	if primary {
		for _, item := range ranked {
			if p.PrimaryRefs[item.Entry.Ref] {
				leading = append(leading, item)
			}
		}
	} else {
		topK := p.svc.routingCfg().TopK
		if topK > len(ranked) {
			topK = len(ranked)
		}
		leading = ranked[:topK]
	}
	var cheaperFirst []*Candidate
	for _, item := range leading {
		if !item.PricierEqual {
			cheaperFirst = append(cheaperFirst, item)
		}
	}
	pool := leading
	if len(cheaperFirst) > 0 {
		pool = cheaperFirst
	}
	choice = pool[0]
	if opts.Sample && len(pool) > 1 {
		temperature := maxf(0.01, p.Profile.Constraints.Exploration)
		top := pool[0].Score
		weights := make([]float64, len(pool))
		total := 0.0
		for index, item := range pool {
			weights[index] = math.Exp((item.Score - top) / temperature)
			total += weights[index]
		}
		draw := p.random() * total
		for index := range pool {
			draw -= weights[index]
			if draw <= 0 {
				choice = pool[index]
				break
			}
		}
	}
	if p.parsed.Auto {
		choice.Entry.RoutingReason = groupReason(p.Group, choice, all, p.AffinityRef)
	} else {
		choice.Entry.RoutingReason = ""
	}
	choice.Entry.RoutingGroup = ""
	if p.Group != nil && p.svc.Contains(p.Group, choice.Entry) {
		choice.Entry.RoutingGroup = p.Group.Name
	}
	p.tried[choice.Entry.Ref] = true
	if p.LastResort {
		strict := *p.Context()
		strict.LastResort = false
		strict.LongShot = false
		if preferenceReasons[p.svc.Rejection(choice.Entry, &strict, now)] {
			choice.Entry.LastResort = true
		}
	}
	if p.LongShot && p.heldBack(choice.Entry, now) {
		choice.Entry.TryingLuck = true
		p.markLongShot(choice.Entry.Ref, now)
		p.LongShotsLeft--
		if p.LongShotsLeft <= 0 {
			p.LongShot = false
		}
	}
	return choice
}

// compareRoutingPriority orders candidates by style tier, then preference, then
// tier.
func compareRoutingPriority(a, b *Candidate) int {
	if a.StyleTier != b.StyleTier {
		if a.StyleTier < b.StyleTier {
			return -1
		}
		return 1
	}
	if a.Preference != b.Preference {
		if a.Preference < b.Preference {
			return -1
		}
		return 1
	}
	if a.Tier != b.Tier {
		if a.Tier < b.Tier {
			return -1
		}
		return 1
	}
	return 0
}

// advanceGroup moves to the group's fallback, once each.
func (p *RoutingPlan) advanceGroup() bool {
	if p.Group == nil || p.Group.Fallback == nil {
		return false
	}
	group, err := p.svc.RequireGroup(p.UserID, *p.Group.Fallback)
	if err != nil {
		return false
	}
	if p.visitedGroups[group.Name] {
		return false
	}
	p.visitedGroups[group.Name] = true
	p.setGroup(group)
	return true
}

// setGroup switches the plan to a group, taking its style.
func (p *RoutingPlan) setGroup(group *Group) {
	p.Group = group
	p.Profile = p.InitialProfile
	if group.Profile != nil {
		if resolved := p.svc.ResolveProfile(p.UserID, *group.Profile); resolved != nil {
			p.Profile = resolved
		}
	}
	p.Weights = AdaptWeights(p.Profile.Weights, p.Complexity, p.Profile.Constraints.Adaptive)
	if p.pacing != nil && p.pacing.CostShift != 1 {
		p.Weights.Cost = maxf(p.Weights.Cost, pacedCostFloor) * p.pacing.CostShift
	}
	if p.Profile.Constraints.Adaptive {
		p.Need = task.NeededIntelligence(p.Complexity)
	} else {
		p.Need = nil
	}
}

// QueueWaitMs is how long the request may wait for capacity: the configured
// wait, or longer when a plan slot is expected to free later.
func (p *RoutingPlan) QueueWaitMs(fallback int64) int64 {
	windows := make([]int64, 0, len(p.Entries))
	for _, entry := range p.Entries {
		if entry.IsFlatRate && entry.OverflowPolicy != "overflow" {
			windows = append(windows, p.svc.slotWaitMs(entry))
		}
	}
	if len(windows) == 0 {
		return fallback
	}
	longest := fallback
	for _, window := range windows {
		if window > longest {
			longest = window
		}
	}
	return longest
}

// queueContexts are the contexts of the group chain the request may use: a slot
// on any configured group can wake it; the next draw starts with the preferred
// group again, including after a fallback was busy.
func (p *RoutingPlan) queueContexts() []*PlanContext {
	if p.InitialGroup == nil || p.LastResort {
		return []*PlanContext{p.Context()}
	}
	var contexts []*PlanContext
	seen := map[string]bool{}
	var group *Group = p.InitialGroup
	for group != nil && !seen[group.Name] {
		seen[group.Name] = true
		profile := p.InitialProfile
		if group.Profile != nil {
			if resolved := p.svc.ResolveProfile(p.UserID, *group.Profile); resolved != nil {
				profile = resolved
			}
		}
		ctx := p.Context()
		ctx.Group = group
		ctx.Constraints = profile.Constraints
		contexts = append(contexts, ctx)
		if group.Fallback == nil {
			group = nil
			continue
		}
		group = p.svc.ResolveGroup(p.UserID, *group.Fallback)
	}
	return contexts
}

// heldBack reports whether an entry is held back only by its health record (not
// by a limit, a policy or a request it cannot serve).
func (p *RoutingPlan) heldBack(entry *Entry, now time.Time) bool {
	ctx := *p.Context()
	ctx.LongShot = false
	return longShotReasons[p.svc.Rejection(entry, &ctx, now)]
}

// EnableLastResort drops the preferences that held working models back when
// nothing else can serve. It reports whether that added a candidate.
func (p *RoutingPlan) EnableLastResort(now time.Time, filter func(*Entry) bool) bool {
	if p.LastResort {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	p.LastResort = false
	for _, context := range p.queueContexts() {
		relaxed := *context
		relaxed.LastResort = true
		strict := *context
		strict.LongShot = false
		for _, entry := range p.Entries {
			if p.tried[entry.Ref] || (filter != nil && !filter(entry)) {
				continue
			}
			if preferenceReasons[p.svc.Rejection(entry, &strict, now)] && p.svc.Rejection(entry, &relaxed, now) == "" {
				p.LastResort = true
				return true
			}
		}
	}
	return false
}

// EnableLongShots allows a few tries of connections held back only by their
// health record when nothing else can serve.
func (p *RoutingPlan) EnableLongShots(now time.Time, filter func(*Entry) bool) bool {
	if now.IsZero() {
		now = time.Now()
	}
	if p.LongShot || p.LongShotsLeft <= 0 {
		return false
	}
	ctx := *p.Context()
	ctx.LongShot = true
	possible := false
	for _, entry := range p.Entries {
		if p.tried[entry.Ref] || (filter != nil && !filter(entry)) {
			continue
		}
		if p.heldBack(entry, now) && p.svc.longShotDue(entry.Ref, now) && p.svc.Rejection(entry, &ctx, now) == "" {
			possible = true
			break
		}
	}
	p.LongShot = possible
	return possible
}

// FreesUpAt is when the soonest candidate that is only blocked for now
// (rate-limited, cooling down, at its concurrency or per-minute limit) may take
// work again, or nil when none will without outside help.
func (p *RoutingPlan) FreesUpAt(now time.Time, filter func(*Entry) bool) *int64 {
	if now.IsZero() {
		now = time.Now()
	}
	nowMs := now.UnixMilli()
	var soonest *int64
	setSoonest := func(at int64) {
		if soonest == nil || at < *soonest {
			value := at
			soonest = &value
		}
	}
	if p.SlotWaitUntil != nil && *p.SlotWaitUntil > nowMs {
		setSoonest(minInt64(*p.SlotWaitUntil, nowMs+busyRecheckMs))
	}
	for _, ctx := range p.queueContexts() {
		for _, entry := range p.Entries {
			if p.tried[entry.Ref] || (filter != nil && !filter(entry)) {
				continue
			}
			reason := p.svc.Rejection(entry, ctx, now)
			var at *int64
			switch {
			case reason == "account_busy" || reason == "held_for_priority":
				value := nowMs + busyRecheckMs
				at = &value
			case reason == "paced":
				value := nowMs + pacedRecheckMs
				if next := p.svc.SmoothingNextAt(entry, SmoothingContext{UserID: p.UserID, InputTokens: p.InputTokens, MaxOutput: p.Request.MaxTokens, Priority: p.Key.Priority, Now: now}); next != nil {
					value = *next
				}
				at = &value
			case shortCooldowns[reason]:
				value := nowMs + busyRecheckMs
				if availability := p.svc.stats.availability(entry.Ref, now); availability.Until != 0 {
					value = availability.Until
				}
				at = &value
			case strings.HasPrefix(reason, "quota_"):
				blocked := p.svc.quotaEvaluate(entry, p.InputTokens, now).Blocked
				if blocked != nil && blocked.ResetAt != nil {
					value := blocked.ResetAt.UnixMilli()
					at = &value
				} else if reason == "quota_rpm" || reason == "quota_tpm" {
					value := nowMs + minuteMs
					at = &value
				}
			}
			if at != nil {
				setSoonest(*at)
			}
		}
	}
	return soonest
}

// WaitingOnBusy reports whether a model that can take this request is only at
// its account's limit of requests at once: a slot frees as soon as one of those
// answers.
func (p *RoutingPlan) WaitingOnBusy(now time.Time, filter func(*Entry) bool) bool {
	if now.IsZero() {
		now = time.Now()
	}
	for _, ctx := range p.queueContexts() {
		for _, entry := range p.Entries {
			if p.tried[entry.Ref] || (filter != nil && !filter(entry)) {
				continue
			}
			if p.svc.Rejection(entry, ctx, now) == "account_busy" {
				return true
			}
		}
	}
	return false
}

// JoinQueue announces what this waiting request needs, so lower priorities
// leave it alone. Called on every pass, as candidates free up or block;
// LeaveQueue ends it.
func (p *RoutingPlan) JoinQueue(now time.Time, filter func(*Entry) bool) {
	if now.IsZero() {
		now = time.Now()
	}
	targets := map[string]bool{}
	var list []string
	add := func(target string) {
		if !targets[target] {
			targets[target] = true
			list = append(list, target)
		}
	}
	for _, ctx := range p.queueContexts() {
		for _, entry := range p.Entries {
			if p.tried[entry.Ref] || (filter != nil && !filter(entry)) {
				continue
			}
			reason := p.svc.Rejection(entry, ctx, now)
			if reason == "account_busy" {
				add("account:" + entry.AccountID)
			} else if reason == "held_for_priority" || shortCooldowns[reason] || reason == "quota_rpm" || reason == "quota_tpm" {
				add("model:" + formatRef(entry.Ref))
			}
		}
	}
	p.LeaveQueue()
	p.stopWaiting = p.svc.waiting.join(list, p.Key.Priority)
}

// LeaveQueue ends this request's claim on what it was waiting for.
func (p *RoutingPlan) LeaveQueue() {
	if p.stopWaiting != nil {
		p.stopWaiting()
		p.stopWaiting = nil
	}
}

// NoteFailure records a failed attempt, so the request moves on from a
// connection that keeps failing.
func (p *RoutingPlan) NoteFailure(entry *Entry, kind Kind) {
	if connectionFailures[kind] {
		p.AccountFailures[entry.AccountID]++
	}
}

// RejectionSummary counts why candidates were left out. Filter marks the ones
// the caller cannot use as "unsupported", so none go unexplained.
func (p *RoutingPlan) RejectionSummary(now time.Time, filter func(*Entry) bool) map[string]int {
	if now.IsZero() {
		now = time.Now()
	}
	counts := map[string]int{}
	for _, item := range p.Rank(RankOptions{IncludeRejected: true, Sample: false, Now: now}) {
		reason := item.Rejected
		if reason == "" && filter != nil && !filter(item.Entry) {
			reason = "unsupported"
		}
		if reason != "" {
			counts[reason]++
		}
	}
	return counts
}

// longShotDue reports whether a model is a long shot at most once per gap,
// across all requests. Each held-back model is a long shot at most this often:
// a busy server must not send every request to models it already knows are
// refusing.
func (s *Service) longShotDue(ref int64, now time.Time) bool {
	s.longShotsMu.Lock()
	defer s.longShotsMu.Unlock()
	last := s.longShots[ref]
	return now.UnixMilli()-last >= longShotGapMs
}

func (p *RoutingPlan) markLongShot(ref int64, now time.Time) {
	s := p.svc
	s.longShotsMu.Lock()
	defer s.longShotsMu.Unlock()
	if len(s.longShots) > 5000 {
		s.longShots = map[int64]int64{}
	}
	s.longShots[ref] = now.UnixMilli()
}

// ResetLongShots forgets when each model was last tried as a long shot.
func (s *Service) ResetLongShots() {
	s.longShotsMu.Lock()
	defer s.longShotsMu.Unlock()
	s.longShots = map[int64]int64{}
}

// estimateMaxCost, budgetRemaining, tokenLimitReached and pacingFor keep the
// budget consumer optional for tests and for a Service wired without one.
