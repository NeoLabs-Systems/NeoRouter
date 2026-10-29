'use strict';

// Prepaid plans: the allowance is already paid for and does not roll over,
// so routing prefers plan models while there is room, holds back as the
// allowance runs low and skips the plan once it is used up until it renews.
// Allowances come from the provider's quota endpoint (probed periodically) as
// one or more windows: `plan_requests` counts requests (Synthetic),
// `plan_5h` / `plan_week` are percentages left (MiniMax). Requests sent since
// the last probe are subtracted from counted windows; the tightest window wins.
const quota = require('../quota/quota_service');

const PLAN_METRICS = new Set(['plan_requests', 'plan_5h', 'plan_week', 'plan_month']);
const COUNTED = 'plan_requests';
// Plenty left: a mild preference. Running low: hold back.
const PREFERENCE = 1.1;
const LOW_SHARE = 0.15;
const LOW_FACTOR = 0.5;

const sent = new Map();

function windows(accountId) {
  return quota.accountState(accountId).filter((item) => item.scope === `a:${accountId}` && PLAN_METRICS.has(item.metric) && item.limit);
}

/** { share: fraction left of the tightest window, resetAt } or null when unknown. */
function room(entry, now = Date.now()) {
  let tightest = null;
  for (const window of windows(entry.accountId)) {
    let remaining = window.remaining ?? window.limit;
    if (window.resetAt && window.resetAt <= now) remaining = window.limit;
    else if (window.metric === COUNTED) {
      const counted = sent.get(entry.accountId);
      if (counted && counted.since === window.observedAt) remaining -= counted.count;
    }
    const share = Math.max(0, remaining) / window.limit;
    if (!tightest || share < tightest.share) tightest = { share, resetAt: window.resetAt && window.resetAt > now ? window.resetAt : null };
  }
  return tightest;
}

function onSent(entry) {
  if (!entry.isPlan) return;
  const counted = windows(entry.accountId).find((window) => window.metric === COUNTED);
  if (!counted) return;
  const previous = sent.get(entry.accountId);
  sent.set(entry.accountId, { since: counted.observedAt, count: previous && previous.since === counted.observedAt ? previous.count + 1 : 1 });
}

const usedUp = (left) => Boolean(left) && left.share <= 0;

/**
 * The entry as routing should see it now: once a "plan, then pay per use"
 * plan is used up, its models are billed like any paid model.
 */
function effective(entry, now = Date.now()) {
  if (!entry.isPlan || !entry.planOverflow) return entry;
  return usedUp(room(entry, now)) ? { ...entry, isPlan: false } : entry;
}

/** 'plan_used_up' when a plan-only account has nothing left, else null. */
function rejection(entry, now = Date.now()) {
  if (!entry.isPlan) return null;
  return usedUp(room(entry, now)) ? 'plan_used_up' : null;
}

function factor(entry, now = Date.now()) {
  if (!entry.isPlan) return 1;
  const left = room(entry, now);
  return left && left.share < LOW_SHARE ? LOW_FACTOR : PREFERENCE;
}

// ChatGPT (Codex) reports its plan windows on every response:
// x-codex-primary-used-percent, -window-minutes, -reset-at (Unix seconds),
// and the same for "secondary" (usually 5 hours and a week).
function observeHeaders(entry, headers, now = Date.now()) {
  if (!headers?.get) return;
  for (const slot of ['primary', 'secondary']) {
    const raw = headers.get(`x-codex-${slot}-used-percent`);
    const used = raw === null || raw === '' ? NaN : Number(raw);
    if (!Number.isFinite(used)) continue;
    const minutes = Number(headers.get(`x-codex-${slot}-window-minutes`));
    const resetAt = Number(headers.get(`x-codex-${slot}-reset-at`)) * 1000 || null;
    const metric = !Number.isFinite(minutes) || minutes <= 6 * 60 ? 'plan_5h' : minutes <= 8 * 24 * 60 ? 'plan_week' : 'plan_month';
    quota.setState(`a:${entry.accountId}`, metric, { limit: 100, remaining: Math.max(0, 100 - used), resetAt }, 'headers', now);
  }
}

function resetForTests() {
  sent.clear();
}

module.exports = { room, onSent, effective, rejection, factor, observeHeaders, PLAN_METRICS, resetForTests };
