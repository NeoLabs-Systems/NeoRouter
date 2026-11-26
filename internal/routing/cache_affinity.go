package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Which model answered a conversation last, so a follow-up reuses it and the
// provider's prompt cache keeps hitting. Only a digest of the session is kept,
// in memory.
type affinityEntry struct {
	ref int64
	at  int64
}

type affinityStore struct {
	mu       sync.Mutex
	sessions map[string]affinityEntry
	order    []string
}

func newAffinityStore() *affinityStore {
	return &affinityStore{sessions: map[string]affinityEntry{}}
}

func affinityDigest(scope, session string) string {
	sum := sha256.Sum256([]byte(scope + "\n" + session))
	return hex.EncodeToString(sum[:])
}

// CachedTokens is cache_affinity.cachedTokens: the cached part of a usage
// block, never more than the prompt.
func CachedTokens(promptTokens int, cachedTokens int) int {
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	if cachedTokens > promptTokens {
		return promptTokens
	}
	return cachedTokens
}

// RememberAffinity records which model answered a conversation.
func (s *Service) RememberAffinity(scope, session string, entry *Entry, now time.Time) {
	if session == "" || entry == nil {
		return
	}
	cfg := s.routingCfg()
	nowMs := now.UnixMilli()
	ttl := cfg.CacheAffinityTTL.Milliseconds()
	s.affinity.mu.Lock()
	defer s.affinity.mu.Unlock()
	for key, value := range s.affinity.sessions {
		if nowMs-value.at > ttl {
			delete(s.affinity.sessions, key)
		}
	}
	key := affinityDigest(scope, session)
	delete(s.affinity.sessions, key)
	s.affinity.sessions[key] = affinityEntry{ref: entry.Ref, at: nowMs}
	s.affinity.order = append(s.affinity.order, key)
	for int64(len(s.affinity.sessions)) > int64(cfg.CacheAffinitySessions) && len(s.affinity.order) > 0 {
		oldest := s.affinity.order[0]
		s.affinity.order = s.affinity.order[1:]
		delete(s.affinity.sessions, oldest)
	}
}

// AffinityRef is the model that answered this conversation recently, nil when
// there is none.
func (s *Service) AffinityRef(scope, session string, now time.Time) *int64 {
	if session == "" {
		return nil
	}
	s.affinity.mu.Lock()
	hit, ok := s.affinity.sessions[affinityDigest(scope, session)]
	s.affinity.mu.Unlock()
	if !ok {
		return nil
	}
	if now.UnixMilli()-hit.at > s.routingCfg().CacheAffinityTTL.Milliseconds() {
		return nil
	}
	ref := hit.ref
	return &ref
}

// CacheRates is the recent measured share of input served from cache per model.
// Unknown and zero-hit models receive zero benefit. A rolling window lets
// changed providers relearn.
func (s *Service) CacheRates(userID string, now time.Time) map[int64]float64 {
	rates := map[int64]float64{}
	if s.opts.DB == nil {
		return rates
	}
	since := now.UnixMilli() - s.routingCfg().CacheLearningWindow.Milliseconds()
	rows, err := s.opts.DB.R.Query(`SELECT model_ref, SUM(cached_input_tokens) cached, SUM(input_tokens) input
		FROM requests WHERE user_id = ? AND status = 'success' AND created_at >= ? GROUP BY model_ref`, userID, since)
	if err != nil {
		return rates
	}
	defer rows.Close()
	for rows.Next() {
		var ref int64
		var cached, input float64
		if err := rows.Scan(&ref, &cached, &input); err != nil {
			continue
		}
		if input > 0 {
			rates[ref] = minf(1, cached/input)
		} else {
			rates[ref] = 0
		}
	}
	return rates
}

// CacheBonus is what a cache hit is worth on the score: proportional to the
// hit rate, the size of the input and the discount the provider gives for
// cached input.
func (s *Service) CacheBonus(entry *Entry, hitRate float64, inputTokens int) float64 {
	if hitRate <= 0 {
		return 0
	}
	cfg := s.routingCfg()
	discount := 0.0
	if entry.PriceCachedIn != nil && entry.PriceIn != nil && *entry.PriceIn > 0 {
		discount = maxf(0, 1 - *entry.PriceCachedIn / *entry.PriceIn)
	}
	return cfg.CacheAffinityMaxBonus * hitRate * (float64(inputTokens) / float64(inputTokens+cfg.CacheAffinityScale)) * (1 + discount) / 2
}
