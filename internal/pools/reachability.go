package pools

// Whether a pool partner answered the last time we tried, so the pool page
// shows "not reachable" as soon as a partner goes down, and "online" again as
// soon as it answers. Kept in memory: a restart starts optimistic.

import (
	"github.com/NeoLabs-Systems/neorouter/internal/providers"
)

// unreachableKinds are the failures that mean a partner could not be reached.
var unreachableKinds = map[providers.Kind]bool{
	providers.KindNetwork: true,
	providers.KindTimeout: true,
}

// forgetAfterMS is how long a failure counts.
const forgetAfterMS = 30 * 60_000

// NotePartnerSuccess records that a partner answered.
func (s *Service) NotePartnerSuccess(accountID string) {
	if accountID == "" {
		return
	}
	s.reachMu.Lock()
	delete(s.down, accountID)
	s.reachMu.Unlock()
}

// NotePartnerFailure records that a partner did not answer.
func (s *Service) NotePartnerFailure(accountID string, kind providers.Kind, now int64) {
	if accountID == "" || !unreachableKinds[kind] {
		return
	}
	s.reachMu.Lock()
	s.down[accountID] = now
	s.reachMu.Unlock()
}

// PartnerDown reports whether a partner counted as unreachable lately.
func (s *Service) PartnerDown(accountID string, now int64) bool {
	if accountID == "" {
		return false
	}
	s.reachMu.Lock()
	defer s.reachMu.Unlock()
	since, ok := s.down[accountID]
	if !ok {
		return false
	}
	if now-since > forgetAfterMS {
		delete(s.down, accountID)
		return false
	}
	return true
}
