// Package budget owns NeoRouter's spend ledger: what a router key spent in its
// budget period, the worst-case cost of a request in flight (reserved so
// concurrent requests cannot jointly overshoot), and the token allowance on
// free and subscription models.
package budget

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// Key is the router key a budget belongs to. Nil money and token limits mean
// "no limit".
type Key struct {
	ID           string
	BudgetUSD    *float64
	BudgetPeriod string // "total", "daily" or "monthly"
	TokenLimit   *int64
	TokenPeriod  string // "total", "daily" or "monthly"
}

// Price is the price picture of a catalog entry the budget maths needs.
type Price struct {
	IsFree          bool
	IsPlan          bool
	PriceIn         *float64
	PriceOut        *float64
	PriceCachedIn   *float64
	PriceCacheWrite *float64
}

// Usage is one request's normalized token usage: the gateway fills CachedTokens
// with the part of the prompt the provider served from its cache.
type Usage struct {
	PromptTokens             int64
	CompletionTokens         int64
	CachedTokens             int64
	CacheCreationInputTokens int64
}

const (
	// reservationEpsilon is the smallest amount worth keeping a reservation for.
	reservationEpsilon = 1e-12
	// perMillion turns a per-million-token price into a cost.
	perMillion = 1e6
)

// Service is the budget ledger. Every method is safe for concurrent use.
type Service struct {
	DB  *dbx.DB
	Now func() time.Time

	mu           sync.Mutex
	reservations map[string]float64

	historyMu sync.Mutex
	history   map[string]historyEntry
}

// New builds the ledger.
func New(db *dbx.DB) *Service {
	return &Service{DB: db, Now: time.Now, reservations: map[string]float64{}, history: map[string]historyEntry{}}
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.Now = now
	}
}

// PeriodStart is when the key's budget period began, 0 for a one-time budget.
func PeriodStart(period string, now time.Time) int64 {
	switch period {
	case "daily":
		return time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
	case "monthly":
		return time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	default:
		return 0
	}
}

// PeriodEnd is when the key's budget period ends, nil for a one-time budget.
func PeriodEnd(period string, now time.Time) *int64 {
	switch period {
	case "daily":
		end := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day()+1, 0, 0, 0, 0, time.UTC).UnixMilli()
		return &end
	case "monthly":
		end := time.Date(now.UTC().Year(), now.UTC().Month()+1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
		return &end
	default:
		return nil
	}
}

// Spent is the sum of the key's ledger rows in its current budget period.
func (s *Service) Spent(key Key, now time.Time) (float64, error) {
	var total float64
	err := s.DB.R.QueryRowContext(context.Background(),
		`SELECT COALESCE(SUM(amount_usd), 0) total FROM budget_ledger WHERE router_key_id = ? AND created_at >= ?`,
		key.ID, PeriodStart(key.BudgetPeriod, now)).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("budget: read spend: %w", err)
	}
	return total, nil
}

// TokensUsed is the tokens the key used on models that cost it nothing per
// request (free and subscription models) in its token period.
func (s *Service) TokensUsed(key Key, now time.Time) (int64, error) {
	period := key.TokenPeriod
	if period == "" {
		period = "monthly"
	}
	var total float64
	err := s.DB.R.QueryRowContext(context.Background(), `SELECT COALESCE(SUM(input_tokens + output_tokens), 0) total FROM requests
		WHERE router_key_id = ? AND created_at >= ? AND status = 'success' AND cost_usd = 0`,
		key.ID, PeriodStart(period, now)).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("budget: read token use: %w", err)
	}
	return int64(total), nil
}

// TokenLimitReached reports whether the key's token allowance is used up, so
// free and subscription models stop.
func (s *Service) TokenLimitReached(key Key, now time.Time) (bool, error) {
	if key.TokenLimit == nil {
		return false, nil
	}
	used, err := s.TokensUsed(key, now)
	if err != nil {
		return false, err
	}
	return used >= *key.TokenLimit, nil
}

// Reserved is the worst-case cost of the requests in flight for a key.
func (s *Service) Reserved(keyID string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reservations[keyID]
}

// Remaining is what the key may still spend in its period, nil without a
// budget.
func (s *Service) Remaining(key Key, now time.Time) (*float64, error) {
	if key.BudgetUSD == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining, err := s.remainingLocked(key, now)
	if err != nil {
		return nil, err
	}
	return &remaining, nil
}

// remainingLocked is Remaining for a key with a budget, with mu held, so a
// settling request is never counted neither as spent nor as reserved.
func (s *Service) remainingLocked(key Key, now time.Time) (float64, error) {
	spent, err := s.Spent(key, now)
	if err != nil {
		return 0, err
	}
	return *key.BudgetUSD - spent - s.reservations[key.ID], nil
}

// CostOf is what a request cost at the entry's prices, 0 for free and
// subscription models and for models without prices.
func CostOf(entry Price, usage *Usage) float64 {
	if entry.IsFree || entry.IsPlan || usage == nil {
		return 0
	}
	if entry.PriceIn == nil || entry.PriceOut == nil {
		return 0
	}
	priceIn, priceOut := *entry.PriceIn, *entry.PriceOut
	cached := usage.CachedTokens
	creation := math.Min(math.Max(0, float64(usage.CacheCreationInputTokens)), math.Max(0, float64(usage.PromptTokens-cached)))
	cachedRate := priceIn
	if entry.PriceCachedIn != nil {
		cachedRate = *entry.PriceCachedIn
	}
	creationRate := priceIn
	if entry.PriceCacheWrite != nil {
		creationRate = *entry.PriceCacheWrite
	}
	billable := float64(usage.PromptTokens-cached) - creation
	return (billable*priceIn + float64(cached)*cachedRate + creation*creationRate + float64(usage.CompletionTokens)*priceOut) / perMillion
}

// EstimateMaxCost is the worst case a request may cost: its input tokens plus
// the most output it may produce. Nil when the entry has no prices.
func EstimateMaxCost(entry Price, inputTokens, maxOutputTokens int64) *float64 {
	if entry.IsFree || entry.IsPlan {
		return float64Ptr(0)
	}
	if entry.PriceIn == nil || entry.PriceOut == nil {
		return nil
	}
	// Cache writes may cost more than ordinary input; reserve the worst case.
	inputPrice := *entry.PriceIn
	if entry.PriceCacheWrite != nil && *entry.PriceCacheWrite > inputPrice {
		inputPrice = *entry.PriceCacheWrite
	}
	estimate := (float64(inputTokens)*inputPrice + float64(maxOutputTokens)*(*entry.PriceOut)) / perMillion
	return &estimate
}

func float64Ptr(v float64) *float64 { return &v }

// Reservation holds a request's worst-case cost until it is settled or
// released.
type Reservation struct {
	service *Service
	key     Key
	amount  float64

	mu   sync.Mutex
	done bool
}

// Reserve holds amount against the key's budget. An amount of zero reserves
// nothing and still yields a usable reservation.
func (s *Service) Reserve(key Key, amount float64) *Reservation {
	reservation := &Reservation{service: s, key: key, amount: amount}
	if amount == 0 {
		return reservation
	}
	s.mu.Lock()
	s.reservations[key.ID] += amount
	s.mu.Unlock()
	return reservation
}

// TryReserve is Reserve only while the key's budget still covers amount, checked
// and held in one step so concurrent requests cannot jointly overshoot. A key
// without a budget, or an amount of zero, always fits.
func (s *Service) TryReserve(key Key, amount float64, now time.Time) (*Reservation, bool, error) {
	if key.BudgetUSD == nil || amount == 0 {
		return s.Reserve(key, amount), true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remaining, err := s.remainingLocked(key, now)
	if err != nil {
		return nil, false, err
	}
	if amount > remaining {
		return nil, false, nil
	}
	s.reservations[key.ID] += amount
	return &Reservation{service: s, key: key, amount: amount}, true, nil
}

// Release gives the reserved amount back without recording anything.
func (r *Reservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	if r.amount == 0 {
		return
	}
	r.service.mu.Lock()
	defer r.service.mu.Unlock()
	next := r.service.reservations[r.key.ID] - r.amount
	if next <= reservationEpsilon {
		delete(r.service.reservations, r.key.ID)
		return
	}
	r.service.reservations[r.key.ID] = next
}

// Settle books the request's actual cost and releases the reservation, in that
// order: in between the cost counts twice rather than not at all.
func (r *Reservation) Settle(actual float64, requestID string) error {
	defer r.Release()
	return r.service.Record(r.key, actual, requestID)
}

// Record books one ledger row for the key.
func (s *Service) Record(key Key, amount float64, requestID string) error {
	if amount == 0 {
		return nil
	}
	var id any
	if requestID != "" {
		id = requestID
	}
	_, err := s.DB.W.ExecContext(context.Background(),
		`INSERT INTO budget_ledger (router_key_id, request_id, amount_usd, created_at) VALUES (?, ?, ?, ?)`,
		key.ID, id, amount, s.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("budget: record spend: %w", err)
	}
	return nil
}

// Reset drops the in-memory reservations and the pacing history cache (tests).
func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.reservations)
	forgetPacing(s)
}
