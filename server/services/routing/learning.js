'use strict';

const { KINDS } = require('../../providers/provider_error');
const stats = require('./stats_store');
const concurrency = require('./concurrency');
const plans = require('./plans');
const quota = require('../quota/quota_service');
const billing = require('../providers/billing_check');
const { getDatabase } = require('../../db/database');
const experience = require('./task/experience');

// Failures that say the model handled this kind of task badly (as opposed to
// limits, outages or bad requests, which say nothing about the task).
const TASK_FAILURES = new Set([KINDS.EMPTY, KINDS.INVALID_OUTPUT, KINDS.TRUNCATED]);

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const RATE_LIMIT_COOLDOWN_MS = 90 * SECOND;
const QUOTA_LADDER_MS = [2 * MINUTE, 10 * MINUTE, HOUR, DAY];
const NOT_FOUND_LADDER_MS = [6 * HOUR, DAY, 7 * DAY];
const MAX_RETRY_AFTER_MS = DAY;

// How each error kind changes what the router believes about a model.
// fallback: whether trying another model makes sense for this request.
function policyFor(error) {
  switch (error.kind) {
    case KINDS.RATE_LIMIT: return { weight: 0.5, penalty: 2, breaker: false, fallback: true };
    case KINDS.QUOTA_EXHAUSTED: return { weight: 0.5, penalty: 1, breaker: false, fallback: true };
    case KINDS.PAYMENT: return { weight: 1, penalty: 3, breaker: false, fallback: true };
    case KINDS.AUTH: return { weight: 1, penalty: 3, breaker: false, fallback: true };
    case KINDS.NOT_FOUND: return { weight: 1, penalty: 2, breaker: false, fallback: true };
    case KINDS.CONTEXT_LENGTH: return { weight: 0, penalty: 0, breaker: false, fallback: true };
    case KINDS.TRUNCATED: return { weight: 0.25, penalty: 0.5, breaker: false, fallback: true };
    case KINDS.BAD_REQUEST: return { weight: 0.5, penalty: 0.5, breaker: false, fallback: true };
    case KINDS.TIMEOUT: return { weight: 1, penalty: 1.5, breaker: true, fallback: true };
    case KINDS.SERVER:
    case KINDS.NETWORK:
    case KINDS.EMPTY:
    case KINDS.INVALID_OUTPUT: return { weight: 1, penalty: 1, breaker: true, fallback: true };
    case KINDS.ABORTED: return { weight: 0, penalty: 0, breaker: false, fallback: false };
    default: return { weight: 1, penalty: 1, breaker: true, fallback: true };
  }
}

function cooldownFor(error, row, now) {
  const retryAfter = Number.isFinite(error.retryAfterMs) ? Math.min(error.retryAfterMs, MAX_RETRY_AFTER_MS) : null;
  switch (error.kind) {
    case KINDS.RATE_LIMIT:
      return { until: now + Math.max(retryAfter || 0, RATE_LIMIT_COOLDOWN_MS), reason: 'rate_limited', escalate: false };
    case KINDS.QUOTA_EXHAUSTED: {
      if (error.learned?.value === 0) return { until: now + DAY, reason: 'no_free_quota', escalate: false };
      const step = QUOTA_LADDER_MS[Math.min(row.cooldown_level, QUOTA_LADDER_MS.length - 1)];
      return { until: now + Math.max(retryAfter || 0, step), reason: 'quota_exhausted', escalate: true };
    }
    case KINDS.PAYMENT: return { until: now + DAY, reason: 'payment_required', escalate: false };
    case KINDS.AUTH: return { until: now + 30 * MINUTE, reason: 'auth_failed', escalate: false };
    case KINDS.NOT_FOUND: {
      // Listed but not deployed (e.g. NVIDIA "Function not found"): back off
      // further each time it is still missing.
      const step = NOT_FOUND_LADDER_MS[Math.min(row.cooldown_level, NOT_FOUND_LADDER_MS.length - 1)];
      return { until: now + step, reason: 'not_available', escalate: true };
    }
    case KINDS.BAD_REQUEST: {
      // One 400 may be the client's fault; the same model rejecting again
      // within a short window means it does not support this kind of request.
      const repeated = row.last_failure_at && now - row.last_failure_at < 15 * MINUTE && String(row.last_error || '').startsWith('bad_request');
      if (!repeated) return null;
      const step = [HOUR, 6 * HOUR, DAY][Math.min(row.cooldown_level, 2)];
      return { until: now + step, reason: 'incompatible', escalate: true };
    }
    default: return null;
  }
}

function learnContextWindow(entry, estimatedTokens) {
  if (!estimatedTokens) return;
  const limit = Math.floor(estimatedTokens * 0.9);
  getDatabase().prepare('UPDATE models SET context_window = ? WHERE id = ? AND (context_window IS NULL OR context_window > ?)')
    .run(limit, entry.ref, limit);
}

// A reasoning model needs room for thinking plus the answer. Remember a
// budget comfortably above the one that was not enough.
const TRUNCATED_HEADROOM = 2;

function onFailure(entry, error, { estimatedTokens = 0, maxTokens = null, now = Date.now() } = {}) {
  const policy = policyFor(error);
  if (error.kind === KINDS.ABORTED) return policy;
  if (TASK_FAILURES.has(error.kind)) experience.recordOutcome(entry.ref, entry.task, false, now);
  if (error.kind === KINDS.TRUNCATED && maxTokens) stats.raiseMinOutputTokens(entry.ref, Math.ceil(maxTokens * TRUNCATED_HEADROOM), now);
  if (error.kind === KINDS.CONTEXT_LENGTH) {
    learnContextWindow(entry, estimatedTokens);
    return policy;
  }
  // "Too many concurrent requests" says nothing about the per-minute rate:
  // the account is busy, not used up, so no cooldown and no exhausted rpm.
  const busy = error.kind === KINDS.RATE_LIMIT && concurrency.onRateLimited(entry, error, { now });
  const current = stats.snapshot(entry.ref, now);
  const cooldown = busy ? null : cooldownFor(error, current, now);
  stats.recordFailure(entry.ref, {
    // Busy is not unreliable: it neither costs reliability nor earns a penalty.
    weight: busy ? 0 : policy.weight,
    penalty: busy ? 0 : policy.penalty,
    message: `${error.kind}: ${error.message}`,
    countsForBreaker: policy.breaker,
    cooldownUntil: cooldown?.until || 0,
    cooldownReason: cooldown?.reason || null,
    escalate: cooldown?.escalate || false,
  }, now);
  if (error.headers) {
    quota.observeHeaders(entry, error.headers, now);
    billing.observeHeaders(entry, error.headers);
    plans.observeHeaders(entry, error.headers, now);
  }
  if (error.learned?.value) quota.learnLimit(entry, error.learned);
  if (error.kind === KINDS.QUOTA_EXHAUSTED) billing.observeFailure(entry, error);
  if (error.kind === KINDS.QUOTA_EXHAUSTED || (error.kind === KINDS.RATE_LIMIT && !busy)) {
    quota.markExhausted(entry, error.kind === KINDS.QUOTA_EXHAUSTED ? 'rpd' : 'rpm', cooldown?.until || null, now);
  }
  return policy;
}

// An attempt that lost a hedge race was slower than an alternative. That only
// says something about speed, so it updates the latency estimate and leaves
// reliability and penalties alone.
function onSlow(entry, elapsedMs, now = Date.now()) {
  stats.recordSlow(entry.ref, elapsedMs, now);
}

function onSuccess(entry, measurement, headers, now = Date.now()) {
  stats.recordSuccess(entry.ref, measurement, now);
  experience.recordOutcome(entry.ref, entry.task, true, now);
  concurrency.onSuccess(entry, { now });
  if (headers) {
    quota.observeHeaders(entry, headers, now);
    billing.observeHeaders(entry, headers);
    plans.observeHeaders(entry, headers, now);
  }
}

module.exports = { onFailure, onSuccess, onSlow, policyFor, QUOTA_LADDER_MS };
