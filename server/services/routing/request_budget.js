'use strict';

// What a router key may send right now, so a client can pace itself instead
// of sending requests that would only be refused or queued: the key's own
// per-minute limit, and how many requests the connected accounts take at
// once (stated, documented or learned per account; see concurrency.js).
const catalog = require('../catalog/catalog_service');
const routerKeys = require('../router_keys/router_key_service');
const keyConcurrency = require('../router_keys/key_concurrency');
const concurrency = require('./concurrency');
const loadTracker = require('./load_tracker');

function usableEntries(key) {
  return catalog.listForUser(key.user_id, { capability: 'chat' })
    .filter((entry) => entry.enabled && entry.accountEnabled && entry.available)
    .filter((entry) => {
      if (entry.isFree) return key.allow_free !== 0;
      if (entry.isPlan) return key.allow_plan !== 0;
      return key.mode !== 'free_only';
    });
}

/**
 * { requests: { perMinute, remaining, resetSeconds, intervalMs } | null,
 *   concurrent: { limit, inFlight, available } } — limit is null when at
 * least one usable account has no known cap (then nothing is promised).
 */
function forKey(key, now = Date.now()) {
  const rpm = routerKeys.rpmState(key, now);
  const accounts = new Map();
  for (const entry of usableEntries(key)) {
    if (!accounts.has(entry.accountId)) accounts.set(entry.accountId, entry);
  }
  let limit = 0;
  let unknown = accounts.size === 0;
  let inFlight = 0;
  for (const entry of accounts.values()) {
    inFlight += loadTracker.inflight(entry).account;
    const known = concurrency.limitFor(entry, now);
    if (known) limit += known.limit;
    else unknown = true;
  }
  const ownLimit = key.concurrent_limit;
  const ownInFlight = ownLimit ? keyConcurrency.inFlight(key) : 0;
  const ownAvailable = ownLimit ? Math.max(0, ownLimit - ownInFlight) : null;
  const providerAvailable = unknown ? null : Math.max(0, limit - inFlight);
  return {
    requests: rpm && { perMinute: rpm.limit, remaining: rpm.remaining, resetSeconds: rpm.resetSeconds, intervalMs: Math.ceil(60_000 / rpm.limit) },
    concurrent: ownLimit
      ? { limit: ownLimit, inFlight: ownInFlight, available: Math.min(ownAvailable, providerAvailable ?? Infinity) }
      : { limit: unknown ? null : limit, inFlight, available: providerAvailable },
  };
}

// OpenAI's own x-ratelimit-* names where they fit, so SDKs and proxies that
// already read them pace themselves; concurrency has no OpenAI header.
function headers(budget) {
  const out = {};
  if (budget.requests) {
    out['x-ratelimit-limit-requests'] = String(budget.requests.perMinute);
    out['x-ratelimit-remaining-requests'] = String(budget.requests.remaining);
    out['x-ratelimit-reset-requests'] = `${budget.requests.resetSeconds}s`;
  }
  if (budget.concurrent.limit !== null) {
    out['X-NeoRouter-Concurrency-Limit'] = String(budget.concurrent.limit);
    out['X-NeoRouter-Concurrency-Available'] = String(budget.concurrent.available);
  }
  return out;
}

module.exports = { forKey, headers };
