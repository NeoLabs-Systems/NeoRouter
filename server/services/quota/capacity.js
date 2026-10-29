'use strict';

const catalog = require('../catalog/catalog_service');
const quota = require('./quota_service');

// A scope's known limits: requests per day, and per minute.
function limitsOf(scope) {
  const limits = quota.effectiveLimits(scope);
  return { perDay: limits.rpd?.value ?? null, perMinute: limits.rpm?.value ?? null };
}

/**
 * How many chat requests the free models could take if used around the
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
  let perDay = 0;
  let perMinute = 0;
  let unlimitedModels = 0;
  const add = (limits, key) => {
    if (limits.perDay !== null) perDay += limits.perDay;
    else perMinute += limits.perMinute;
    counted.add(key);
  };
  for (const entry of entries) {
    const scopes = quota.scopesFor(entry);
    const poolScope = scopes.find((scope) => scope.kind === 'pool');
    const shared = poolScope ? limitsOf(poolScope) : { perDay: null, perMinute: null };
    if (shared.perDay !== null || shared.perMinute !== null) {
      // One allowance for every model in the pool.
      if (!pools.has(poolScope.key)) {
        pools.set(poolScope.key, true);
        add(shared, entry.accountId);
      }
      continue;
    }
    const own = limitsOf(scopes.find((scope) => scope.kind === 'model'));
    if (own.perDay === null && own.perMinute === null) {
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
    providersCounted: counted.size,
    // Free models whose provider publishes no limit we know of: not counted.
    modelsWithoutLimit: unlimitedModels,
    providersWithoutLimit: unknown.size,
  };
}

module.exports = { freeCapacity };
