package routing

import (
	"sync"
	"time"
)

// In-flight request counts per model and per provider account. Routing reads
// them synchronously so a burst of requests spreads out immediately, before any
// of them finishes and updates the learned stats.
type loadTracker struct {
	mu        sync.Mutex
	byModel   map[int64]int
	byAccount map[string]int
}

func newLoadTracker() *loadTracker {
	return &loadTracker{byModel: map[int64]int{}, byAccount: map[string]int{}}
}

func (l *loadTracker) addModel(ref int64, delta int) {
	next := l.byModel[ref] + delta
	if next <= 0 {
		delete(l.byModel, ref)
		return
	}
	l.byModel[ref] = next
}

func (l *loadTracker) addAccount(accountID string, delta int) int {
	next := l.byAccount[accountID] + delta
	if next <= 0 {
		delete(l.byAccount, accountID)
		return 0
	}
	l.byAccount[accountID] = next
	return next
}

// LoadCounts is how many requests are in flight for a model and its account.
type LoadCounts struct {
	Model   int `json:"model"`
	Account int `json:"account"`
}

func (l *loadTracker) inflight(entry *Entry) LoadCounts {
	if entry == nil {
		return LoadCounts{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return LoadCounts{Model: l.byModel[entry.Ref], Account: l.byAccount[entry.AccountID]}
}

// Lease marks one request in flight on a model and its account. The returned
// function releases it (idempotent), as load_tracker.lease does.
func (s *Service) Lease(entry *Entry) func() {
	if entry == nil {
		return func() {}
	}
	s.load.mu.Lock()
	s.load.addModel(entry.Ref, 1)
	account := s.load.addAccount(entry.AccountID, 1)
	s.load.mu.Unlock()
	s.slots.transition(entry, account, time.Now())
	released := false
	return func() {
		if released {
			return
		}
		released = true
		s.load.mu.Lock()
		s.load.addModel(entry.Ref, -1)
		account := s.load.addAccount(entry.AccountID, -1)
		s.load.mu.Unlock()
		s.slots.transition(entry, account, time.Now())
	}
}

// Inflight is how many requests are running on a model and its account.
func (s *Service) Inflight(entry *Entry) LoadCounts { return s.load.inflight(entry) }

// LoadSnapshot is what the router is running right now, for GET /routing/load.
type LoadSnapshot struct {
	Models   map[string]int `json:"models"`
	Accounts map[string]int `json:"accounts"`
}

// Load returns the in-flight counts per model ref and per account.
func (s *Service) Load() LoadSnapshot {
	s.load.mu.Lock()
	defer s.load.mu.Unlock()
	out := LoadSnapshot{Models: make(map[string]int, len(s.load.byModel)), Accounts: make(map[string]int, len(s.load.byAccount))}
	for ref, count := range s.load.byModel {
		out.Models[formatRef(ref)] = count
	}
	for account, count := range s.load.byAccount {
		out.Accounts[account] = count
	}
	return out
}
