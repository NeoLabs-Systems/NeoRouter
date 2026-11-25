package proxies

// Retained per-proxy transports: pool.js keeps one undici dispatcher per proxy
// and bounds the cache, releasing idle ones gracefully so running streams finish.

import "time"

// --- dispatchers --------------------------------------------------------

// dispatcherFor returns the retained transport for row, evicting idle ones and
// capping the cache the way pool.agentFor bounds it.
func (s *Service) dispatcherFor(row proxyRow) (*dispatcher, error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cached := range s.agents {
		if id == row.ID {
			continue
		}
		if now.Sub(cached.usedAt) > s.pol.idleDuration() {
			s.disposeLocked(id)
		}
	}
	cached, ok := s.agents[row.ID]
	if !ok {
		for len(s.agents) >= s.pol.MaxDispatchers {
			s.evictOldestLocked()
		}
		created, err := s.newDispatcher(row)
		if err != nil {
			return nil, err
		}
		s.agents[row.ID] = created
		cached = created
	}
	cached.usedAt = now
	return cached, nil
}

func (s *Service) evictOldestLocked() {
	oldestID := ""
	var oldest time.Time
	for id, cached := range s.agents {
		if oldestID == "" || cached.usedAt.Before(oldest) {
			oldestID, oldest = id, cached.usedAt
		}
	}
	if oldestID != "" {
		s.disposeLocked(oldestID)
	}
}

// disposeLocked drops a retained transport. A graceful release only closes idle
// connections; Go cannot abort the streams an ungraceful destroy() would.
func (s *Service) disposeLocked(id string) {
	cached, ok := s.agents[id]
	if !ok {
		return
	}
	delete(s.agents, id)
	cached.close(true)
}
