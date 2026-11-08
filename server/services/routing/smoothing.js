'use strict';

// Even out usage: free and plan allowances are spent at a steady pace, so
// they last until they reset instead of running out early and leaving the
// rest of the period to rejections. It is learned, not guessed:
// - Daily limits: each hour a connection may spend about its share of the
//   day's limit, weighted by when this person usually sends requests (from
//   the last two weeks), with room for peaks.
// - Plan windows (5 hours, a week, a month): what is used may run only a
//   little ahead of how much of the window has passed.
// It can be switched on only once there is enough data to go by (readiness).
// Keys with a high priority are never held back, and paid connections are
// not affected.
const { getDatabase } = require('../../db/database');
const settings = require('../settings/settings_service');
const quota = require('../quota/quota_service');
const plans = require('./plans');
const { HttpError } = require('../../middleware/error_handler');

const SETTING = 'smooth_allowances';
const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;
const CACHE_MS = 10 * 60_000;
// Enough data: this many days since the first request, with traffic on this
// many of the last 14 days, this many answered requests in them, and known
// limits behind this share of last week's answered requests.
const NEEDED = Object.freeze({ days: 7, activeDays: 5, requests: 100, coverage: 0.5 });
// An hour may use this many times its learned share of a daily limit, and
// never less than this share of the day, so a quiet hour still answers.
const PEAK_ROOM = 1.6;
const MIN_HOUR_SHARE = 1 / 48;
// A plan may run this far ahead of the time that has passed.
const PLAN_CUSHION = 0.08;
// Smoothed into an even day, so one unusual day does not decide an hour.
const PROFILE_BLEND = 0.6;
const PEER_DEFINITION = 'neorouter_peer';

const readinessCache = new Map();
const profileCache = new Map();

function cached(cache, userId, now, compute) {
  const hit = cache.get(userId);
  if (hit && now - hit.at < CACHE_MS) return hit.value;
  const value = compute();
  cache.set(userId, { at: now, value });
  return value;
}

// Only free and plan models have an allowance to spread; paid ones are
// billed per use and pool partners keep their own limits.
const paceable = (entry) => (entry.isFree || entry.isPlan) && entry.definitionId !== PEER_DEFINITION;

// Whether NeoRouter knows how this model's allowance behaves: a daily limit
// or plan window it can pace, or only per-minute limits, which cannot run
// out over a day and so need no pacing. Unknown: no limit learned at all.
function limitKnown(entry, now) {
  if (entry.isPlan && plans.schedule(entry, now)) return true;
  return quota.scopesFor(entry).some((scope) => {
    const daily = quota.dailyLimits(scope, now);
    if (daily.rpd !== null || daily.tpd !== null) return true;
    const known = quota.effectiveLimits(scope);
    return Boolean(known.rpm || known.tpm);
  });
}

function computeReadiness(userId, now) {
  const db = getDatabase();
  const first = db.prepare('SELECT MIN(created_at) first FROM requests WHERE user_id = ? AND created_at >= ?').get(userId, now - 30 * DAY_MS).first;
  const days = first ? Math.floor((now - first) / DAY_MS) : 0;
  const recent = db.prepare(`SELECT COUNT(*) requests, COUNT(DISTINCT CAST(created_at / ${DAY_MS} AS INTEGER)) active
    FROM requests WHERE user_id = ? AND created_at >= ? AND status = 'success'`).get(userId, now - 14 * DAY_MS);
  // Of last week's chat answers on free and plan models (the ones with an
  // allowance to spread), how many went to a model whose limits are known.
  // Paid models, other capabilities and pool partners do not count either way.
  const byModel = db.prepare(`SELECT model_ref, COUNT(*) count FROM requests WHERE user_id = ? AND created_at >= ? AND status = 'success' AND model_ref IS NOT NULL
    GROUP BY model_ref`).all(userId, now - 7 * DAY_MS);
  const catalog = require('../catalog/catalog_service');
  const entries = new Map(catalog.listForUser(userId, { capability: 'chat', includeUnavailable: true }).map((entry) => [entry.ref, entry]));
  let total = 0;
  let known = 0;
  for (const row of byModel) {
    const entry = entries.get(row.model_ref);
    if (!entry || !paceable(entry)) continue;
    total += row.count;
    if (limitKnown(entry, now)) known += row.count;
  }
  const coverage = total ? known / total : 0;
  const status = { days, activeDays: recent.active, requests: recent.requests, coverage: Math.round(coverage * 100) / 100 };
  const ready = status.days >= NEEDED.days && status.activeDays >= NEEDED.activeDays && status.requests >= NEEDED.requests && status.coverage >= NEEDED.coverage;
  return { ready, ...status, needed: NEEDED };
}

/** { ready, days, activeDays, requests, coverage, needed }: whether there is enough to go by. */
function readiness(userId, now = Date.now()) {
  return cached(readinessCache, userId, now, () => computeReadiness(userId, now));
}

// Share of a day's requests that usually falls in each hour (server time).
function hourProfile(userId, now) {
  return cached(profileCache, userId, now, () => {
    const counts = new Array(24).fill(0);
    const rows = getDatabase().prepare('SELECT created_at FROM requests WHERE user_id = ? AND created_at >= ?').all(userId, now - 14 * DAY_MS);
    for (const row of rows) counts[new Date(row.created_at).getHours()] += 1;
    const total = counts.reduce((sum, count) => sum + count, 0);
    return counts.map((count) => PROFILE_BLEND * (total ? count / total : 1 / 24) + (1 - PROFILE_BLEND) / 24);
  });
}

function enabled(userId) {
  return settings.getUserSetting(userId, SETTING, false) === true;
}

/** Switched on and with enough data: routing evens out usage. */
function active(userId, now = Date.now()) {
  return enabled(userId) && readiness(userId, now).ready;
}

function status(userId, now = Date.now()) {
  return { enabled: enabled(userId), active: active(userId, now), readiness: readiness(userId, now) };
}

function setEnabled(userId, value, now = Date.now()) {
  if (value && !readiness(userId, now).ready) throw new HttpError(409, 'NOT_ENOUGH_DATA', 'NeoRouter does not have enough data yet to even out usage.');
  settings.setUserSetting(userId, SETTING, value === true);
  return status(userId, now);
}

/**
 * True when this model's allowance is ahead of its pace and the request
 * should wait for another. Only free and plan models are paced.
 */
function held(entry, { userId, inputTokens = 0, priority = 'normal', now = Date.now() }) {
  if (priority === 'high' || !paceable(entry)) return false;
  if (entry.isPlan) {
    const plan = plans.schedule(entry, now);
    if (plan && plan.usedShare > plan.elapsedShare + PLAN_CUSHION) return true;
  }
  const hourShare = Math.max(MIN_HOUR_SHARE, hourProfile(userId, now)[new Date(now).getHours()]) * PEAK_ROOM;
  for (const scope of quota.scopesFor(entry)) {
    const daily = quota.dailyLimits(scope, now);
    if (daily.rpd === null && daily.tpd === null) continue;
    const used = quota.usageInLast(scope.key, 60, now);
    if (daily.rpd !== null && used.requests + 1 > daily.rpd * hourShare) return true;
    if (daily.tpd !== null && used.tokens + inputTokens > daily.tpd * hourShare) return true;
  }
  return false;
}

function resetForTests() {
  readinessCache.clear();
  profileCache.clear();
}

module.exports = { readiness, active, status, setEnabled, held, resetForTests, NEEDED, SETTING };
