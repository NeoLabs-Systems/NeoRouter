'use strict';

const catalog = require('../catalog/catalog_service');
const quota = require('./quota_service');
const { getDatabase } = require('../../db/database');

// Tokens one answer takes when there is no history to go by yet.
const DEFAULT_TOKENS_PER_REQUEST = 1500;
const HISTORY_MS = 30 * 86_400_000;

// The user's own average tokens per answered request over the last month.
function tokensPerRequest(userId) {
  const row = getDatabase().prepare(`SELECT AVG(input_tokens + output_tokens) tokens FROM requests
    WHERE user_id = ? AND created_at >= ? AND status = 'success' AND input_tokens + output_tokens > 0`).get(userId, Date.now() - HISTORY_MS);
  return row?.tokens ? Math.round(row.tokens) : DEFAULT_TOKENS_PER_REQUEST;
}

// A scope's known limits per day and per minute, in requests and in tokens.
// A published token limit wins; otherwise requests are turned into tokens.
function limitsOf(scope, perRequest) {
  const limits = quota.effectiveLimits(scope);
  const tokens = (tokenLimit, requestLimit) => {
    if (tokenLimit !== undefined) return requestLimit === undefined ? tokenLimit.value : Math.min(tokenLimit.value, requestLimit.value * perRequest);
    return requestLimit === undefined ? null : requestLimit.value * perRequest;
  };
  return {
    perDay: limits.rpd?.value ?? null,
    perMinute: limits.rpm?.value ?? null,
    tokensPerDay: limits.rpd || limits.tpd ? tokens(limits.tpd, limits.rpd) : null,
    tokensPerMinute: limits.rpm || limits.tpm ? tokens(limits.tpm, limits.rpm) : null,
  };
}

/**
 * How many chat requests, and tokens, the free models could take if used around the
 * clock, from their known usage limits. Only real daily limits make the
 * daily figure: a per-minute limit kept up all day overstates providers that
 * also cap a day or month in ways they do not report (Cloudflare's neurons,
 * trial keys), so those are given per minute instead. Models sharing an
 * account allowance count once against it; models with no known limit are
 * counted apart.
 */
function freeCapacity(userId) {
  const entries = catalog.listForUser(userId, { capability: 'chat' })
    .filter((entry) => entry.isFree && entry.enabled && entry.accountEnabled && entry.available);
  const pools = new Map();
  const counted = new Set();
  const unknown = new Set();
  const perRequest = tokensPerRequest(userId);
  let perDay = 0;
  let perMinute = 0;
  let tokensPerDay = 0;
  let tokensPerMinute = 0;
  let unlimitedModels = 0;
  const add = (limits, key) => {
    if (limits.tokensPerDay !== null) tokensPerDay += limits.tokensPerDay;
    else tokensPerMinute += limits.tokensPerMinute;
    if (limits.perDay !== null) perDay += limits.perDay;
    else if (limits.perMinute !== null) perMinute += limits.perMinute;
    counted.add(key);
  };
  for (const entry of entries) {
    const scopes = quota.scopesFor(entry);
    const poolScope = scopes.find((scope) => scope.kind === 'pool');
    const shared = poolScope ? limitsOf(poolScope, perRequest) : { tokensPerDay: null, tokensPerMinute: null };
    if (shared.tokensPerDay !== null || shared.tokensPerMinute !== null) {
      // One allowance for every model in the pool.
      if (!pools.has(poolScope.key)) {
        pools.set(poolScope.key, true);
        add(shared, entry.accountId);
      }
      continue;
    }
    const own = limitsOf(scopes.find((scope) => scope.kind === 'model'), perRequest);
    if (own.tokensPerDay === null && own.tokensPerMinute === null) {
      unlimitedModels += 1;
      unknown.add(entry.accountId);
      continue;
    }
    add(own, entry.accountId);
  }
  for (const accountId of counted) unknown.delete(accountId);
  return {
    perDay: Math.round(perDay),
    perHour: Math.round(perDay / 24),
    // Providers that only limit per minute: what they allow each minute.
    perMinuteOnly: Math.round(perMinute),
    tokensPerDay: Math.round(tokensPerDay),
    tokensPerHour: Math.round(tokensPerDay / 24),
    tokensPerMinuteOnly: Math.round(tokensPerMinute),
    // What one request is taken to cost in tokens where only requests are limited.
    tokensPerRequest: perRequest,
    providersCounted: counted.size,
    // Free models whose provider publishes no limit we know of: not counted.
    modelsWithoutLimit: unlimitedModels,
    providersWithoutLimit: unknown.size,
  };
}

module.exports = { freeCapacity };
