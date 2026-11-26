package pools

// How much of a shared connection its owner keeps for themself today, from
// their own use of it over the last week: what they usually still send from
// this time of day until midnight (UTC), on a busy day, plus a margin. Partners
// get the rest. A connection the owner hardly uses is almost all theirs to
// share; one the owner leans on stays theirs until they are usually done for
// the day.

import (
	"context"
	"math"
	"sort"
)

const (
	// reserveDays is how many past days the expectation is read from.
	reserveDays = 7
	// busyRank is a busy day, not the busiest: the second highest of the last
	// week, so one unusual spike does not lock partners out for a week.
	busyRank = 2
	// reserveMargin is added on top of the busy day.
	reserveMargin = 1.2
	// minHeadroom is kept free in windows without a daily count.
	minHeadroom = 0.1
	// reserveCacheMS is how long one expectation is reused.
	reserveCacheMS = 5 * 60_000
)

// expectedOwnUse is how many requests the owner is expected to still send on this
// connection today.
func (s *Service) expectedOwnUse(ctx context.Context, model Model, now int64) int64 {
	cacheKey := model.AccountID + "|" + itoa(now/reserveCacheMS)
	s.reserveMu.Lock()
	if cached, ok := s.reserved[cacheKey]; ok {
		s.reserveMu.Unlock()
		return cached
	}
	s.reserveMu.Unlock()

	dayStart := (now / dayMS) * dayMS
	sinceStartOfDay := now - dayStart
	from := dayStart - reserveDays*dayMS
	rows, err := s.DB.R.QueryContext(ctx, `SELECT CAST((r.created_at - ?) / `+itoa(dayMS)+` AS INTEGER) day,
		COUNT(*) requests FROM requests r LEFT JOIN router_keys k ON k.id = r.router_key_id
		WHERE r.user_id = ? AND r.account_id = ? AND r.created_at >= ? AND r.created_at < ?
			AND k.peer_link_id IS NULL AND CAST(r.created_at - ? AS INTEGER) % `+itoa(dayMS)+` >= ?
		GROUP BY day`, from, model.UserID, model.AccountID, from, dayStart, from, sinceStartOfDay)
	if err != nil {
		s.log().Warn("Could not read what a connection's owner uses", "error", err)
		return 0
	}
	defer rows.Close()
	counts := make([]int64, reserveDays)
	for rows.Next() {
		var (
			day      int64
			requests int64
		)
		if err := rows.Scan(&day, &requests); err != nil {
			s.log().Warn("Could not read what a connection's owner uses", "error", err)
			return 0
		}
		if day >= 0 && day < reserveDays {
			counts[day] = requests
		}
	}
	sort.Slice(counts, func(i, j int) bool { return counts[i] > counts[j] })
	expected := int64(math.Ceil(float64(counts[busyRank-1]) * reserveMargin))
	s.reserveMu.Lock()
	if len(s.reserved) > 5000 {
		s.reserved = map[string]int64{}
	}
	s.reserved[cacheKey] = expected
	s.reserveMu.Unlock()
	return expected
}

// keptForOwner is why a partner may not use this connection right now (or
// false). state is the model's quota picture. Daily request counts keep what the
// owner is expected to need today; other windows keep a small margin.
func (s *Service) keptForOwner(ctx context.Context, model Model, state QuotaState, now int64) bool {
	if state.Headroom < minHeadroom {
		return true
	}
	daily := []float64{}
	for _, detail := range state.Details {
		if detail.Metric == "rpd" && detail.Remaining != nil {
			daily = append(daily, *detail.Remaining)
		}
	}
	if len(daily) == 0 {
		return false
	}
	keep := float64(s.expectedOwnUse(ctx, model, now))
	for _, remaining := range daily {
		if remaining-keep < 1 {
			return true
		}
	}
	return false
}

// resetReserveForTests clears the cached expectations.
func (s *Service) resetReserveForTests() {
	s.reserveMu.Lock()
	s.reserved = map[string]int64{}
	s.reserveMu.Unlock()
}
