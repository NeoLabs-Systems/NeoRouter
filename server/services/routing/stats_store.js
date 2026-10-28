'use strict';

const { getDatabase } = require('../../db/database');

// Learned per-model behaviour. Counts decay exponentially so that a model
// that was broken yesterday can earn its way back, and penalties fade within
// minutes so short incidents do not exclude a model for long.
const OUTCOME_HALF_LIFE_MS = 24 * 60 * 60 * 1000;
const PENALTY_HALF_LIFE_MS = 5 * 60 * 1000;
const MAX_PENALTY = 10;
const EWMA_ALPHA = 0.2;
const BREAKER_WINDOW_MS = 15 * 60 * 1000;
const BREAKER_THRESHOLD = 3;
const BREAKER_OPEN_MS = 10 * 60 * 1000;

const cache = new Map();

function blank(ref) {
  return {
    model_ref: ref, successes: 0, failures: 0, decayed_at: Date.now(), latency_ms: null, ttft_ms: null, tps: null, samples: 0,
    penalty: 0, penalty_at: Date.now(), cooldown_until: 0, cooldown_reason: null, cooldown_level: 0, recent_failures_json: '[]',
    breaker_open_until: 0, last_error: null, last_success_at: null, last_failure_at: null, min_output_tokens: 0,
  };
}

function get(ref) {
  if (cache.has(ref)) return cache.get(ref);
  const row = getDatabase().prepare('SELECT * FROM model_stats WHERE model_ref = ?').get(ref) || blank(ref);
  cache.set(ref, row);
  return row;
}

function decay(row, now = Date.now()) {
  const outcomeFactor = 0.5 ** ((now - (row.decayed_at || now)) / OUTCOME_HALF_LIFE_MS);
  row.successes *= outcomeFactor;
  row.failures *= outcomeFactor;
  row.decayed_at = now;
  const penaltyFactor = 0.5 ** ((now - (row.penalty_at || now)) / PENALTY_HALF_LIFE_MS);
  row.penalty *= penaltyFactor;
  row.penalty_at = now;
  return row;
}

function save(row) {
  getDatabase().prepare(`INSERT INTO model_stats (model_ref, successes, failures, decayed_at, latency_ms, ttft_ms, tps, samples,
      penalty, penalty_at, cooldown_until, cooldown_reason, cooldown_level, recent_failures_json, breaker_open_until, last_error,
      last_success_at, last_failure_at, min_output_tokens)
    VALUES (@model_ref, @successes, @failures, @decayed_at, @latency_ms, @ttft_ms, @tps, @samples, @penalty, @penalty_at,
      @cooldown_until, @cooldown_reason, @cooldown_level, @recent_failures_json, @breaker_open_until, @last_error,
      @last_success_at, @last_failure_at, @min_output_tokens)
    ON CONFLICT(model_ref) DO UPDATE SET successes = excluded.successes, failures = excluded.failures, decayed_at = excluded.decayed_at,
      latency_ms = excluded.latency_ms, ttft_ms = excluded.ttft_ms, tps = excluded.tps, samples = excluded.samples,
      penalty = excluded.penalty, penalty_at = excluded.penalty_at, cooldown_until = excluded.cooldown_until,
      cooldown_reason = excluded.cooldown_reason, cooldown_level = excluded.cooldown_level,
      recent_failures_json = excluded.recent_failures_json, breaker_open_until = excluded.breaker_open_until,
      last_error = excluded.last_error, last_success_at = excluded.last_success_at, last_failure_at = excluded.last_failure_at,
      min_output_tokens = excluded.min_output_tokens`)
    .run(row);
}

function ewma(previous, value) {
  if (!Number.isFinite(value)) return previous;
  return previous === null || previous === undefined ? value : previous + EWMA_ALPHA * (value - previous);
}

// Current view for scoring (decayed, without persisting).
function snapshot(ref, now = Date.now()) {
  const row = { ...get(ref) };
  decay(row, now);
  return row;
}

function update(ref, mutate, now = Date.now()) {
  const row = decay(get(ref), now);
  mutate(row, now);
  row.penalty = Math.min(MAX_PENALTY, Math.max(0, row.penalty));
  cache.set(ref, row);
  save(row);
  return row;
}

function recordSuccess(ref, { latencyMs, ttftMs, outputTokens, generationMs }, now = Date.now()) {
  return update(ref, (row) => {
    // Slowness is a speed matter, not a reliability one: a slow answer only
    // moves the latency average, so styles that do not care about speed
    // (e.g. "smartest") are not steered away from big, slow models.
    row.successes += 1;
    row.latency_ms = ewma(row.latency_ms, latencyMs);
    if (Number.isFinite(ttftMs)) row.ttft_ms = ewma(row.ttft_ms, ttftMs);
    if (outputTokens >= 16 && generationMs > 50) row.tps = ewma(row.tps, outputTokens / (generationMs / 1000));
    row.samples += 1;
    row.cooldown_level = 0;
    row.last_success_at = now;
    if (row.breaker_open_until && row.breaker_open_until <= now) {
      row.breaker_open_until = 0;
      row.recent_failures_json = '[]';
    }
  }, now);
}

// An attempt cancelled because another model answered first took at least
// this long; it lowers the speed estimate and nothing else.
function recordSlow(ref, elapsedMs, now = Date.now()) {
  return update(ref, (row) => {
    if (Number.isFinite(elapsedMs) && elapsedMs > (row.latency_ms || 0)) row.latency_ms = ewma(row.latency_ms, elapsedMs);
  }, now);
}

function recordFailure(ref, { weight = 1, penalty = 1, message = null, countsForBreaker = true, cooldownUntil = 0, cooldownReason = null, escalate = false }, now = Date.now()) {
  return update(ref, (row) => {
    row.failures += weight;
    row.penalty += penalty;
    row.last_error = message ? String(message).slice(0, 500) : row.last_error;
    row.last_failure_at = now;
    if (escalate) row.cooldown_level += 1;
    if (cooldownUntil > row.cooldown_until) {
      row.cooldown_until = cooldownUntil;
      row.cooldown_reason = cooldownReason;
    }
    if (countsForBreaker) {
      const recent = JSON.parse(row.recent_failures_json || '[]').filter((time) => now - time < BREAKER_WINDOW_MS);
      recent.push(now);
      row.recent_failures_json = JSON.stringify(recent.slice(-20));
      if (recent.length >= BREAKER_THRESHOLD && row.breaker_open_until <= now) row.breaker_open_until = now + BREAKER_OPEN_MS;
    }
  }, now);
}

function raiseMinOutputTokens(ref, tokens, now = Date.now()) {
  return update(ref, (row) => {
    row.min_output_tokens = Math.max(row.min_output_tokens || 0, tokens);
  }, now);
}

function clearCooldown(ref) {
  return update(ref, (row) => {
    row.cooldown_until = 0;
    row.cooldown_reason = null;
    row.cooldown_level = 0;
    row.breaker_open_until = 0;
    row.recent_failures_json = '[]';
    row.penalty = 0;
  });
}

// A half-open breaker lets exactly one probe through after it expires; the
// probe's outcome closes it again (success) or reopens it (failure).
function availability(ref, now = Date.now()) {
  const row = get(ref);
  if (row.cooldown_until > now) return { available: false, reason: row.cooldown_reason || 'cooldown', until: row.cooldown_until };
  if (row.breaker_open_until > now) return { available: false, reason: 'circuit_open', until: row.breaker_open_until };
  return { available: true, halfOpen: row.breaker_open_until > 0 };
}

function resetForTests() {
  cache.clear();
}

module.exports = { snapshot, recordSuccess, recordSlow, recordFailure, raiseMinOutputTokens, clearCooldown, availability, resetForTests, MAX_PENALTY };
