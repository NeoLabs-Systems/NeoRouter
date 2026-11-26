package routing

import (
	"context"
	"sync"
	"time"
)

// Time integrals of router-observed concurrency, without request contents.
// Restarting begins a new observation interval; downtime is never called idle.

// flatRate is the stored billing value of a flat-rate subscription.
const flatRate = 3

type slotState struct {
	at    int64
	busy  int
	limit *int
}

type slotUsage struct {
	svc *Service

	mu     sync.Mutex
	states map[string]*slotState
}

func newSlotUsage(svc *Service) *slotUsage {
	return &slotUsage{svc: svc, states: map[string]*slotState{}}
}

// SlotPoint is one bucket of the utilization series.
type SlotPoint struct {
	Bucket      int64    `json:"bucket"`
	AverageBusy *float64 `json:"averageBusy"`
	PeakBusy    int64    `json:"peakBusy"`
}

// SlotSnapshot is one account's observed concurrency, for the usage page.
type SlotSnapshot struct {
	Current     int         `json:"current"`
	Limit       *int        `json:"limit"`
	ObservedMs  int64       `json:"observedMs"`
	AverageBusy *float64    `json:"averageBusy"`
	PeakBusy    int64       `json:"peakBusy"`
	IdleShare   *float64    `json:"idleShare"`
	Series      []SlotPoint `json:"series"`
}

func (u *slotUsage) write(accountID string, bucket, elapsed int64, busy int, limit *int) error {
	if u.svc.opts.DB == nil {
		return nil
	}
	capacityMs := int64(0)
	unknownMs := elapsed
	if limit != nil {
		capacityMs = int64(*limit) * elapsed
		unknownMs = 0
	}
	_, err := u.svc.opts.DB.W.Exec(`INSERT INTO slot_utilization (account_id, bucket, observed_ms, busy_ms, capacity_ms, unknown_ms, peak)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(account_id, bucket) DO UPDATE SET
		observed_ms = observed_ms + excluded.observed_ms, busy_ms = busy_ms + excluded.busy_ms,
		capacity_ms = capacity_ms + excluded.capacity_ms, unknown_ms = unknown_ms + excluded.unknown_ms,
		peak = MAX(peak, excluded.peak)`,
		accountID, bucket, elapsed, int64(busy)*elapsed, capacityMs, unknownMs, busy)
	return err
}

// flush writes the time since the last observation into buckets.
func (u *slotUsage) flush(accountID string, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()
	state, ok := u.states[accountID]
	if !ok || now.UnixMilli() < state.at {
		return
	}
	if u.svc.opts.DB == nil {
		u.states[accountID] = &slotState{at: now.UnixMilli(), busy: state.busy, limit: state.limit}
		return
	}
	var exists int
	if err := u.svc.opts.DB.R.QueryRow(`SELECT 1 FROM provider_accounts WHERE id = ?`, accountID).Scan(&exists); err != nil {
		delete(u.states, accountID)
		return
	}
	cfg := u.svc.routingCfg()
	bucketMs := cfg.SlotUsageBucket.Milliseconds()
	windowMs := cfg.SlotUsageWindow.Milliseconds()
	if bucketMs <= 0 {
		return
	}
	nowMs := now.UnixMilli()
	at := state.at
	if floor := nowMs - windowMs; floor > at {
		at = floor
	}
	for at < nowMs {
		bucket := at / bucketMs * bucketMs
		end := bucket + bucketMs
		if end > nowMs {
			end = nowMs
		}
		if err := u.write(accountID, bucket, end-at, state.busy, state.limit); err != nil {
			u.svc.log().Warn("routing: could not record slot utilization", "account", accountID, "error", err)
		}
		at = end
	}
	state.at = nowMs
}

// watch begins (or continues) observing an account, when it is a flat-rate
// subscription that is enabled.
func (u *slotUsage) watch(accountID string, now time.Time) {
	if u.svc.opts.DB == nil {
		return
	}
	var definitionID string
	var prepaid, enabled int
	err := u.svc.opts.DB.R.QueryRow(`SELECT definition_id, prepaid, enabled FROM provider_accounts WHERE id = ?`, accountID).
		Scan(&definitionID, &prepaid, &enabled)
	if err != nil {
		u.mu.Lock()
		delete(u.states, accountID)
		u.mu.Unlock()
		return
	}
	u.flush(accountID, now)
	if prepaid != flatRate || enabled == 0 {
		u.mu.Lock()
		delete(u.states, accountID)
		u.mu.Unlock()
		return
	}
	entry := &Entry{AccountID: accountID, DefinitionID: definitionID}
	var limit *int
	if known := u.svc.conc.limitFor(entry, now); known != nil {
		limit = &known.Limit
	}
	busy := u.svc.load.inflight(entry).Account
	u.mu.Lock()
	u.states[accountID] = &slotState{at: now.UnixMilli(), busy: busy, limit: limit}
	u.mu.Unlock()
}

// transition records a change in an account's in-flight count.
func (u *slotUsage) transition(entry *Entry, busy int, now time.Time) {
	if !entry.IsFlatRate {
		u.mu.Lock()
		_, watched := u.states[entry.AccountID]
		u.mu.Unlock()
		if !watched {
			return
		}
	}
	u.mu.Lock()
	_, watched := u.states[entry.AccountID]
	u.mu.Unlock()
	if !watched {
		u.watch(entry.AccountID, now)
	}
	u.mu.Lock()
	state, ok := u.states[entry.AccountID]
	u.mu.Unlock()
	if !ok {
		return
	}
	u.flush(entry.AccountID, now)
	var limit *int
	if known := u.svc.conc.limitFor(entry, now); known != nil {
		limit = &known.Limit
	}
	u.mu.Lock()
	state.busy = busy
	state.limit = limit
	u.mu.Unlock()
	bucketMs := u.svc.routingCfg().SlotUsageBucket.Milliseconds()
	if bucketMs <= 0 {
		return
	}
	if err := u.write(entry.AccountID, now.UnixMilli()/bucketMs*bucketMs, 0, busy, limit); err != nil {
		u.svc.log().Warn("routing: could not record slot utilization", "account", entry.AccountID, "error", err)
	}
}

// initialize begins observing every enabled flat-rate account and, when workers
// run, keeps observing on a ticker (called at app start).
func (s *Service) Initialize(ctx context.Context) error {
	if s.opts.DB == nil {
		return nil
	}
	now := time.Now()
	rows, err := s.opts.DB.R.Query(`SELECT id FROM provider_accounts WHERE prepaid = ? AND enabled = 1`, flatRate)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s.slots.watch(id, now)
	}
	if s.cfg().WorkersEnabled {
		bucket := s.routingCfg().SlotUsageBucket
		if bucket <= 0 {
			bucket = time.Minute
		}
		go func() {
			ticker := time.NewTicker(bucket)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case at := <-ticker.C:
					s.slots.mu.Lock()
					accounts := make([]string, 0, len(s.slots.states))
					for id := range s.slots.states {
						accounts = append(accounts, id)
					}
					s.slots.mu.Unlock()
					for _, id := range accounts {
						s.slots.watch(id, at)
					}
				}
			}
		}()
	}
	return nil
}

// SlotSnapshot is one account's observed utilization (usage page).
func (s *Service) SlotSnapshot(accountID string, now time.Time) SlotSnapshot {
	s.slots.watch(accountID, now)
	cfg := s.routingCfg()
	bucketMs := cfg.SlotUsageBucket.Milliseconds()
	windowMs := cfg.SlotUsageWindow.Milliseconds()
	out := SlotSnapshot{Series: []SlotPoint{}}
	if bucketMs <= 0 || s.opts.DB == nil {
		return out
	}
	since := (now.UnixMilli() - windowMs) / bucketMs * bucketMs
	if _, err := s.opts.DB.W.Exec(`DELETE FROM slot_utilization WHERE account_id = ? AND bucket < ?`, accountID, since); err != nil {
		s.log().Warn("routing: could not prune slot utilization", "account", accountID, "error", err)
	}
	rows, err := s.opts.DB.R.Query(`SELECT bucket, observed_ms, busy_ms, capacity_ms, unknown_ms, peak FROM slot_utilization
		WHERE account_id = ? AND bucket >= ? ORDER BY bucket`, accountID, since)
	if err != nil {
		return out
	}
	defer rows.Close()
	var observed, busyMs, capacityMs, unknownMs int64
	for rows.Next() {
		var bucket, observedMs, busy, capacity, unknown, peak int64
		if err := rows.Scan(&bucket, &observedMs, &busy, &capacity, &unknown, &peak); err != nil {
			continue
		}
		observed += observedMs
		busyMs += busy
		capacityMs += capacity
		unknownMs += unknown
		if peak > out.PeakBusy {
			out.PeakBusy = peak
		}
		point := SlotPoint{Bucket: bucket, PeakBusy: peak}
		if observedMs != 0 {
			average := float64(busy) / float64(observedMs)
			point.AverageBusy = &average
		}
		out.Series = append(out.Series, point)
	}
	s.slots.mu.Lock()
	if state, ok := s.slots.states[accountID]; ok {
		out.Current = state.busy
		out.Limit = state.limit
	}
	s.slots.mu.Unlock()
	out.ObservedMs = observed
	if observed != 0 {
		average := float64(busyMs) / float64(observed)
		out.AverageBusy = &average
	}
	if capacityMs != 0 && unknownMs == 0 {
		idle := maxf(0, 1-float64(busyMs)/float64(capacityMs))
		out.IdleShare = &idle
	}
	return out
}
