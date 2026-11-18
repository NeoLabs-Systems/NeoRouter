'use strict';

const { createHash } = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { getConfig } = require('../../config/env');
const sessions = new Map();
const digest = (scope, session) => createHash('sha256').update(`${scope}\n${session}`).digest('hex');

function cachedTokens(usage) {
  return Math.max(0, Math.min(usage?.prompt_tokens || 0, usage?.prompt_tokens_details?.cached_tokens || usage?.cache_read_input_tokens || 0));
}
function remember(scope, session, entry, now = Date.now()) {
  if (!session) return;
  const config = getConfig().routing;
  for (const [key, value] of sessions) if (now - value.at > config.cacheAffinityTtlMs) sessions.delete(key);
  const key = digest(scope, session);
  sessions.delete(key);
  sessions.set(key, { ref: entry.ref, at: now });
  while (sessions.size > config.cacheAffinityMaxSessions) sessions.delete(sessions.keys().next().value);
}
function affinity(scope, session, now = Date.now()) {
  const hit = session && sessions.get(digest(scope, session));
  return hit && now - hit.at <= getConfig().routing.cacheAffinityTtlMs ? hit.ref : null;
}
// The recent measured share of input served from cache. Unknown and zero-hit
// models receive zero benefit. A rolling window lets changed providers relearn.
function rates(userId, now = Date.now()) {
  return new Map(getDatabase().prepare(`SELECT model_ref, SUM(cached_input_tokens) cached, SUM(input_tokens) input
    FROM requests WHERE user_id = ? AND status = 'success' AND created_at >= ? GROUP BY model_ref`)
    .all(userId, now - getConfig().routing.cacheLearningWindowMs)
    .map((row) => [row.model_ref, row.input > 0 ? Math.min(1, row.cached / row.input) : 0]));
}
function bonus(entry, hitRate, inputTokens) {
  if (!(hitRate > 0)) return 0;
  const config = getConfig().routing;
  const discount = Number.isFinite(entry.priceCachedIn) && entry.priceIn > 0 ? Math.max(0, 1 - entry.priceCachedIn / entry.priceIn) : 0;
  return config.cacheAffinityMaxBonus * hitRate * (inputTokens / (inputTokens + config.cacheAffinityTokenScale)) * (1 + discount) / 2;
}
module.exports = { cachedTokens, remember, affinity, rates, bonus };
