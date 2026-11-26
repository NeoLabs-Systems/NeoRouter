package quota

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

// AccountLimitMetrics are the limits a person can state for an account.
var AccountLimitMetrics = []string{RPM, RPD, TPM, TPD, "concurrent"}

// AccountLimits is what people stated for provider accounts, cached per
// account because every routing decision reads it.
type AccountLimits struct {
	db *dbx.DB
	// Watch is told when an account's limits change, so routing can take a
	// utilization snapshot around the write (routing's slot_utilization.watch).
	Watch func(accountID string)

	mu    sync.Mutex
	cache map[string]map[string]float64
}

func newAccountLimits(db *dbx.DB) *AccountLimits {
	return &AccountLimits{db: db, cache: map[string]map[string]float64{}}
}

// Get is the limits stated for an account, by metric.
func (a *AccountLimits) Get(accountID string) map[string]float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.get(accountID)
}

func (a *AccountLimits) get(accountID string) map[string]float64 {
	if cached, ok := a.cache[accountID]; ok {
		return cached
	}
	limits := map[string]float64{}
	rows, err := a.db.R.QueryContext(context.Background(), `SELECT metric, value FROM account_limits WHERE account_id = ?`, accountID)
	if err == nil {
		for rows.Next() {
			var metric string
			var value float64
			if err := rows.Scan(&metric, &value); err != nil {
				break
			}
			limits[metric] = value
		}
		rows.Close()
	}
	a.cache[accountID] = limits
	return limits
}

// Set applies a patch: a value sets, nil clears, an absent metric stays as it
// is. It returns the account's limits after the write.
func (a *AccountLimits) Set(accountID string, patch map[string]*float64, now time.Time) (map[string]float64, error) {
	a.watch(accountID)
	ctx := context.Background()
	tx, err := a.db.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("quota: set account limits: %w", err)
	}
	for _, metric := range AccountLimitMetrics {
		value, ok := patch[metric]
		if !ok {
			continue
		}
		if value == nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM account_limits WHERE account_id = ? AND metric = ?`, accountID, metric); err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("quota: set account limits: %w", err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_limits (account_id, metric, value, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(account_id, metric) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			accountID, metric, int64(jsRound(*value)), now.UnixMilli()); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("quota: set account limits: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("quota: set account limits: %w", err)
	}
	a.mu.Lock()
	delete(a.cache, accountID)
	a.mu.Unlock()
	a.watch(accountID)
	return a.Get(accountID), nil
}

func (a *AccountLimits) watch(accountID string) {
	if a.Watch != nil {
		a.Watch(accountID)
	}
}

// Reset drops the cache (tests).
func (a *AccountLimits) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	clear(a.cache)
}
