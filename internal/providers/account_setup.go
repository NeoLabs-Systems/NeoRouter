package providers

// What follows adding a connection: list its models, check how it is billed
// and, for a plan, how much of the allowance is left. A failed model list is
// reported, not thrown; the connection is kept and synced again later.

import (
	"context"
)

// FinishSetup runs the checks a new or re-keyed connection needs and returns the
// catalog's sync report, or the error object the API reports instead of failing.
func (s *Service) FinishSetup(ctx context.Context, accountID string) any {
	var sync any
	if s.Catalog != nil {
		result, err := s.Catalog.SyncAccount(ctx, accountID)
		if err != nil {
			sync = map[string]any{"error": err.Error()}
			s.log().Warn("Initial model sync failed", "accountId", accountID, "error", err)
		} else {
			sync = result
		}
	}
	if _, err := s.VerifyAccount(ctx, accountID); err != nil {
		s.log().Warn("Billing check failed", "accountId", accountID, "error", err)
	}
	// A plan's allowance should be known before the first request; a provider
	// that reports nothing is fine.
	if _, err := s.ProbeQuota(ctx, accountID); err != nil {
		s.log().Debug("Quota probe failed", "accountId", accountID, "error", err)
	}
	return sync
}
