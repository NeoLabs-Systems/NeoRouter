package routing

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Prepaid plans: a request costs no money, but it spends an allowance that is
// shared with the user's other use of the plan. Routing prices that allowance
// by how fast it is going: nothing when it would expire unused anyway (it does
// not roll over), a little when on schedule, more as it falls behind, so free
// models that are good enough take easy prompts and the plan is kept for
// prompts that need it. It holds back as the allowance runs low and skips the
// plan once it is used up until it renews.
//
// Allowances come from the provider's quota endpoint (probed periodically) or
// response headers as one or more windows: plan_requests counts requests
// (Synthetic), plan_5h / plan_week are percentages left (MiniMax, ChatGPT).
// Requests sent since the last probe are subtracted from counted windows; the
// tightest window wins.
const (
	planMetricRequests = "plan_requests"
	planMetric5h       = "plan_5h"
	planMetricWeek     = "plan_week"
	planMetricMonth    = "plan_month"
	// Running low: hold back.
	planLowShare  = 0.15
	planLowFactor = 0.5
	// What a request from the allowance costs, on the router's cost scale
	// (0 = free, 1 = the priciest models): on schedule or unknown, and far
	// behind. Above the router's "clearly cheaper" step (0.05), so a free model
	// of the same quality wins, and below the cheapest usual paid models, so
	// the plan still comes before paying.
	planOnScheduleCost = 0.08
	planBehindCost     = 0.2
	// Well ahead of schedule (part of it would expire unused): lean on the
	// plan.
	planSurplusBonus = 0.15
	planMinTimeLeft  = 0.02
)

// planMetricNames are the metrics that are plan windows.
var planMetricNames = map[string]bool{
	planMetricRequests: true,
	planMetric5h:       true,
	planMetricWeek:     true,
	planMetricMonth:    true,
}

// planWindowMs are the window lengths, to compare what is left with the time
// still to run.
var planWindowMs = map[string]int64{
	planMetric5h:    5 * hourMs,
	planMetricWeek:  7 * dayMs,
	planMetricMonth: 30 * dayMs,
}

// planSent is how many requests were sent since the last probe of a counted
// window.
type planSent struct {
	since int64
	count float64
}

type planSentStore struct {
	mu   sync.Mutex
	sent map[string]planSent
}

func newPlanSentStore() *planSentStore {
	return &planSentStore{sent: map[string]planSent{}}
}

// planRoom is what is left of the tightest plan window.
type planRoom struct {
	Share   float64
	ResetAt *int64
}

// planSchedule is one window compared with the time that has run.
type planSchedule struct {
	Metric       string
	UsedShare    float64
	ElapsedShare float64
	StartAt      int64
	ResetAt      int64
	RequestShare float64
}

// planWindows are an account's plan windows with a limit.
func (s *Service) planWindows(accountID string) []QuotaWindow {
	if s.opts.Quota == nil {
		return nil
	}
	scope := "a:" + accountID
	var windows []QuotaWindow
	for _, state := range s.opts.Quota.AccountState(accountID) {
		if state.Scope != scope || !planMetricNames[state.Metric] {
			continue
		}
		if state.Limit == nil || *state.Limit == 0 {
			continue
		}
		windows = append(windows, state)
	}
	return windows
}

// remainingOf is what a window has left, with sends since the probe taken off.
func (s *Service) remainingOf(window QuotaWindow, now time.Time) float64 {
	limit := 0.0
	if window.Limit != nil {
		limit = *window.Limit
	}
	if window.ResetAt != nil && *window.ResetAt <= now.UnixMilli() {
		return limit
	}
	remaining := limit
	if window.Remaining != nil {
		remaining = *window.Remaining
	}
	if window.Metric == planMetricRequests {
		s.planSent.mu.Lock()
		counted, ok := s.planSent.sent[strings.TrimPrefix(window.Scope, "a:")]
		s.planSent.mu.Unlock()
		if ok && counted.since == window.ObservedAt {
			remaining -= counted.count
		}
	} else if window.RequestCost != nil && *window.RequestCost > 0 && window.LocalUsed != nil && s.opts.Quota != nil {
		later := s.opts.Quota.UsageInLast(window.Scope, 1440, now).Requests - *window.LocalUsed
		if later < 0 {
			later = 0
		}
		remaining -= later * *window.RequestCost
	}
	return maxf(0, remaining)
}

// planRoomOf is {share: fraction left of the tightest window, resetAt} or nil
// when unknown.
func (s *Service) planRoomOf(entry *Entry, now time.Time) *planRoom {
	if entry.IsFlatRate {
		return nil
	}
	var tightest *planRoom
	for _, window := range s.planWindows(entry.AccountID) {
		limit := 0.0
		if window.Limit != nil {
			limit = *window.Limit
		}
		share := s.remainingOf(window, now) / limit
		if tightest == nil || share < tightest.Share {
			room := &planRoom{Share: share}
			if window.ResetAt != nil && *window.ResetAt > now.UnixMilli() {
				at := *window.ResetAt
				room.ResetAt = &at
			}
			tightest = room
		}
	}
	return tightest
}

// planPace is share left divided by the share of the window still to run, for
// the tightest window: above 1 the allowance is ahead of schedule, below 1 it
// is used faster than it renews. Nil when no window has a known length and
// reset time.
func (s *Service) planPace(entry *Entry, now time.Time) *float64 {
	if entry.IsFlatRate {
		return nil
	}
	nowMs := now.UnixMilli()
	var tightest *float64
	for _, window := range s.planWindows(entry.AccountID) {
		length := planWindowMs[window.Metric]
		if length == 0 || window.ResetAt == nil {
			continue
		}
		renewed := *window.ResetAt <= nowMs
		share := 1.0
		timeLeft := 1.0
		if !renewed {
			limit := 0.0
			if window.Limit != nil {
				limit = *window.Limit
			}
			share = s.remainingOf(window, now) / limit
			timeLeft = minf(1, maxf(planMinTimeLeft, float64(*window.ResetAt-nowMs)/float64(length)))
		}
		ratio := share / timeLeft
		if tightest == nil || ratio < *tightest {
			tightest = &ratio
		}
	}
	return tightest
}

// planSchedules is how far each plan window is used compared with how far it
// has run: usedShare and elapsedShare (both 0 to 1). Smoothing holds a plan
// back while usedShare runs ahead of elapsedShare.
func (s *Service) planSchedules(entry *Entry, now time.Time) []planSchedule {
	if entry.IsFlatRate {
		return nil
	}
	nowMs := now.UnixMilli()
	var out []planSchedule
	for _, window := range s.planWindows(entry.AccountID) {
		length := planWindowMs[window.Metric]
		if length == 0 || window.ResetAt == nil || *window.ResetAt <= nowMs {
			continue
		}
		limit := 0.0
		if window.Limit != nil {
			limit = *window.Limit
		}
		used := 1 - s.remainingOf(window, now)/limit
		elapsed := minf(1, maxf(0, 1-float64(*window.ResetAt-nowMs)/float64(length)))
		requestShare := 0.0
		if window.RequestCost != nil {
			requestShare = *window.RequestCost / limit
		}
		out = append(out, planSchedule{
			Metric:       window.Metric,
			UsedShare:    used,
			ElapsedShare: elapsed,
			StartAt:      *window.ResetAt - length,
			ResetAt:      *window.ResetAt,
			RequestShare: requestShare,
		})
	}
	return out
}

// planSchedule is the window furthest ahead of the time that has run.
func (s *Service) planSchedule(entry *Entry, now time.Time) *planSchedule {
	var worst *planSchedule
	for _, item := range s.planSchedules(entry, now) {
		if worst == nil || item.UsedShare-item.ElapsedShare > worst.UsedShare-worst.ElapsedShare {
			schedule := item
			worst = &schedule
		}
	}
	return worst
}

// planAllowanceCost is the allowance a request spends, priced by planPace (0
// for models outside a plan).
func (s *Service) planAllowanceCost(entry *Entry, now time.Time) float64 {
	if !entry.IsPlan || entry.IsFlatRate {
		return 0
	}
	ratio := s.planPace(entry, now)
	if ratio == nil {
		return planOnScheduleCost
	}
	if *ratio >= 1 {
		return planOnScheduleCost * maxf(0, 2-*ratio)
	}
	return planOnScheduleCost + (planBehindCost-planOnScheduleCost)*(1-*ratio)
}

// planOnSent records that a request was sent on a plan model.
func (s *Service) planOnSent(entry *Entry, now time.Time) {
	if !entry.IsPlan || entry.IsFlatRate {
		return
	}
	if s.opts.Quota != nil {
		s.opts.Quota.RecordPlanRequest(entry.AccountID, now)
	}
	var counted *QuotaWindow
	for _, window := range s.planWindows(entry.AccountID) {
		if window.Metric == planMetricRequests {
			window := window
			counted = &window
			break
		}
	}
	if counted == nil {
		return
	}
	s.planSent.mu.Lock()
	defer s.planSent.mu.Unlock()
	previous, ok := s.planSent.sent[entry.AccountID]
	if ok && previous.since == counted.ObservedAt {
		s.planSent.sent[entry.AccountID] = planSent{since: counted.ObservedAt, count: previous.count + 1}
		return
	}
	s.planSent.sent[entry.AccountID] = planSent{since: counted.ObservedAt, count: 1}
}

func planUsedUp(left *planRoom) bool { return left != nil && left.Share <= 0 }

// planEffective is the entry as routing should see it now: once a "plan, then
// pay per use" plan is used up, its models are billed like any paid model.
func (s *Service) planEffective(entry *Entry, now time.Time) *Entry {
	if !entry.IsPlan || !entry.PlanOverflow {
		return entry
	}
	if planUsedUp(s.planRoomOf(entry, now)) {
		copied := *entry
		copied.IsPlan = false
		return &copied
	}
	return entry
}

// planRejection is 'plan_used_up' when a plan-only account has nothing left.
func (s *Service) planRejection(entry *Entry, now time.Time) string {
	if !entry.IsPlan {
		return ""
	}
	if planUsedUp(s.planRoomOf(entry, now)) {
		return "plan_used_up"
	}
	return ""
}

// planFactor is the score factor of a plan model: held back when the allowance
// runs low, preferred when well ahead of schedule.
func (s *Service) planFactor(entry *Entry, now time.Time) float64 {
	if !entry.IsPlan {
		return 1
	}
	if left := s.planRoomOf(entry, now); left != nil && left.Share < planLowShare {
		return planLowFactor
	}
	if ratio := s.planPace(entry, now); ratio != nil && *ratio > 1 {
		return 1 + planSurplusBonus*minf(1, *ratio-1)
	}
	return 1
}

// plansObserveHeaders learns plan windows from a response's headers. ChatGPT
// (Codex) reports its plan windows on every response:
// x-codex-primary-used-percent, -window-minutes, -reset-at (Unix seconds), and
// the same for "secondary" (usually 5 hours and a week).
func (s *Service) plansObserveHeaders(entry *Entry, headers http.Header, now time.Time) {
	if headers == nil || s.opts.Quota == nil {
		return
	}
	for _, slot := range []string{"primary", "secondary"} {
		raw := headers.Get("x-codex-" + slot + "-used-percent")
		if raw == "" {
			continue
		}
		used, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		minutes := 0.0
		if value, err := strconv.ParseFloat(headers.Get("x-codex-"+slot+"-window-minutes"), 64); err == nil {
			minutes = value
		}
		var resetAt *int64
		if seconds, err := strconv.ParseFloat(headers.Get("x-codex-"+slot+"-reset-at"), 64); err == nil && seconds != 0 {
			ms := int64(seconds * 1000)
			resetAt = &ms
		}
		metric := planMetricMonth
		if minutes > 0 && minutes <= 6*60 {
			metric = planMetric5h
		} else if minutes > 0 && minutes <= 8*24*60 {
			metric = planMetricWeek
		}
		limit := 100.0
		remaining := maxf(0, 100-used)
		s.opts.Quota.SetState("a:"+entry.AccountID, metric, QuotaWindow{Limit: &limit, Remaining: &remaining, ResetAt: resetAt}, "headers", now)
	}
}
