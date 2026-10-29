'use strict';

const catalog = require('../catalog/catalog_service');
const quota = require('./quota_service');

const DAY_MINUTES = 1440;

// A scope's requests per day from its known limits: the daily limit, or the
// per-minute limit kept up all day, whichever is lower. Infinity when unknown.
function dailyRequests(scope) {
  const limits = quota.effectiveLimits(scope);
  const perDay = limits.rpd?.value ?? Infinity;
  const perMinute = limits.rpm?.value ? limits.rpm.value * DAY_MINUTES : Infinity;
  return Math.min(perDay, perMinute);
}

/**
 * How many chat requests the free models could take in a day if used around
 * the clock, from every known usage limit. Models sharing an account
 * allowance count once against it; models with no known limit are counted
 * apart instead of being guessed.
 */
function freeCapacity(userId) {
  const entries = catalog.listForUser(userId, { capability: 'chat' })
    .filter((entry) => entry.isFree && entry.enabled && entry.accountEnabled && entry.available);
  const pools = new Map();
  const known = new Set();
  const unknown = new Set();
  let modelsTotal = 0;
  let unlimitedModels = 0;
  for (const entry of entries) {
    const scopes = quota.scopesFor(entry);
    const own = dailyRequests(scopes.find((scope) => scope.kind === 'model'));
    const poolScope = scopes.find((scope) => scope.kind === 'pool');
    const shared = poolScope ? dailyRequests(poolScope) : Infinity;
    if (own === Infinity && shared === Infinity) {
      unlimitedModels += 1;
      unknown.add(entry.accountId);
      continue;
    }
    known.add(entry.accountId);
    if (shared === Infinity) {
      modelsTotal += own;
      continue;
    }
    const pool = pools.get(poolScope.key) || { cap: shared, sum: 0 };
    pool.sum += Math.min(own, shared);
    pools.set(poolScope.key, pool);
  }
  let perDay = modelsTotal;
  for (const pool of pools.values()) perDay += Math.min(pool.cap, pool.sum);
  for (const accountId of known) unknown.delete(accountId);
  return {
    perDay: Math.round(perDay),
    perHour: Math.round(perDay / 24),
    providersCounted: known.size,
    // Free models whose provider publishes no limit we know of: not counted.
    modelsWithoutLimit: unlimitedModels,
    providersWithoutLimit: unknown.size,
  };
}

module.exports = { freeCapacity };
