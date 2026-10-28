'use strict';

const { getDatabase } = require('../../db/database');
const registry = require('../../providers/registry');
const { parseRateLimitHeaders } = require('./header_parser');

// Usage is counted in one-minute buckets per scope. A scope is either one
// model ("m:<ref>") or a pool shared by several models of one account
// ("p:<accountId>"). Buckets live in memory and are flushed to SQLite.
const METRICS = ['rpm', 'rpd', 'tpm', 'tpd'];
const MINUTE = 60_000;
const DAY_MINUTES = 1440;
const HEADROOM_THRESHOLD = 0.2;
const HEADROOM_FLOOR = 0.1;
const LIMIT_SOURCES_RANK = { manual: 3, header: 2, learned: 1, curated: 0 };

const buckets = new Map();
const dirty = new Set();
const limits = new Map();
const states = new Map();
let loaded = false;

const minuteOf = (time) => Math.floor(time / MINUTE);

function load() {
  if (loaded) return;
  loaded = true;
  const db = getDatabase();
  const cutoff = minuteOf(Date.now()) - DAY_MINUTES - 1;
  db.prepare('DELETE FROM quota_buckets WHERE minute < ?').run(cutoff);
  for (const row of db.prepare('SELECT * FROM quota_buckets').all()) {
    if (!buckets.has(row.scope_key)) buckets.set(row.scope_key, new Map());
    buckets.get(row.scope_key).set(row.minute, { requests: row.requests, tokens: row.tokens });
  }
  for (const row of db.prepare('SELECT * FROM quota_limits').all()) {
    limits.set(`${row.scope_key}|${row.metric}`, { value: row.value, source: row.source, updatedAt: row.updated_at });
  }
  for (const row of db.prepare('SELECT * FROM quota_state').all()) {
    states.set(`${row.scope_key}|${row.metric}`, { limit: row.limit_value, remaining: row.remaining, resetAt: row.reset_at, source: row.source, observedAt: row.observed_at });
  }
}

function flush() {
  if (!loaded || dirty.size === 0) return;
  const db = getDatabase();
  const upsert = db.prepare(`INSERT INTO quota_buckets (scope_key, minute, requests, tokens) VALUES (?, ?, ?, ?)
    ON CONFLICT(scope_key, minute) DO UPDATE SET requests = excluded.requests, tokens = excluded.tokens`);
  db.transaction(() => {
    for (const key of dirty) {
      const [scope, minute] = key.split('|');
      const bucket = buckets.get(scope)?.get(Number(minute));
      if (bucket) upsert.run(scope, Number(minute), bucket.requests, bucket.tokens);
    }
  })();
  dirty.clear();
  const cutoff = minuteOf(Date.now()) - DAY_MINUTES - 1;
  for (const map of buckets.values()) for (const minute of map.keys()) if (minute < cutoff) map.delete(minute);
}

function matches(pattern, modelId) {
  return pattern ? new RegExp(pattern, 'i').test(modelId) : true;
}

// Scopes a model's usage counts against, with curated limits for each.
function scopesFor(entry) {
  const definition = registry.get(entry.definitionId);
  const spec = definition?.limits || { defaults: {}, models: [], account: {} };
  const modelCurated = { ...spec.defaults };
  for (const rule of spec.models || []) {
    if (new RegExp(rule.match, 'i').test(entry.modelId)) Object.assign(modelCurated, rule);
  }
  delete modelCurated.match;
  const scopes = [{ key: `m:${entry.ref}`, curated: modelCurated, kind: 'model' }];
  const accountCurated = spec.account || {};
  const poolApplies = matches(spec.poolMatch, entry.modelId);
  if (poolApplies && (Object.keys(accountCurated).length || spec.pool === 'account')) {
    scopes.push({ key: `p:${entry.accountId}`, curated: accountCurated, kind: 'pool' });
  }
  return scopes;
}

function addUsage(scope, requests, tokens, now = Date.now()) {
  load();
  const minute = minuteOf(now);
  if (!buckets.has(scope)) buckets.set(scope, new Map());
  const map = buckets.get(scope);
  const bucket = map.get(minute) || { requests: 0, tokens: 0 };
  bucket.requests += requests;
  bucket.tokens += tokens;
  map.set(minute, bucket);
  dirty.add(`${scope}|${minute}`);
}

// Sliding-window estimate: current minute plus the overlapping share of the
// previous minute, and the last 1440 minutes for daily windows.
function usageOf(scope, now = Date.now()) {
  load();
  const map = buckets.get(scope);
  const result = { rpm: 0, tpm: 0, rpd: 0, tpd: 0 };
  if (!map) return result;
  const current = minuteOf(now);
  const overlap = 1 - (now % MINUTE) / MINUTE;
  for (const [minute, bucket] of map) {
    if (minute > current - DAY_MINUTES) {
      result.rpd += bucket.requests;
      result.tpd += bucket.tokens;
    }
    if (minute === current) {
      result.rpm += bucket.requests;
      result.tpm += bucket.tokens;
    } else if (minute === current - 1) {
      result.rpm += bucket.requests * overlap;
      result.tpm += bucket.tokens * overlap;
    }
  }
  return result;
}

function effectiveLimits(scope) {
  load();
  const result = {};
  for (const metric of METRICS) {
    const stored = limits.get(`${scope.key}|${metric}`);
    if (stored) result[metric] = { value: stored.value, source: stored.source };
    else if (scope.curated[metric]) result[metric] = { value: scope.curated[metric], source: 'curated' };
  }
  return result;
}

function setLimit(scopeKey, metric, value, source) {
  load();
  const key = `${scopeKey}|${metric}`;
  const existing = limits.get(key);
  // Learned limits only ever tighten; a fresher header or manual value wins.
  if (existing && source === 'learned' && existing.value <= value) return;
  if (existing && LIMIT_SOURCES_RANK[existing.source] > LIMIT_SOURCES_RANK[source]) return;
  const record = { value: Math.max(1, Math.round(value)), source, updatedAt: Date.now() };
  limits.set(key, record);
  getDatabase().prepare(`INSERT INTO quota_limits (scope_key, metric, value, source, updated_at) VALUES (?, ?, ?, ?, ?)
    ON CONFLICT(scope_key, metric) DO UPDATE SET value = excluded.value, source = excluded.source, updated_at = excluded.updated_at`)
    .run(scopeKey, metric, record.value, source, record.updatedAt);
}

function clearLimit(scopeKey, metric) {
  load();
  limits.delete(`${scopeKey}|${metric}`);
  getDatabase().prepare('DELETE FROM quota_limits WHERE scope_key = ? AND metric = ?').run(scopeKey, metric);
}

function setState(scopeKey, metric, { limit = null, remaining = null, resetAt = null }, source, now = Date.now()) {
  load();
  const record = { limit, remaining, resetAt, source, observedAt: now };
  states.set(`${scopeKey}|${metric}`, record);
  getDatabase().prepare(`INSERT INTO quota_state (scope_key, metric, limit_value, remaining, reset_at, source, observed_at)
    VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(scope_key, metric) DO UPDATE SET limit_value = excluded.limit_value,
    remaining = excluded.remaining, reset_at = excluded.reset_at, source = excluded.source, observed_at = excluded.observed_at`)
    .run(scopeKey, metric, limit, remaining, resetAt, source, now);
}

function liveState(scopeKey, metric, now = Date.now()) {
  const state = states.get(`${scopeKey}|${metric}`);
  if (!state) return null;
  if (state.resetAt && state.resetAt <= now) return null;
  // Without a reset time an observation is trusted for one window length.
  const ttl = metric.endsWith('d') ? 86_400_000 : MINUTE;
  if (!state.resetAt && now - state.observedAt > ttl) return null;
  return state;
}

// Header observations are the most precise signal: the provider tells us its
// own counter. They also teach the limit for future windows.
function observeHeaders(entry, headers, now = Date.now()) {
  if (!headers) return;
  const definition = registry.get(entry.definitionId);
  const observations = parseRateLimitHeaders(headers, definition?.quota || {}, now);
  if (!observations.length) return;
  const scopes = scopesFor(entry);
  const target = scopes.find((scope) => scope.kind === 'pool' && definition.limits.pool === 'account') || scopes[0];
  for (const observation of observations) {
    setState(target.key, observation.metric, observation, 'header', now);
    if (observation.limit) setLimit(target.key, observation.metric, observation.limit, 'header');
  }
}

function learnLimit(entry, learned) {
  if (!learned?.metric || !learned.value) return;
  const scopes = scopesFor(entry);
  const definition = registry.get(entry.definitionId);
  const target = scopes.find((scope) => scope.kind === 'pool' && definition.limits.pool === 'account') || scopes[0];
  setLimit(target.key, learned.metric, learned.value, 'learned');
}

// Marks the scope as exhausted until the reset (used for 429/quota errors).
function markExhausted(entry, metric, resetAt, now = Date.now()) {
  const scope = scopesFor(entry)[0];
  setState(scope.key, metric, { limit: null, remaining: 0, resetAt }, 'error', now);
}

function evaluate(entry, estimatedTokens = 0, now = Date.now()) {
  let headroom = 1;
  let blocked = null;
  const details = [];
  for (const scope of scopesFor(entry)) {
    const used = usageOf(scope.key, now);
    const scopeLimits = effectiveLimits(scope);
    for (const metric of METRICS) {
      const state = liveState(scope.key, metric, now);
      const limit = scopeLimits[metric]?.value ?? state?.limit ?? null;
      let remaining = limit !== null ? limit - used[metric] : null;
      if (state && state.remaining !== null) remaining = remaining === null ? state.remaining : Math.min(remaining, state.remaining);
      if (remaining === null) continue;
      const need = metric.startsWith('r') ? 1 : estimatedTokens;
      if (remaining < need && !blocked) blocked = { metric, scope: scope.key, resetAt: state?.resetAt || null };
      if (limit) headroom = Math.min(headroom, Math.max(0, remaining) / limit);
      details.push({ scope: scope.key, kind: scope.kind, metric, limit, used: Math.round(used[metric]), remaining: Math.max(0, Math.round(remaining)), source: scopeLimits[metric]?.source || state?.source, resetAt: state?.resetAt || null });
    }
  }
  const factor = headroom >= HEADROOM_THRESHOLD ? 1 : HEADROOM_FLOOR + (1 - HEADROOM_FLOOR) * (headroom / HEADROOM_THRESHOLD);
  return { blocked, headroom, factor, details };
}

function recordRequest(entry, tokens, now = Date.now()) {
  for (const scope of scopesFor(entry)) addUsage(scope.key, 1, tokens, now);
}

function recordTokens(entry, tokens, now = Date.now()) {
  if (!tokens) return;
  for (const scope of scopesFor(entry)) addUsage(scope.key, 0, tokens, now);
}

function accountState(accountId) {
  load();
  const result = [];
  for (const [key, state] of states) {
    const [scope, metric] = key.split('|');
    if (scope === `p:${accountId}` || scope === `a:${accountId}`) result.push({ scope, metric, ...state });
  }
  return result;
}

function resetForTests() {
  buckets.clear();
  dirty.clear();
  limits.clear();
  states.clear();
  loaded = false;
}

module.exports = {
  scopesFor,
  usageOf,
  evaluate,
  recordRequest,
  recordTokens,
  observeHeaders,
  learnLimit,
  markExhausted,
  setLimit,
  clearLimit,
  setState,
  accountState,
  flush,
  resetForTests,
  METRICS,
};
