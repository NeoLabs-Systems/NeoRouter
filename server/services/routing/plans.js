'use strict';

// Prepaid plans: a request costs no money, but it spends an allowance that
// is shared with the user's other use of the plan. Routing prices that
// allowance by how fast it is going: nothing when it would expire unused
// anyway (it does not roll over), a little when on schedule, more as it falls
// behind, so free models that are good enough take easy prompts and the plan
// is kept for prompts that need it. It holds back as the allowance runs low
// and skips the plan once it is used up until it renews.
// Allowances come from the provider's quota endpoint (probed periodically) or
// response headers as one or more windows: `plan_requests` counts requests
// (Synthetic), `plan_5h` / `plan_week` are percentages left (MiniMax,
// ChatGPT). Requests sent since the last probe are subtracted from counted
// windows; the tightest window wins.
const quota = require('../quota/quota_service');

const PLAN_METRICS = new Set(['plan_requests', 'plan_5h', 'plan_week', 'plan_month']);
const COUNTED = 'plan_requests';
// Running low: hold back.
const LOW_SHARE = 0.15;
const LOW_FACTOR = 0.5;
// Window lengths, to compare what is left with the time still to run.
const WINDOW_MS = { plan_5h: 5 * 3_600_000, plan_week: 7 * 86_400_000, plan_month: 30 * 86_400_000 };
const MIN_TIME_LEFT = 0.02;
// What a request from the allowance costs, on the router's cost scale
// (0 = free, 1 = the priciest models): on schedule or unknown, and far behind.
// Above the router's "clearly cheaper" step (0.05), so a free model of the
// same quality wins, and below the cheapest usual paid models, so the plan
// still comes before paying.
const ON_SCHEDULE_COST = 0.08;
const BEHIND_COST = 0.2;
// Well ahead of schedule (part of it would expire unused): lean on the plan.
const SURPLUS_BONUS = 0.15;

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

/**
 * Share left divided by the share of the window still to run, for the
 * tightest window: above 1 the allowance is ahead of schedule, below 1 it is
 * used faster than it renews. Null when no window has a known length and
 * reset time.
 */
function pace(entry, now = Date.now()) {
  let tightest = null;
  for (const window of windows(entry.accountId)) {
    const length = WINDOW_MS[window.metric];
    if (!length || !window.resetAt) continue;
    const renewed = window.resetAt <= now;
    const share = renewed ? 1 : Math.max(0, window.remaining ?? window.limit) / window.limit;
    const timeLeft = renewed ? 1 : Math.min(1, Math.max(MIN_TIME_LEFT, (window.resetAt - now) / length));
    const ratio = share / timeLeft;
    if (tightest === null || ratio < tightest) tightest = ratio;
  }
  return tightest;
}

/** The allowance a request spends, priced by pace() (0 for models outside a plan). */
function allowanceCost(entry, now = Date.now()) {
  if (!entry.isPlan) return 0;
  const ratio = pace(entry, now);
  if (ratio === null) return ON_SCHEDULE_COST;
  if (ratio >= 1) return ON_SCHEDULE_COST * Math.max(0, 2 - ratio);
  return ON_SCHEDULE_COST + (BEHIND_COST - ON_SCHEDULE_COST) * (1 - ratio);
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
  if (left && left.share < LOW_SHARE) return LOW_FACTOR;
  const ratio = pace(entry, now);
  return ratio !== null && ratio > 1 ? 1 + SURPLUS_BONUS * Math.min(1, ratio - 1) : 1;
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

module.exports = { room, pace, allowanceCost, onSent, effective, rejection, factor, observeHeaders, PLAN_METRICS, resetForTests };
