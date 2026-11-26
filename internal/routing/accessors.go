package routing

import "time"

// PlanSent records that a request was sent on a plan model (plans.onSent in
// the Node server). The live gateway calls it when a session opens; the chat
// executor calls it through the plan itself.
func (s *Service) PlanSent(entry *Entry) {
	if entry == nil {
		return
	}
	s.planOnSent(entry, time.Now())
}

// LimitFor is the account's concurrency limit as this entry sees it, or nil
// when unknown (concurrency.limitFor). The accounts and pools domains read it
// instead of keeping their own copy of the learning rules.
func (s *Service) LimitFor(entry *Entry, now time.Time) *ConcurrencyLimit {
	return s.conc.limitFor(entry, now)
}
