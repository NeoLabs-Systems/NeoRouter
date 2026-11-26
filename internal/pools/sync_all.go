package pools

import "context"

// SyncAll picks up members who joined or left while this server was away: it
// compares every pool with its host's signed member list (pool_service.syncAll,
// the scheduler's sync-pools task). A failing pool is logged and skipped, as in
// the Node server.
func (s *Service) SyncAll(ctx context.Context) {
	for _, membership := range s.poolMemberships(ctx) {
		if err := s.sync(ctx, membership.UserID, membership.PoolID); err != nil {
			s.log().Warn("Pool sync failed", "error", err)
		}
	}
}
