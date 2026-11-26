package routing

import (
	"fmt"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/NeoLabs-Systems/neorouter/internal/routing/task"
)

// The router: hard filters, scoring, the softmax draw and the fallbacks
// (router.js).
const (
	defaultOutputTokens     = 1024
	minOutputReserve        = 256
	unknownCapabilityFactor = 0.85
	// UnknownContextLimit is UNKNOWN_CONTEXT_LIMIT: the window a model that
	// publishes none is held to. Routing uses it to refuse an input that no
	// known window could hold, and a routing group reports it for a member
	// without a window, so that member caps the group.
	UnknownContextLimit = 131072
	relativeBlend       = 0.5
	// Profiles that care about quality (intelligence weight at least this
	// share) push candidates that are clearly weaker than the best one down
	// hard, so a tiny model that is merely fast and reliable is not picked for
	// them.
	qualityProfileShare = 0.25
	qualityGap          = 0.3
	qualityGateFactor   = 0.45
	// Speed and price can outweigh skill, but only so far: a model that is
	// truly out of its depth counts for less. Kept low on purpose, so small
	// fast models that are good enough stay in the running on a style that
	// wants speed.
	weakForTaskFactor = 0.6
	// A pool partner's copy of a model counts for a little less than an own
	// connection, so partners' capacity is used when it helps, not by habit.
	peerFactor = 0.8
	// Reasoning models think before they answer, and a client's max_tokens
	// counts the thinking too. When a model already ran out at this size (its
	// learned output floor) and using it costs nothing extra (free or a
	// flat-rate plan), it is given the room it needs instead of being skipped:
	// the client's limit is raised for that attempt only, so a stronger or
	// faster model is not passed over for a weaker one just because of a budget
	// meant for the answer.
	maxBoostedOutput = 32000
	// How many failures one connection may cost a single request. A provider
	// with many models must not use up every attempt before others get a turn.
	maxFailuresPerAccount = 2
	// A connection held back to even out usage gets a little room again within
	// minutes.
	pacedRecheckMs = 5 * minuteMs
	// Budget pacing (services/budget/pacing.js).
	pacedCostFloor      = 0.1
	overPaceFactor      = 0.15
	typicalOutputTokens = 1500
	accountSoftCapacity = 8
	// The same model on several accounts (two Groq keys, say): prefer the copy
	// with more allowance left and more free capacity.
	siblingFloor = 0.55
	// Among the leading candidates, a model that is clearly more expensive than
	// another of about the same quality drops behind it.
	cheaperCandidates  = 40
	similarQuality     = 0.03
	clearlyCheaper     = 0.05
	pricierEqualFactor = 0.6
	// Cooldowns that end by themselves; whether waiting helps depends on when.
	busyRecheckMs = 1000
	// When nothing else can serve, connections held back only by what the
	// router believes about their health are tried anyway.
	maxLongShots  = 3
	longShotGapMs = 10 * minuteMs
)

// relativeFeatures are the features that get blended with each candidate's
// position within the field.
var relativeFeatures = []string{"intelligence", "speed", "cost"}

// partnerKindTier are tier offsets for a pool partner's request: free first,
// then subscription, then paid.
var partnerKindTier = map[string]int{kindFree: 0, kindPlan: 100, kindPaid: 200}

// longShotReasons are the rejections a long shot may ignore. Not the ones a
// person must fix (payment, key, a model that rejects this kind of request).
var longShotReasons = map[string]bool{
	"rate_limited": true, "quota_exhausted": true, "not_available": true,
	"circuit_open": true, "cooldown": true, "account_resting": true,
}

// preferenceReasons are rejections that mean "held back only by a preference,
// not because it cannot answer". When nothing else can take the request, a slow
// or weaker model that works beats failing.
var preferenceReasons = map[string]bool{
	"outside_routing_group": true, "too_slow": true, "below_min_intelligence": true, "needs_more_tokens": true,
}

// shortCooldowns are the rejections whose cooldown ends by itself.
var shortCooldowns = map[string]bool{
	"rate_limited": true, "quota_exhausted": true, "circuit_open": true, "cooldown": true,
}

// RequestInfo is what the plan needs from a request: its messages (for task
// classification), the tools and format it carries, and the output limit the
// client set. The gateway builds it from the canonical chat request.
type RequestInfo struct {
	Messages       []task.Message
	Tools          []task.Tool
	ResponseFormat *task.ResponseFormat
	MaxTokens      *int
}

func (r RequestInfo) taskRequest() task.Request {
	return task.Request{Messages: r.Messages, Tools: r.Tools, ResponseFormat: r.ResponseFormat}
}

// PlanOptions builds a RoutingPlan (the RoutingPlan constructor's arguments).
type PlanOptions struct {
	UserID         string
	Key            RouterKey
	Request        RequestInfo
	RequestedModel string
	Needs          Needs
	InputTokens    int
	// FreeOnly shuts paid models out (a used-up budget, the playground toggle).
	FreeOnly bool
	Random   func() float64
	// Capability is "chat" when empty.
	Capability string
	// EstimateCost prices one request for capabilities that price per image,
	// second or character; nil uses the budget service's token estimate.
	EstimateCost func(*Entry) *float64
	// TaskHint is a task kind the client named (X-NeoRouter-Task).
	TaskHint string
	// KeepPrivate keeps very sensitive content off pool partners.
	KeepPrivate bool
	// ShareReserve is, for a partner's request, the share of capacity kept for
	// the owner.
	ShareReserve *float64
	SharePoolID  string
	RoutingGroup string
	SessionID    string
	Workload     string
	Now          time.Time
}

// RoutingPlan is one request's route through the catalog. It is created once,
// then Next is called per attempt; Rank and the explain helpers expose what it
// considered.
type RoutingPlan struct {
	svc       *Service
	startedAt time.Time
	// SlotWaitUntil is when a full set of paid slots may have room, nil when the
	// plan is not waiting.
	SlotWaitUntil *int64

	capability   string
	keepPrivate  bool
	shareReserve *float64
	sharePoolID  string
	estimateCost func(*Entry) *float64

	Key         RouterKey
	Request     RequestInfo
	Needs       Needs
	InputTokens int
	freeOnly    bool
	random      func() float64

	tried  map[int64]bool
	parsed ParsedModel

	Group   *Group
	Alias   *Group
	Profile *Profile
	// InitialGroup is the group the request asked for, which a fallback group
	// replaces.
	InitialGroup *Group
	// InitialProfile is the profile before a fallback group's own.
	InitialProfile *Profile

	Task       *task.Result
	Complexity *float64
	Weights    Weights
	Need       *float64
	pacing     *Pacing

	TokenLimitReached bool
	UserID            string
	AffinityScope     string
	AffinityRef       *int64

	cacheRates    map[int64]float64
	visitedGroups map[string]bool
	smoothing     bool

	Entries         []*Entry
	PrimaryRefs     map[int64]bool
	AccountFailures map[string]int

	LongShot      bool
	LongShotsLeft int
	LastResort    bool

	stopWaiting func()
}

// NewPlan builds a request's plan, or fails with the same errors the Node
// router throws (403 MODEL_NOT_ALLOWED, 400 UNKNOWN_PROFILE, 404
// MODEL_NOT_FOUND).
func (s *Service) NewPlan(opts PlanOptions) (*RoutingPlan, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	capability := opts.Capability
	if capability == "" {
		capability = capabilityChat
	}
	requestedModel := opts.RequestedModel
	if strings.TrimSpace(requestedModel) == "" {
		requestedModel = "auto"
	}
	random := opts.Random
	if random == nil {
		random = s.opts.Random
	}
	plan := &RoutingPlan{
		svc:             s,
		startedAt:       now,
		capability:      capability,
		keepPrivate:     opts.KeepPrivate,
		shareReserve:    opts.ShareReserve,
		sharePoolID:     opts.SharePoolID,
		estimateCost:    opts.EstimateCost,
		Key:             opts.Key,
		Request:         opts.Request,
		Needs:           opts.Needs,
		InputTokens:     opts.InputTokens,
		freeOnly:        opts.FreeOnly,
		random:          random,
		tried:           map[int64]bool{},
		parsed:          ParseModel(requestedModel),
		UserID:          opts.UserID,
		PrimaryRefs:     map[int64]bool{},
		AccountFailures: map[string]int{},
		LongShotsLeft:   maxLongShots,
	}
	named := plan.parsed.Profile
	if named == "default" {
		named = ""
	}
	groupName := plan.parsed.Group
	if groupName == "" {
		groupName = opts.RoutingGroup
	}
	if groupName == "" && plan.parsed.Auto {
		groupName = opts.Key.RoutingGroup
	}
	workload := ""
	if plan.parsed.Auto && capability == capabilityChat {
		workload = opts.Workload
	}
	group, alias, err := s.GroupPolicyResolve(opts.UserID, groupName, workload, func(candidate *Group) error {
		if !s.RouteAllowedByKey("pool:"+candidate.Name, opts.Key) {
			return httpserver.New(403, "MODEL_NOT_ALLOWED", "This key may not use this routing group.")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	plan.Group, plan.Alias = group, alias
	profileRef := ""
	if group != nil && group.Profile != nil {
		profileRef = *group.Profile
	}
	if profileRef == "" {
		profileRef = named
	}
	if profileRef == "" {
		profileRef = opts.Key.Profile
	}
	if profileRef == "" {
		profileRef = "balanced"
	}
	plan.Profile = s.ResolveProfile(opts.UserID, profileRef)
	if plan.Profile == nil {
		return nil, httpserver.New(400, "UNKNOWN_PROFILE", fmt.Sprintf("Unknown routing profile %q.", profileRef))
	}
	if (group == nil || !plan.parsed.Auto) && !s.RouteAllowedByKey(requestedModel, opts.Key) {
		display := strings.TrimSpace(requestedModel)
		if display == "" {
			display = "auto"
		}
		return nil, httpserver.New(403, "MODEL_NOT_ALLOWED", fmt.Sprintf("This key may not use %q. Ask for one of the models or routings it allows (GET /v1/models).", display))
	}
	// What a chat request asks for (kind mix, skills, difficulty); other
	// capabilities have no prompt to read.
	if capability == capabilityChat && len(opts.Request.Messages) > 0 {
		plan.Task = task.Classify(opts.Request.taskRequest(), opts.InputTokens, opts.TaskHint)
	}
	if plan.Task != nil {
		complexity := plan.Task.Difficulty
		plan.Complexity = &complexity
	}
	adaptive := plan.Profile.Constraints.Adaptive
	plan.Weights = AdaptWeights(plan.Profile.Weights, plan.Complexity, adaptive)
	// Models above what the task needs compete on price, speed and reliability.
	if adaptive {
		plan.Need = task.NeededIntelligence(plan.Complexity)
	}
	// A budget that would run out before the period ends shifts weight to cost;
	// one with plenty left lets quality and speed count for more.
	if !opts.FreeOnly {
		plan.pacing = s.pacingFor(opts.Key, now)
	}
	if plan.pacing != nil && plan.pacing.CostShift != 1 {
		plan.Weights.Cost = maxf(plan.Weights.Cost, pacedCostFloor) * plan.pacing.CostShift
	}
	plan.InitialGroup = plan.Group
	plan.InitialProfile = plan.Profile
	plan.TokenLimitReached = s.tokenLimitReached(opts.Key, now)
	plan.AffinityScope = opts.Key.ID
	if group != nil && (group.SubagentGroup != nil || group.PreferredAccount != nil) {
		plan.AffinityScope = fmt.Sprintf("[%q,%q,%q]", opts.Key.ID, group.ID, group.ID)
	}
	if opts.SessionID != "" {
		plan.AffinityRef = s.AffinityRef(plan.AffinityScope, opts.SessionID, now)
	}
	plan.cacheRates = map[int64]float64{}
	if plan.AffinityRef != nil {
		plan.cacheRates = s.CacheRates(opts.UserID, now)
	}
	plan.visitedGroups = map[string]bool{}
	if plan.Group != nil {
		plan.visitedGroups[plan.Group.Name] = true
	}
	plan.smoothing = s.SmoothingActive(opts.UserID, now)
	plan.Entries = nil
	for _, entry := range s.catalogList(opts.UserID, capability) {
		plan.Entries = append(plan.Entries, s.planEffective(entry, now))
	}
	// Outcomes are learned per kind of task (learning.go).
	if plan.Task != nil {
		for _, entry := range plan.Entries {
			entry.Task = plan.Task.Primary
		}
	}
	if !plan.parsed.Auto {
		if err := plan.scopeToRequestedModel(); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// scopeToRequestedModel points an explicit model at that exact entry first; the
// same model on the user's other providers serves as fallback.
func (p *RoutingPlan) scopeToRequestedModel() error {
	requested := p.parsed.Model
	var primary []*Entry
	for _, entry := range p.Entries {
		if entry.ID == requested {
			primary = append(primary, entry)
		}
	}
	if len(primary) == 0 {
		for _, entry := range p.Entries {
			if entry.ModelID == requested {
				primary = append(primary, entry)
			}
		}
	}
	slug := requested
	if index := strings.IndexByte(requested, '/'); index >= 0 {
		slug = requested[:index]
	}
	modelPart := requested
	if strings.Contains(requested, "/") {
		hasProvider := false
		for _, entry := range p.Entries {
			if entry.ProviderSlug == slug {
				hasProvider = true
				break
			}
		}
		if hasProvider {
			modelPart = requested[len(slug)+1:]
		}
	}
	canonical := ""
	if len(primary) > 0 {
		canonical = primary[0].CanonicalID
	}
	if canonical == "" {
		canonical = p.svc.canonicalID(modelPart)
	}
	var sameModel []*Entry
	for _, entry := range p.Entries {
		if entry.CanonicalID == canonical {
			sameModel = append(sameModel, entry)
		}
	}
	if len(primary) == 0 && len(sameModel) == 0 {
		close := closestModels(requested, p.Entries)
		hint := ""
		if len(close) > 0 {
			quoted := make([]string, 0, len(close))
			for _, id := range close {
				quoted = append(quoted, fmt.Sprintf("%q", id))
			}
			hint = " Did you mean " + strings.Join(quoted, " or ") + "?"
		}
		message := fmt.Sprintf("The model %q is not a %s model in your catalog.%s Use GET /v1/models or \"auto\".", requested, p.capability, hint)
		httpErr := httpserver.New(404, "MODEL_NOT_FOUND", message)
		if len(close) > 0 {
			httpErr.Details = map[string]any{"suggestions": close}
		}
		return httpErr
	}
	for _, entry := range primary {
		p.PrimaryRefs[entry.Ref] = true
	}
	refs := map[int64]bool{}
	for _, entry := range append(append([]*Entry{}, primary...), sameModel...) {
		refs[entry.Ref] = true
	}
	kept := make([]*Entry, 0, len(p.Entries))
	for _, entry := range p.Entries {
		if refs[entry.Ref] {
			kept = append(kept, entry)
		}
	}
	p.Entries = kept
	return nil
}
