package routing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	sqlite "modernc.org/sqlite"
)

// Learned per-model behaviour (stats_store.js). Counts decay exponentially so
// that a model that was broken yesterday can earn its way back, and penalties
// fade within minutes so short incidents do not exclude a model for long.
const (
	outcomeHalfLifeMs = dayMs
	penaltyHalfLifeMs = 5 * minuteMs
	maxPenalty        = 10
	ewmaAlpha         = 0.2
	breakerWindowMs   = 15 * minuteMs
	breakerThreshold  = 3
	breakerOpenMs     = 10 * minuteMs
	// A learned output-room floor is a judgement from one moment: the model or
	// its settings may change. It holds for this long, then the model is tried
	// without it again.
	outputFloorRecheckMs = 6 * hourMs
)

// sqliteConstraintForeignKey is SQLITE_CONSTRAINT_FOREIGNKEY: the model was
// removed while a request to it ran, so there is nothing left to record.
const sqliteConstraintForeignKey = 787

// StatsRow is one model's learned record (model_stats).
type StatsRow struct {
	ModelRef         int64
	Successes        float64
	Failures         float64
	DecayedAt        int64
	LatencyMs        *float64
	TTFTMs           *float64
	TPS              *float64
	Samples          int64
	Penalty          float64
	PenaltyAt        int64
	CooldownUntil    int64
	CooldownReason   *string
	CooldownLevel    int64
	RecentFailures   []int64
	BreakerOpenUntil int64
	LastError        *string
	LastSuccessAt    *int64
	LastFailureAt    *int64
	MinOutputTokens  int64
	MinOutputAt      int64
}

// Measurement is what a successful attempt measured.
type Measurement struct {
	LatencyMs    *float64
	TTFTMs       *float64
	OutputTokens float64
	GenerationMs float64
}

// FailureRecord is what a failed attempt changes in a model's record.
type FailureRecord struct {
	Weight           float64
	Penalty          float64
	Message          *string
	CountsForBreaker bool
	CooldownUntil    int64
	CooldownReason   *string
	Escalate         bool
}

// Availability says whether a model may be tried now.
type Availability struct {
	Available bool
	Reason    string
	Until     int64
	HalfOpen  bool
}

type statsStore struct {
	db  *dbx.DB
	log *slog.Logger

	mu    sync.Mutex
	cache map[int64]*StatsRow
}

func newStatsStore(db *dbx.DB, log *slog.Logger) *statsStore {
	return &statsStore{db: db, log: log, cache: map[int64]*StatsRow{}}
}

func blankStats(ref int64, now time.Time) *StatsRow {
	return &StatsRow{ModelRef: ref, DecayedAt: now.UnixMilli(), PenaltyAt: now.UnixMilli()}
}

func (st *statsStore) get(ref int64, now time.Time) *StatsRow {
	if row, ok := st.cache[ref]; ok {
		return row
	}
	row := blankStats(ref, now)
	if st.db != nil {
		var loaded StatsRow
		var latency, ttft, tps *float64
		var cooldownReason, lastError *string
		var lastSuccess, lastFailure *int64
		var recentJSON string
		err := st.db.R.QueryRow(`SELECT model_ref, successes, failures, decayed_at, latency_ms, ttft_ms, tps, samples, penalty,
			penalty_at, cooldown_until, cooldown_reason, cooldown_level, recent_failures_json, breaker_open_until, last_error,
			last_success_at, last_failure_at, min_output_tokens, min_output_at FROM model_stats WHERE model_ref = ?`, ref).
			Scan(&loaded.ModelRef, &loaded.Successes, &loaded.Failures, &loaded.DecayedAt, &latency, &ttft, &tps, &loaded.Samples,
				&loaded.Penalty, &loaded.PenaltyAt, &loaded.CooldownUntil, &cooldownReason, &loaded.CooldownLevel, &recentJSON,
				&loaded.BreakerOpenUntil, &lastError, &lastSuccess, &lastFailure, &loaded.MinOutputTokens, &loaded.MinOutputAt)
		if err == nil {
			loaded.LatencyMs, loaded.TTFTMs, loaded.TPS = latency, ttft, tps
			loaded.CooldownReason, loaded.LastError = cooldownReason, lastError
			loaded.LastSuccessAt, loaded.LastFailureAt = lastSuccess, lastFailure
			row = &loaded
			_ = json.Unmarshal([]byte(recentJSON), &row.RecentFailures)
			if row.RecentFailures == nil {
				row.RecentFailures = []int64{}
			}
		}
	}
	st.cache[ref] = row
	return row
}

func decayStats(row *StatsRow, now time.Time) *StatsRow {
	ms := now.UnixMilli()
	outcomeFactor := pow(0.5, float64(ms-row.DecayedAt)/float64(outcomeHalfLifeMs))
	row.Successes *= outcomeFactor
	row.Failures *= outcomeFactor
	row.DecayedAt = ms
	penaltyFactor := pow(0.5, float64(ms-row.PenaltyAt)/float64(penaltyHalfLifeMs))
	row.Penalty *= penaltyFactor
	row.PenaltyAt = ms
	return row
}

func (st *statsStore) save(row *StatsRow) error {
	if st.db == nil {
		return nil
	}
	recentJSON, err := json.Marshal(row.RecentFailures)
	if err != nil {
		recentJSON = []byte("[]")
	}
	_, err = st.db.W.Exec(`INSERT INTO model_stats (model_ref, successes, failures, decayed_at, latency_ms, ttft_ms, tps, samples,
		penalty, penalty_at, cooldown_until, cooldown_reason, cooldown_level, recent_failures_json, breaker_open_until, last_error,
		last_success_at, last_failure_at, min_output_tokens, min_output_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(model_ref) DO UPDATE SET successes = excluded.successes, failures = excluded.failures, decayed_at = excluded.decayed_at,
		latency_ms = excluded.latency_ms, ttft_ms = excluded.ttft_ms, tps = excluded.tps, samples = excluded.samples,
		penalty = excluded.penalty, penalty_at = excluded.penalty_at, cooldown_until = excluded.cooldown_until,
		cooldown_reason = excluded.cooldown_reason, cooldown_level = excluded.cooldown_level,
		recent_failures_json = excluded.recent_failures_json, breaker_open_until = excluded.breaker_open_until,
		last_error = excluded.last_error, last_success_at = excluded.last_success_at, last_failure_at = excluded.last_failure_at,
		min_output_tokens = excluded.min_output_tokens, min_output_at = excluded.min_output_at`,
		row.ModelRef, row.Successes, row.Failures, row.DecayedAt, row.LatencyMs, row.TTFTMs, row.TPS, row.Samples,
		row.Penalty, row.PenaltyAt, row.CooldownUntil, row.CooldownReason, row.CooldownLevel, string(recentJSON),
		row.BreakerOpenUntil, row.LastError, row.LastSuccessAt, row.LastFailureAt, row.MinOutputTokens, row.MinOutputAt)
	return err
}

func ewma(previous *float64, value *float64) *float64 {
	if value == nil {
		return previous
	}
	if previous == nil {
		return value
	}
	next := *previous + ewmaAlpha*(*value-*previous)
	return &next
}

func ewmaFloat(previous *float64, value float64) *float64 {
	if previous == nil {
		return &value
	}
	next := *previous + ewmaAlpha*(value-*previous)
	return &next
}

// snapshot is the current view for scoring (decayed, without persisting).
func (st *statsStore) snapshot(ref int64, now time.Time) StatsRow {
	st.mu.Lock()
	defer st.mu.Unlock()
	row := *st.get(ref, now)
	return *decayStats(&row, now)
}

func (st *statsStore) update(ref int64, mutate func(*StatsRow, time.Time), now time.Time) StatsRow {
	st.mu.Lock()
	defer st.mu.Unlock()
	row := decayStats(st.get(ref, now), now)
	mutate(row, now)
	row.Penalty = minf(maxPenalty, maxf(0, row.Penalty))
	st.cache[ref] = row
	if err := st.save(row); err != nil {
		// The model was removed while a request to it was still running (its
		// connection deleted, a pool partner left): nothing left to record.
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.Code() != sqliteConstraintForeignKey {
			if st.log != nil {
				st.log.Warn("routing: could not record model stats", "model", ref, "error", err)
			}
		}
	}
	return *row
}

// recordSuccess notes an answer that worked.
func (st *statsStore) recordSuccess(ref int64, m Measurement, now time.Time) StatsRow {
	return st.update(ref, func(row *StatsRow, now time.Time) {
		// Slowness is a speed matter, not a reliability one: a slow answer only
		// moves the latency average, so styles that do not care about speed
		// (e.g. "smartest") are not steered away from big, slow models.
		row.Successes++
		row.LatencyMs = ewma(row.LatencyMs, m.LatencyMs)
		if m.TTFTMs != nil {
			row.TTFTMs = ewma(row.TTFTMs, m.TTFTMs)
		}
		if m.OutputTokens >= 16 && m.GenerationMs > 50 {
			row.TPS = ewmaFloat(row.TPS, m.OutputTokens/(m.GenerationMs/1000))
		}
		row.Samples++
		row.CooldownLevel = 0
		at := now.UnixMilli()
		row.LastSuccessAt = &at
		if row.BreakerOpenUntil != 0 && row.BreakerOpenUntil <= at {
			row.BreakerOpenUntil = 0
			row.RecentFailures = []int64{}
		}
	}, now)
}

// recordSlow notes an attempt cancelled because another model answered first:
// it lowers the speed estimate and nothing else.
func (st *statsStore) recordSlow(ref int64, elapsedMs float64, now time.Time) StatsRow {
	return st.update(ref, func(row *StatsRow, now time.Time) {
		current := 0.0
		if row.LatencyMs != nil {
			current = *row.LatencyMs
		}
		if elapsedMs > current {
			row.LatencyMs = ewmaFloat(row.LatencyMs, elapsedMs)
		}
	}, now)
}

// recordFailure notes a failure with the weight and penalty learning gives it.
func (st *statsStore) recordFailure(ref int64, record FailureRecord, now time.Time) StatsRow {
	return st.update(ref, func(row *StatsRow, now time.Time) {
		at := now.UnixMilli()
		row.Failures += record.Weight
		row.Penalty += record.Penalty
		if record.Message != nil && *record.Message != "" {
			message := *record.Message
			if len(message) > 500 {
				message = message[:500]
			}
			row.LastError = &message
		}
		row.LastFailureAt = &at
		if record.Escalate {
			row.CooldownLevel++
		}
		if record.CooldownUntil > row.CooldownUntil {
			row.CooldownUntil = record.CooldownUntil
			row.CooldownReason = record.CooldownReason
		}
		if record.CountsForBreaker {
			recent := make([]int64, 0, len(row.RecentFailures))
			for _, failure := range row.RecentFailures {
				if at-failure < breakerWindowMs {
					recent = append(recent, failure)
				}
			}
			recent = append(recent, at)
			if len(recent) > 20 {
				recent = recent[len(recent)-20:]
			}
			row.RecentFailures = recent
			if int64(len(recent)) >= breakerThreshold && row.BreakerOpenUntil <= at {
				row.BreakerOpenUntil = at + breakerOpenMs
			}
		}
	}, now)
}

// raiseMinOutputTokens learns the output room a reasoning model needs.
func (st *statsStore) raiseMinOutputTokens(ref int64, tokens int64, now time.Time) StatsRow {
	return st.update(ref, func(row *StatsRow, now time.Time) {
		// A floor past its recheck is stale: the new evidence replaces it.
		current := outputFloorOf(row, now)
		row.MinOutputTokens = maxInt64(current, tokens)
		row.MinOutputAt = now.UnixMilli()
	}, now)
}

func outputFloorOf(row *StatsRow, now time.Time) int64 {
	if row.MinOutputTokens == 0 {
		return 0
	}
	if now.UnixMilli()-row.MinOutputAt < outputFloorRecheckMs {
		return row.MinOutputTokens
	}
	return 0
}

// outputFloor is the output tokens a model needs at least, or 0 (none, or due
// for a retry without it).
func (st *statsStore) outputFloor(ref int64, now time.Time) int64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	return outputFloorOf(st.get(ref, now), now)
}

// clearCooldown lets a model be tried again.
func (st *statsStore) clearCooldown(ref int64, now time.Time) StatsRow {
	return st.update(ref, func(row *StatsRow, now time.Time) {
		row.CooldownUntil = 0
		row.CooldownReason = nil
		row.CooldownLevel = 0
		row.BreakerOpenUntil = 0
		row.RecentFailures = []int64{}
		row.Penalty = 0
	}, now)
}

// forgive gives a model a person marked as fixed a clean slate.
func (st *statsStore) forgive(ref int64, now time.Time) StatsRow {
	return st.update(ref, func(row *StatsRow, now time.Time) {
		row.CooldownUntil = 0
		row.CooldownReason = nil
		row.CooldownLevel = 0
		row.BreakerOpenUntil = 0
		row.RecentFailures = []int64{}
		row.Penalty = 0
		row.Failures = 0
	}, now)
}

// availability is whether a model may take work now. A half-open breaker lets
// exactly one probe through after it expires; the probe's outcome closes it
// again (success) or reopens it (failure).
func (st *statsStore) availability(ref int64, now time.Time) Availability {
	st.mu.Lock()
	row := *st.get(ref, now)
	st.mu.Unlock()
	ms := now.UnixMilli()
	if row.CooldownUntil > ms {
		reason := "cooldown"
		if row.CooldownReason != nil && *row.CooldownReason != "" {
			reason = *row.CooldownReason
		}
		return Availability{Reason: reason, Until: row.CooldownUntil}
	}
	if row.BreakerOpenUntil > ms {
		return Availability{Reason: "circuit_open", Until: row.BreakerOpenUntil}
	}
	return Availability{Available: true, HalfOpen: row.BreakerOpenUntil > 0}
}

// ModelAvailability is whether a model may be tried right now, and why not
// (the usage domain's model health view).
func (s *Service) ModelAvailability(ref int64, now time.Time) Availability {
	return s.stats.availability(ref, now)
}

// ModelStats is a model's learned record as it stands now, decayed without
// being persisted (the usage domain's learning view).
func (s *Service) ModelStats(ref int64, now time.Time) StatsRow {
	return s.stats.snapshot(ref, now)
}
