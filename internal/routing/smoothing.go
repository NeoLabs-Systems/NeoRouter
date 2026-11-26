package routing

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Even out allowance consumption using actual balances, reset windows and
// learned request/token demand (smoothing.js). Fixed windows use cumulative
// allowance; rolling windows include the remaining balance and upcoming
// expirations. It can be switched on only once there is enough data to go by
// (readiness). Keys with a high priority are never held back, and paid
// connections are not affected.

// SmoothingSetting is the user setting that turns evened-out usage on.
const SmoothingSetting = "smooth_allowances"

// peerDefinition is a pool partner's account: it keeps its own limits.
const peerDefinition = "neorouter_peer"

// SmoothingNeeded is how much history the feature needs before it may be
// switched on.
type SmoothingNeeded struct {
	Days       int     `json:"days"`
	ActiveDays int     `json:"activeDays"`
	Requests   int     `json:"requests"`
	Coverage   float64 `json:"coverage"`
}

// SmoothingNeededValues are the thresholds readiness compares against.
var SmoothingNeededValues = SmoothingNeeded{Days: 7, ActiveDays: 5, Requests: 100, Coverage: 0.5}

// Readiness is how much history exists, and whether that is enough.
type Readiness struct {
	Ready      bool            `json:"ready"`
	Days       int             `json:"days"`
	ActiveDays int             `json:"activeDays"`
	Requests   int             `json:"requests"`
	Coverage   float64         `json:"coverage"`
	Needed     SmoothingNeeded `json:"needed"`
}

// SmoothingStatus is what Settings → Usage shows.
type SmoothingStatus struct {
	Enabled   bool      `json:"enabled"`
	Active    bool      `json:"active"`
	Readiness Readiness `json:"readiness"`
}

// SmoothingContext is who is asking and how large the request is.
type SmoothingContext struct {
	UserID      string
	InputTokens int
	MaxOutput   *int
	Priority    string
	Now         time.Time
}

// SmoothingDecision is whether a model is paced now, the score factor that
// goes with it, and when it could be tried again.
type SmoothingDecision struct {
	Held   bool
	Factor float64
	NextAt *int64
}

type cacheEntry[T any] struct {
	at    int64
	value T
}

type profileShape struct {
	values []float64
	prefix []float64
}

type demandProfile struct {
	requests profileShape
	tokens   profileShape
	output   map[string]struct {
		sum    float64
		weight float64
	}
	meanOutput float64
}

type smoother struct {
	svc *Service

	mu        sync.Mutex
	readiness map[string]cacheEntry[Readiness]
	profiles  map[string]cacheEntry[*demandProfile]
}

func newSmoother(svc *Service) *smoother {
	return &smoother{svc: svc, readiness: map[string]cacheEntry[Readiness]{}, profiles: map[string]cacheEntry[*demandProfile]{}}
}

// paceable: only free and plan models have an allowance to spread; paid ones
// are billed per use and pool partners keep their own limits.
func paceable(entry *Entry) bool {
	return entry != nil && !entry.IsFlatRate && (entry.IsFree || entry.IsPlan) && entry.DefinitionID != peerDefinition
}

// limitKnown reports whether NeoRouter knows how a model's allowance behaves: a
// daily limit or plan window it can pace, or only per-minute limits, which
// cannot run out over a day and so need no pacing.
func (s *Service) limitKnown(entry *Entry, now time.Time) bool {
	if entry.IsPlan && s.planSchedule(entry, now) != nil {
		return true
	}
	if s.opts.Quota == nil {
		return false
	}
	for _, scope := range s.opts.Quota.ScopesFor(entry) {
		daily := s.opts.Quota.DailyLimits(scope, now)
		if daily.RPD > 0 || daily.TPD > 0 {
			return true
		}
		known := s.opts.Quota.EffectiveLimits(scope)
		if limit, ok := known["rpm"]; ok && limit.Value > 0 {
			return true
		}
		if limit, ok := known["tpm"]; ok && limit.Value > 0 {
			return true
		}
	}
	return false
}

func (s *Service) computeReadiness(userID string, now time.Time) Readiness {
	out := Readiness{Needed: SmoothingNeededValues}
	if s.opts.DB == nil {
		return out
	}
	nowMs := now.UnixMilli()
	var first *int64
	_ = s.opts.DB.R.QueryRow(`SELECT MIN(created_at) FROM requests WHERE user_id = ? AND created_at >= ?`, userID, nowMs-30*dayMs).Scan(&first)
	days := 0
	if first != nil {
		days = int((nowMs - *first) / dayMs)
	}
	var requests, active int
	_ = s.opts.DB.R.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT CAST(created_at / ? AS INTEGER)) FROM requests
		WHERE user_id = ? AND created_at >= ? AND status = 'success'`, dayMs, userID, nowMs-14*dayMs).Scan(&requests, &active)
	// Of last week's chat answers on free and plan models (the ones with an
	// allowance to spread), how many went to a model whose limits are known.
	// Paid models, other capabilities and pool partners do not count either way.
	type modelCount struct {
		ref   int64
		count float64
	}
	var byModel []modelCount
	rows, err := s.opts.DB.R.Query(`SELECT model_ref, COUNT(*) FROM requests WHERE user_id = ? AND created_at >= ? AND status = 'success'
		AND model_ref IS NOT NULL GROUP BY model_ref`, userID, nowMs-7*dayMs)
	if err == nil {
		for rows.Next() {
			var item modelCount
			if rows.Scan(&item.ref, &item.count) == nil {
				byModel = append(byModel, item)
			}
		}
		rows.Close()
	}
	entries := map[int64]*Entry{}
	if s.opts.Catalog != nil {
		for _, entry := range s.opts.Catalog.ListForUser(userID, CatalogQuery{Capability: capabilityChat, IncludeUnavailable: true}) {
			entries[entry.Ref] = entry
		}
	}
	total, known := 0.0, 0.0
	for _, item := range byModel {
		entry := entries[item.ref]
		if entry == nil || !paceable(entry) {
			continue
		}
		total += item.count
		if s.limitKnown(entry, now) {
			known += item.count
		}
	}
	coverage := 0.0
	if total != 0 {
		coverage = known / total
	}
	out.Days = days
	out.ActiveDays = active
	out.Requests = requests
	out.Coverage = roundTo(coverage, 2)
	out.Ready = out.Days >= SmoothingNeededValues.Days && out.ActiveDays >= SmoothingNeededValues.ActiveDays &&
		out.Requests >= SmoothingNeededValues.Requests && out.Coverage >= SmoothingNeededValues.Coverage
	return out
}

// SmoothingReadiness is whether there is enough history to go by.
func (s *Service) SmoothingReadiness(userID string, now time.Time) Readiness {
	s.smoother.mu.Lock()
	defer s.smoother.mu.Unlock()
	if hit, ok := s.smoother.readiness[userID]; ok {
		if now.UnixMilli() >= hit.at && now.UnixMilli()-hit.at < policyCacheMs {
			return hit.value
		}
	}
	value := s.computeReadiness(userID, now)
	s.smoother.readiness[userID] = cacheEntry[Readiness]{at: now.UnixMilli(), value: value}
	return value
}

// demandProfileFor learns request and token demand in UTC hour-of-week bins.
// Recent successful work counts more; retries and provider errors do not teach
// a false peak. UTC keeps the profile stable across server moves.
func (s *Service) demandProfileFor(userID string, now time.Time) *demandProfile {
	s.smoother.mu.Lock()
	defer s.smoother.mu.Unlock()
	if hit, ok := s.smoother.profiles[userID]; ok {
		if now.UnixMilli() >= hit.at && now.UnixMilli()-hit.at < policyCacheMs {
			return hit.value
		}
	}
	value := s.computeDemandProfile(userID, now)
	s.smoother.profiles[userID] = cacheEntry[*demandProfile]{at: now.UnixMilli(), value: value}
	return value
}

func (s *Service) computeDemandProfile(userID string, now time.Time) *demandProfile {
	requests := make([]float64, 168)
	tokens := make([]float64, 168)
	output := map[string]struct {
		sum    float64
		weight float64
	}{}
	totalWeight, totalOutput := 0.0, 0.0
	if s.opts.DB != nil {
		nowMs := now.UnixMilli()
		rows, err := s.opts.DB.R.Query(`SELECT created_at, model_ref, account_id, input_tokens, output_tokens FROM requests
			WHERE user_id = ? AND created_at >= ? AND created_at <= ? AND status = 'success'`, userID, nowMs-policyHistoryMs, nowMs)
		if err == nil {
			for rows.Next() {
				var createdAt int64
				var modelRef *int64
				var accountID *string
				var inputTokens, outputTokens float64
				if err := rows.Scan(&createdAt, &modelRef, &accountID, &inputTokens, &outputTokens); err != nil {
					continue
				}
				at := time.UnixMilli(createdAt).UTC()
				bin := int(at.Weekday())*24 + at.Hour()
				weight := math.Pow(0.5, float64(nowMs-createdAt)/float64(policyHistoryHalfLifeMs))
				requests[bin] += weight
				tokens[bin] += weight * (inputTokens + outputTokens)
				totalWeight += weight
				totalOutput += weight * outputTokens
				for _, key := range []string{modelKey(modelRef), accountKey(accountID)} {
					if key == "" {
						continue
					}
					value := output[key]
					value.sum += weight * outputTokens
					value.weight += weight
					output[key] = value
				}
			}
			rows.Close()
		}
	}
	meanOutput := 0.0
	if totalWeight != 0 {
		meanOutput = totalOutput / totalWeight
	}
	return &demandProfile{requests: newProfileShape(requests), tokens: newProfileShape(tokens), output: output, meanOutput: meanOutput}
}

func modelKey(ref *int64) string {
	if ref == nil {
		return ""
	}
	return "m:" + formatRef(*ref)
}

func accountKey(account *string) string {
	if account == nil || *account == "" {
		return ""
	}
	return "p:" + *account
}

func newProfileShape(counts []float64) profileShape {
	sum := 0.0
	for _, count := range counts {
		sum += count
	}
	values := make([]float64, len(counts))
	for i, count := range counts {
		share := 1.0 / 168
		if sum != 0 {
			share = count / sum
		}
		values[i] = policyProfileBlend*share + (1-policyProfileBlend)/168
	}
	prefix := make([]float64, 1, len(values)+1)
	prefix[0] = 0
	for _, value := range values {
		prefix = append(prefix, prefix[len(prefix)-1]+value)
	}
	return profileShape{values: values, prefix: prefix}
}

const (
	weekMs = 7 * dayMs
	// weekEpoch is Date.UTC(1970, 0, 4): a Sunday, as getUTCDay() counts.
	weekEpoch = 3 * dayMs
)

func massOf(shape profileShape, start, end int64) float64 {
	cumulative := func(at int64) float64 {
		weeks := (at - weekEpoch) / weekMs
		phase := at - weekEpoch - weeks*weekMs
		hour := int(phase / hourMs)
		if hour > 167 {
			hour = 167
		}
		if hour < 0 {
			hour = 0
		}
		return float64(weeks) + shape.prefix[hour] + shape.values[hour]*(float64(phase%hourMs)/float64(hourMs))
	}
	return maxf(0, cumulative(end)-cumulative(start))
}

func expectedOutput(profile *demandProfile, scope string, maxOutput *int) float64 {
	learned := profile.output[scope]
	count := learned.weight
	output := learned.sum / maxf(1, count)
	confidence := count / (count + policyOutputPriorSamples)
	estimate := output*confidence + profile.meanOutput*(1-confidence)
	if maxOutput != nil && *maxOutput > 0 {
		return minf(float64(*maxOutput), estimate)
	}
	return estimate
}

// SmoothingEnabled is whether the user switched evened-out usage on.
func (s *Service) SmoothingEnabled(userID string) bool {
	if s.opts.Settings == nil {
		return false
	}
	return s.opts.Settings.UserSettingIs(userID, SmoothingSetting)
}

// SmoothingActive is whether pacing applies (readiness gates initial
// enablement; new unknown models must not unpace allowances that were already
// learned and explicitly protected).
func (s *Service) SmoothingActive(userID string, now time.Time) bool {
	return s.SmoothingEnabled(userID)
}

// SmoothingStatusOf is what Settings → Usage shows.
func (s *Service) SmoothingStatusOf(userID string, now time.Time) SmoothingStatus {
	enabled := s.SmoothingEnabled(userID)
	return SmoothingStatus{Enabled: enabled, Active: enabled, Readiness: s.SmoothingReadiness(userID, now)}
}

// SetSmoothingEnabled switches evened-out usage on or off. It cannot be
// switched on before there is enough data.
func (s *Service) SetSmoothingEnabled(userID string, value bool, now time.Time) (SmoothingStatus, error) {
	if value && !s.SmoothingEnabled(userID) && !s.SmoothingReadiness(userID, now).Ready {
		return SmoothingStatus{}, httpserver.New(409, "NOT_ENOUGH_DATA", "NeoRouter does not have enough data yet to even out usage.")
	}
	if s.opts.Settings != nil {
		if err := s.opts.Settings.SetUserSetting(userID, SmoothingSetting, value); err != nil {
			return SmoothingStatus{}, fmt.Errorf("routing: save smoothing setting: %w", err)
		}
	}
	return s.SmoothingStatusOf(userID, now), nil
}

// smoothingDecision is a decision in one pass: does this request wait, how much
// does pacing cost it, and when could it go.
func (s *Service) smoothingDecision(entry *Entry, ctx SmoothingContext) SmoothingDecision {
	now := ctx.Now
	if now.IsZero() {
		now = time.Now()
	}
	if ctx.Priority == "high" || !paceable(entry) {
		return SmoothingDecision{Factor: 1}
	}
	profile := s.demandProfileFor(ctx.UserID, now)
	var violations []int64
	pressure := 0.0
	fixed := func(used, cost float64, startAt, resetAt int64, cushion float64, shape profileShape) {
		whole := massOf(shape, startAt, resetAt)
		elapsed := 1.0
		if whole != 0 {
			elapsed = massOf(shape, startAt, now.UnixMilli()) / whole
		}
		allowance := minf(1, elapsed+cushion)
		pressure = maxf(pressure, (used+cost)/maxf(allowance, math.SmallestNonzeroFloat64))
		if used+cost <= allowance {
			return
		}
		// Monotonic cumulative demand: find when this request earns enough room.
		low, high := now.UnixMilli(), resetAt
		for i := 0; i < 32 && high-low > 1000; i++ {
			mid := (low + high) / 2
			if massOf(shape, startAt, mid)/whole+cushion >= used+cost {
				high = mid
			} else {
				low = mid
			}
		}
		violations = append(violations, high)
	}
	if entry.IsPlan {
		for _, window := range s.planSchedules(entry, now) {
			fixed(window.UsedShare, window.RequestShare, window.StartAt, window.ResetAt, policyPlanCushion, profile.tokens)
		}
	}
	if s.opts.Quota != nil {
		load := s.load.inflight(entry)
		for _, scope := range s.opts.Quota.ScopesFor(entry) {
			parallel := load.Model
			if scope.Kind == "pool" {
				parallel = load.Account
			}
			output := expectedOutput(profile, scope.Key, ctx.MaxOutput)
			for _, metric := range []string{"rpd", "tpd"} {
				window := s.opts.Quota.DailyWindow(scope, metric, now)
				if window == nil || window.Limit <= 0 {
					continue
				}
				isTokens := metric == "tpd"
				shape := profile.requests
				cost := 1.0
				if isTokens {
					shape = profile.tokens
					cost = float64(ctx.InputTokens) + output
				}
				pending := 0.0
				if isTokens {
					pending = float64(parallel) * output
				}
				if !window.Rolling {
					startAt, resetAt := window.StartAt.UnixMilli(), now.UnixMilli()
					if window.ResetAt != nil {
						resetAt = window.ResetAt.UnixMilli()
					}
					fixed((window.Used+pending)/window.Limit, cost/window.Limit, startAt, resetAt, policyDailyCushion, shape)
					continue
				}
				whole := massOf(shape, now.UnixMilli(), now.UnixMilli()+dayMs)
				hourShare := maxf(policyMinHourShare, massOf(shape, now.UnixMilli(), now.UnixMilli()+hourMs)/whole) * policyPeakRoom
				allowance := minf(window.Limit*hourShare, (window.Remaining+window.Recent)*hourShare+window.Expiring)
				proposed := window.Recent + pending + cost
				pressure = maxf(pressure, proposed/maxf(allowance, math.SmallestNonzeroFloat64))
				if proposed > allowance+1e-6 {
					releaseAt := s.opts.Quota.NextReleaseAt(scope.Key, 60, now)
					dailyRelease := s.opts.Quota.NextReleaseAt(scope.Key, 1440, now)
					recheck := policyRetrySearchMs
					if pending > 0 {
						recheck = policyInflightRecheckMs
					}
					at := now.UnixMilli() + int64(recheck)
					if releaseAt != nil && releaseAt.UnixMilli() < at {
						at = releaseAt.UnixMilli()
					}
					if dailyRelease != nil && dailyRelease.UnixMilli() < at {
						at = dailyRelease.UnixMilli()
					}
					violations = append(violations, at)
				}
			}
		}
	}
	decision := SmoothingDecision{
		Held:   len(violations) > 0,
		Factor: 1 / (1 + maxf(0, pressure-policySoftPressureStart)*policySoftPressureWeight),
	}
	if len(violations) > 0 {
		next := violations[0]
		for _, at := range violations[1:] {
			if at > next {
				next = at
			}
		}
		decision.NextAt = &next
	}
	return decision
}

// SmoothingHeld reports whether a model is paced right now.
func (s *Service) SmoothingHeld(entry *Entry, ctx SmoothingContext) bool {
	return s.smoothingDecision(entry, ctx).Held
}

// SmoothingFactor is the score factor pacing applies.
func (s *Service) SmoothingFactor(entry *Entry, ctx SmoothingContext) float64 {
	return s.smoothingDecision(entry, ctx).Factor
}

// SmoothingNextAt is when a paced model could be tried again.
func (s *Service) SmoothingNextAt(entry *Entry, ctx SmoothingContext) *int64 {
	return s.smoothingDecision(entry, ctx).NextAt
}

// ResetSmoothingForTests forgets the caches.
func (s *Service) resetSmoothing() {
	s.smoother.mu.Lock()
	defer s.smoother.mu.Unlock()
	s.smoother.readiness = map[string]cacheEntry[Readiness]{}
	s.smoother.profiles = map[string]cacheEntry[*demandProfile]{}
}
