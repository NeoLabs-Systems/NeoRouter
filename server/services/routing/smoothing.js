'use strict';

// Even out allowance consumption using actual balances, reset windows and
// learned request/token demand. Fixed windows use cumulative allowance;
// rolling windows include the remaining balance and upcoming expirations.
// It can be switched on only once there is enough data to go by (readiness).
// Keys with a high priority are never held back, and paid connections are
// not affected.
const { getDatabase } = require('../../db/database');
const settings = require('../settings/settings_service');
const quota = require('../quota/quota_service');
const plans = require('./plans');
const policy = require('./pacing_policy');
const load = require('./load_tracker');
const { HttpError } = require('../../middleware/error_handler');

const SETTING = 'smooth_allowances';
const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;
const CACHE_MS = policy.cacheMs;
const NEEDED = Object.freeze({ days: 7, activeDays: 5, requests: 100, coverage: 0.5 });
const PEER_DEFINITION = 'neorouter_peer';

const readinessCache = new Map();
const profileCache = new Map();

function cached(cache, userId, now, compute) {
  const hit = cache.get(userId);
  if (hit && now >= hit.at && now - hit.at < CACHE_MS) return hit.value;
  const value = compute();
  cache.set(userId, { at: now, value });
  return value;
}

// Only free and plan models have an allowance to spread; paid ones are
// billed per use and pool partners keep their own limits.
const paceable = (entry) => !entry.isFlatRate && (entry.isFree || entry.isPlan) && entry.definitionId !== PEER_DEFINITION;

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

// Learn request and token demand independently in UTC hour-of-week bins.
// Recent successful work counts more; retries and provider errors do not
// teach a false peak. UTC keeps the profile stable across server moves.
function demandProfile(userId, now) {
  return cached(profileCache, userId, now, () => {
    const requests = new Array(168).fill(0);
    const tokens = new Array(168).fill(0);
    const output = new Map();
    let totalWeight = 0;
    let totalOutput = 0;
    const rows = getDatabase().prepare(`SELECT created_at, model_ref, account_id, input_tokens, output_tokens FROM requests
      WHERE user_id = ? AND created_at >= ? AND created_at <= ? AND status = 'success'`);
    for (const row of rows.iterate(userId, now - policy.historyMs, now)) {
      const date = new Date(row.created_at);
      const bin = date.getUTCDay() * 24 + date.getUTCHours();
      const weight = Math.pow(0.5, (now - row.created_at) / policy.historyHalfLifeMs);
      requests[bin] += weight;
      tokens[bin] += weight * (row.input_tokens + row.output_tokens);
      totalWeight += weight;
      totalOutput += weight * row.output_tokens;
      for (const key of [`m:${row.model_ref}`, `p:${row.account_id}`]) {
        const value = output.get(key) || { sum: 0, weight: 0 };
        value.sum += weight * row.output_tokens;
        value.weight += weight;
        output.set(key, value);
      }
    }
    const shape = (counts) => {
      const sum = counts.reduce((total, count) => total + count, 0);
      const values = counts.map((count) => policy.profileBlend * (sum ? count / sum : 1 / 168) + (1 - policy.profileBlend) / 168);
      const prefix = [0];
      for (const value of values) prefix.push(prefix.at(-1) + value);
      return { values, prefix };
    };
    return { requests: shape(requests), tokens: shape(tokens), output, meanOutput: totalWeight ? totalOutput / totalWeight : 0 };
  });
}

const WEEK_MS = 7 * DAY_MS;
const WEEK_EPOCH = Date.UTC(1970, 0, 4); // Sunday, as getUTCDay().
function mass(shape, start, end) {
  const cumulative = (time) => {
    const weeks = Math.floor((time - WEEK_EPOCH) / WEEK_MS);
    const phase = time - WEEK_EPOCH - weeks * WEEK_MS;
    const hour = Math.min(167, Math.floor(phase / HOUR_MS));
    return weeks + shape.prefix[hour] + shape.values[hour] * ((phase % HOUR_MS) / HOUR_MS);
  };
  return Math.max(0, cumulative(end) - cumulative(start));
}
function expectedOutput(profile, scope, maxOutput = null) {
  const learned = profile.output.get(scope);
  const count = learned?.weight || 0;
  const output = (learned?.sum || 0) / Math.max(1, count);
  const confidence = count / (count + policy.outputPriorSamples);
  const estimate = output * confidence + profile.meanOutput * (1 - confidence);
  return maxOutput > 0 ? Math.min(maxOutput, estimate) : estimate;
}

function enabled(userId) {
  return settings.getUserSetting(userId, SETTING, false) === true;
}

/** Readiness gates initial enablement; new unknown models must not unpace
 * allowances that were already learned and explicitly protected. */
function active(userId) {
  return enabled(userId);
}

function status(userId, now = Date.now()) {
  return { enabled: enabled(userId), active: active(userId, now), readiness: readiness(userId, now) };
}

function setEnabled(userId, value, now = Date.now()) {
  if (value && !enabled(userId) && !readiness(userId, now).ready) throw new HttpError(409, 'NOT_ENOUGH_DATA', 'NeoRouter does not have enough data yet to even out usage.');
  settings.setUserSetting(userId, SETTING, value === true);
  return status(userId, now);
}

// A decision carries both score pressure and the actual next release time.
// The queue can wake for that time instead of retrying every five minutes.
function decision(entry, { userId, inputTokens = 0, maxOutput = null, priority = 'normal', now = Date.now() }) {
  if (priority === 'high' || !paceable(entry)) return { held: false, factor: 1, nextAt: null };
  const profile = demandProfile(userId, now);
  const violations = [];
  let pressure = 0;
  const fixed = (used, cost, startAt, resetAt, cushion, shape) => {
    const whole = mass(shape, startAt, resetAt);
    const elapsed = whole ? mass(shape, startAt, now) / whole : 1;
    const allowance = Math.min(1, elapsed + cushion);
    pressure = Math.max(pressure, (used + cost) / Math.max(allowance, Number.EPSILON));
    if (used + cost <= allowance) return;
    // Monotonic cumulative demand: find when this request earns enough room.
    let low = now;
    let high = resetAt;
    for (let i = 0; i < 32 && high - low > 1000; i += 1) {
      const mid = (low + high) / 2;
      if (mass(shape, startAt, mid) / whole + cushion >= used + cost) high = mid;
      else low = mid;
    }
    violations.push(Math.ceil(high));
  };
  if (entry.isPlan) for (const window of plans.schedules(entry, now)) {
    fixed(window.usedShare, window.requestShare, window.startAt, window.resetAt, policy.planCushion, profile.tokens);
  }
  for (const scope of quota.scopesFor(entry)) {
    const parallel = load.inflight(entry)[scope.kind === 'pool' ? 'account' : 'model'];
    const output = expectedOutput(profile, scope.key, maxOutput);
    for (const metric of ['rpd', 'tpd']) {
      const window = quota.dailyWindow(scope, metric, now);
      if (!window) continue;
      const isTokens = metric === 'tpd';
      const shape = isTokens ? profile.tokens : profile.requests;
      const cost = isTokens ? inputTokens + output : 1;
      const pending = isTokens ? parallel * output : 0;
      if (!window.rolling) {
        fixed((window.used + pending) / window.limit, cost / window.limit, window.startAt, window.resetAt, policy.dailyCushion, shape);
        continue;
      }
      const whole = mass(shape, now, now + DAY_MS);
      const hourShare = Math.max(policy.minHourShare, mass(shape, now, now + HOUR_MS) / whole) * policy.peakRoom;
      const allowance = Math.min(window.limit * hourShare, (window.remaining + window.recent) * hourShare + window.expiring);
      const proposed = window.recent + pending + cost;
      pressure = Math.max(pressure, proposed / Math.max(allowance, Number.EPSILON));
      if (proposed > allowance + 1e-6) {
        const release = quota.nextReleaseAt(scope.key, 60, now);
        const dailyRelease = quota.nextReleaseAt(scope.key, 1440, now);
        violations.push(Math.min(release || Infinity, dailyRelease || Infinity, now + (pending > 0 ? policy.inflightRecheckMs : policy.retrySearchMs)));
      }
    }
  }
  return { held: violations.length > 0, factor: 1 / (1 + Math.max(0, pressure - policy.softPressureStart) * policy.softPressureWeight), nextAt: violations.length ? Math.max(...violations) : null };
}
function held(entry, context) { return decision(entry, context).held; }
function factor(entry, context) { return decision(entry, context).factor; }
function nextAt(entry, context) { return decision(entry, context).nextAt; }

function resetForTests() {
  readinessCache.clear();
  profileCache.clear();
}

module.exports = { readiness, active, status, setEnabled, held, factor, nextAt, decision, resetForTests, NEEDED, SETTING };
