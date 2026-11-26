package routing

import "time"

// Prepaid slots: a flat-rate subscription takes a fixed number of requests at
// once, and what happens beyond that is the connection's overflow policy
// (slot_plans.js).

// SourceTiers is how many source tiers slotPlans.SOURCE_TIERS reserves.
const SourceTiers = 5

// slotSourceTier ranks a model's source: a paid-for slot first, then a plan
// that is used behind schedule, a plan on schedule, free, and paid last.
func (s *Service) slotSourceTier(entry *Entry, now time.Time) int {
	if entry.IsFlatRate {
		return 0
	}
	if entry.IsPlan {
		if pace := s.planPace(entry, now); pace != nil && *pace > 1 {
			return 1
		}
		return 2
	}
	if entry.IsFree {
		return 3
	}
	return 4
}

// slotWaitMs is how long a connection may be waited for.
func (s *Service) slotWaitMs(entry *Entry) int64 {
	if entry.SlotWaitMs != nil {
		return int64(*entry.SlotWaitMs)
	}
	return s.routingCfg().SlotQueueWait.Milliseconds()
}

// slotDeadline is the latest a request may wait for this connection.
func (s *Service) slotDeadline(entry *Entry, startedAt time.Time) int64 {
	cfg := s.routingCfg()
	room := cfg.TotalBudget.Milliseconds() - cfg.SlotOverflowReserve.Milliseconds()
	if room < 0 {
		room = 0
	}
	return startedAt.UnixMilli() + minInt64(s.slotWaitMs(entry), room)
}

// slotBusyReason says why a model at its account's concurrency limit cannot
// take more work right now.
func (s *Service) slotBusyReason(entry *Entry, startedAt, now time.Time) string {
	if !entry.IsFlatRate {
		return "account_busy"
	}
	if entry.OverflowPolicy == "overflow" || now.UnixMilli() >= s.slotDeadline(entry, startedAt) {
		return "slot_overflow"
	}
	return "account_busy"
}

// slotComparable reports whether two models are interchangeable for a waiting
// request: the same model, or of about the same intelligence.
func (s *Service) slotComparable(candidate, busy *Entry) bool {
	if candidate.CanonicalID != "" && candidate.CanonicalID == busy.CanonicalID {
		return true
	}
	return s.trustedIntelligence(candidate)+s.routingCfg().SlotComparableGap >= s.trustedIntelligence(busy)
}

// slotWaitUntil is when the request may try again, or nil when it should not
// wait. Explicit queueing waits up to the account's bound. Auto waits only
// when there is no comparable free alternative or eligible subscription
// overflow.
func (s *Service) slotWaitUntil(busyEntries []*Entry, available []*Candidate, startedAt, now time.Time, useAvailablePlan bool) *int64 {
	for _, item := range available {
		if item.Entry.IsFlatRate {
			return nil
		}
	}
	planAvailable := false
	if useAvailablePlan {
		for _, item := range available {
			if item.Entry.IsPlan {
				planAvailable = true
				break
			}
		}
	}
	var until *int64
	for _, entry := range busyEntries {
		untilAt := s.slotDeadline(entry, startedAt)
		if entry.OverflowPolicy == "overflow" || now.UnixMilli() >= untilAt {
			continue
		}
		// A routing group can use another included subscription without waiting
		// for its preferred connection. An explicit queue policy still applies.
		if entry.OverflowPolicy != "queue" && planAvailable {
			continue
		}
		if entry.OverflowPolicy != "queue" {
			comparableFree := false
			for _, item := range available {
				if item.Entry.IsFree && s.slotComparable(item.Entry, entry) {
					comparableFree = true
					break
				}
			}
			if comparableFree {
				continue
			}
		}
		if until == nil || untilAt < *until {
			value := untilAt
			until = &value
		}
	}
	return until
}
