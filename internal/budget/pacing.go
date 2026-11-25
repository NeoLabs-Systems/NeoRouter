package budget

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
)

// Budget pacing: a key with a daily or monthly budget should still have money
// at the end of the period. From the spend so far and the key's recent usage,
// pacing forecasts the spend at period end. Over pace, routing leans toward
// cheaper models and caps what one request may cost; well under pace, it may
// spend more on better models. A one-time budget has no end date, so only the
// per-request cap (a share of what is left) applies.
const (
	historyWindowMS  = int64(7 * 86_400_000)
	minObservedHours = 6.0
	historyCacheMS   = int64(30_000)
	// Cost-weight multiplier bounds: from "spend freely" to "cheapest first".
	minCostShift = 0.6
	maxCostShift = 4.0
	// A single request may cost this many times its fair share of what is left.
	perRequestHeadroom = 4.0
	oneTimeShare       = 0.25
)

// Pacing states, worst last.
const (
	PacingOver    = "over"
	PacingTight   = "tight"
	PacingOnTrack = "on_track"
	PacingAhead   = "ahead"
	PacingNone    = "none"
)

// Pace is how a key's budget is pacing through its period.
type Pace struct {
	State         string   `json:"state"`
	CostShift     float64  `json:"costShift"`
	PerRequestCap float64  `json:"perRequestCap"`
	Spent         float64  `json:"spent"`
	Remaining     float64  `json:"remaining"`
	Forecast      *float64 `json:"forecast"`
	ResetsAt      *int64   `json:"resetsAt"`
}

type historyEntry struct {
	at         int64
	since      int64
	requests   int64
	firstAt    *int64
	spend      float64
	spendFirst *int64
}

func forgetPacing(s *Service) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	clear(s.history)
}

// Pacing is a key's budget pacing, nil when the key has no budget.
func (s *Service) Pacing(key Key, now time.Time) (*Pace, error) {
	if key.BudgetUSD == nil {
		return nil, nil
	}
	return s.pace(key, now)
}

// recentHistory is the week of history, cached briefly: it changes slowly,
// while the spend in the current period is always read fresh.
func (s *Service) recentHistory(keyID string, now int64) (historyEntry, error) {
	s.historyMu.Lock()
	cached, ok := s.history[keyID]
	s.historyMu.Unlock()
	if ok && now-cached.at < historyCacheMS {
		return cached, nil
	}
	since := now - historyWindowMS
	var entry historyEntry
	var first, spendFirst sql.NullInt64
	err := s.DB.R.QueryRowContext(context.Background(),
		`SELECT COUNT(*) requests, MIN(created_at) first FROM requests WHERE router_key_id = ? AND created_at >= ?`,
		keyID, since).Scan(&entry.requests, &first)
	if err != nil {
		return historyEntry{}, fmt.Errorf("budget: read request history: %w", err)
	}
	err = s.DB.R.QueryRowContext(context.Background(),
		`SELECT COALESCE(SUM(amount_usd), 0) total, MIN(created_at) first FROM budget_ledger WHERE router_key_id = ? AND created_at >= ?`,
		keyID, since).Scan(&entry.spend, &spendFirst)
	if err != nil {
		return historyEntry{}, fmt.Errorf("budget: read spend history: %w", err)
	}
	entry.at, entry.since = now, since
	if first.Valid {
		entry.firstAt = &first.Int64
	}
	if spendFirst.Valid {
		entry.spendFirst = &spendFirst.Int64
	}
	s.historyMu.Lock()
	s.history[keyID] = entry
	s.historyMu.Unlock()
	return entry, nil
}

func (s *Service) pace(key Key, now time.Time) (*Pace, error) {
	nowMS := now.UnixMilli()
	spent, err := s.Spent(key, now)
	if err != nil {
		return nil, err
	}
	remaining := math.Max(0, *key.BudgetUSD-spent)
	end := PeriodEnd(key.BudgetPeriod, now)
	recent, err := s.recentHistory(key.ID, nowMS)
	if err != nil {
		return nil, err
	}
	// A few minutes of history would extrapolate wildly; count at least
	// minObservedHours.
	firstSeen := int64(nowMS)
	if recent.firstAt != nil {
		firstSeen = *recent.firstAt
	}
	if recent.spendFirst != nil && *recent.spendFirst < firstSeen {
		firstSeen = *recent.spendFirst
	}
	from := recent.since
	if firstSeen > from {
		from = firstSeen
	}
	observedHours := math.Max(minObservedHours, float64(nowMS-from)/3_600_000)
	requestsPerHour := float64(recent.requests) / observedHours
	spendPerHour := recent.spend / observedHours

	if end == nil {
		// One-time budget: no forecast, just keep any single request modest.
		state := PacingNone
		if remaining <= 0 {
			state = PacingOver
		}
		return &Pace{State: state, CostShift: 1, PerRequestCap: remaining * oneTimeShare, Spent: spent, Remaining: remaining}, nil
	}
	start := PeriodStart(key.BudgetPeriod, now)
	hoursLeft := math.Max(0.25, float64(*end-nowMS)/3_600_000)
	elapsed := math.Max(1.0/60.0, float64(nowMS-start)/3_600_000)
	// Forecast: blend this period's own rate with the weekly history, so a new
	// period with little data still has a sensible guess.
	periodRate := spent / elapsed
	rate := periodRate
	if recent.spend > 0 {
		rate = 0.5*periodRate + 0.5*spendPerHour
	}
	forecast := spent + rate*hoursLeft
	ratio := math.Inf(1)
	if *key.BudgetUSD > 0 {
		ratio = forecast / *key.BudgetUSD
	}
	// Smooth and monotone: 0.6 with nothing spent, 1 exactly on pace, 4 at
	// twice the budget.
	costShift := math.Min(maxCostShift, minCostShift+(1-minCostShift)*ratio)
	if ratio > 1 {
		costShift = math.Min(maxCostShift, ratio*ratio)
	}
	expectedRequests := math.Max(1, requestsPerHour*hoursLeft)
	state := PacingOnTrack
	switch {
	case remaining <= 0:
		state = PacingOver
	case ratio > 1.05:
		state = PacingTight
	case ratio < 0.5:
		state = PacingAhead
	}
	return &Pace{
		State:         state,
		CostShift:     costShift,
		PerRequestCap: (remaining / expectedRequests) * perRequestHeadroom,
		Spent:         spent,
		Remaining:     remaining,
		Forecast:      &forecast,
		ResetsAt:      end,
	}, nil
}
