package routing

import "time"

// "This connection works again": a person fixed what was wrong (paid, signed
// up, waited out a ban). Everything the router held against the connection is
// dropped, and its next few failures are forgiven, so it is really tried again
// even though it hardly answered in the last days.

// FixedResult says what marking a connection as fixed did.
type FixedResult struct {
	// Models is how many of its models got a clean slate.
	Models int `json:"models"`
	// Chances is how many failures are forgiven next.
	Chances int `json:"chances"`
}

// MarkAccountFixed gives a connection a fair new start.
func (s *Service) MarkAccountFixed(accountID string, now time.Time) FixedResult {
	if now.IsZero() {
		now = time.Now()
	}
	refs := []int64{}
	if s.opts.DB != nil {
		if rows, err := s.opts.DB.R.Query(`SELECT id FROM models WHERE account_id = ?`, accountID); err == nil {
			for rows.Next() {
				var ref int64
				if rows.Scan(&ref) == nil {
					refs = append(refs, ref)
				}
			}
			rows.Close()
		}
	}
	for _, ref := range refs {
		s.stats.forgive(ref, now)
	}
	s.health.markFixed(accountID)
	s.conc.forget(accountID)
	if s.opts.Quota != nil {
		s.opts.Quota.ClearObservedExhaustion(accountID, refs)
	}
	return FixedResult{Models: len(refs), Chances: s.health.chancesLeft(accountID)}
}
