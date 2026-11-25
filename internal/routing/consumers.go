package routing

import "time"

func (s *Service) estimateMaxCost(entry *Entry, inputTokens, maxOutputTokens int) *float64 {
	if s.opts.Budget == nil {
		return nil
	}
	return s.opts.Budget.EstimateMaxCost(entry, inputTokens, maxOutputTokens)
}

func (s *Service) budgetRemaining(key RouterKey, now time.Time) *float64 {
	if s.opts.Budget == nil {
		return nil
	}
	return s.opts.Budget.Remaining(key, now)
}

func (s *Service) tokenLimitReached(key RouterKey, now time.Time) bool {
	if s.opts.Budget == nil {
		return false
	}
	return s.opts.Budget.TokenLimitReached(key, now)
}

func (s *Service) pacingFor(key RouterKey, now time.Time) *Pacing {
	if s.opts.Budget == nil {
		return nil
	}
	return s.opts.Budget.Pacing(key, now)
}

func (s *Service) isPeerEntry(entry *Entry) bool {
	if s.opts.Pools == nil {
		return entry.DefinitionID == peerDefinition
	}
	return s.opts.Pools.IsPeerEntry(entry)
}

// quotaEvaluate keeps the quota consumer optional: without one, nothing is
// throttled by quota.
func (s *Service) quotaEvaluate(entry *Entry, estimatedTokens int, now time.Time) QuotaEvaluation {
	if s.opts.Quota == nil {
		return QuotaEvaluation{Headroom: 1, Factor: 1}
	}
	return s.opts.Quota.Evaluate(entry, estimatedTokens, now)
}
