'use strict';

// What a partner may take from us, and when.
//   1. Own requests first: a connection or model takes partner work only while
//      it keeps a reserve (allowance and parallel slots) for its owner, and
//      partner keys have the lowest priority, so they yield whenever the owner
//      waits for the same capacity.
//   2. Daily caps per partner: requests, tokens, and requests at once.
//   3. Balance: a partner who takes far more than they give (over the last
//      week) gets gradually lower caps. Never a hard stop.
// Only models on connections the owner chose to share are offered: free ones,
// and paid ones (billed or prepaid) only where the owner explicitly turned that
// on for that connection in that pool. Plan (subscription) models never.
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const quota = require('../quota/quota_service');
const concurrency = require('../routing/concurrency');
const loadTracker = require('../routing/load_tracker');
const store = require('./store');
const traffic = require('./traffic');
const { isPeerEntry } = require('./links');

const DEFAULT_SHARE = Object.freeze({ requestsPerDay: 300, tokensPerDay: 600_000, concurrent: 2, reserve: 0.3 });
const SHARE_BOUNDS = Object.freeze({ requestsPerDay: [1, 100_000], tokensPerDay: [1_000, 100_000_000], concurrent: [1, 20], reserve: [0, 0.9] });
const BALANCE = Object.freeze({ days: 7, minRequests: 100, tolerated: 3, floor: 0.25 });
const BUSY_RETRY_SECONDS = 2;

function shareSettings(pool) {
  const merged = { ...DEFAULT_SHARE, ...(pool?.share || {}) };
  for (const [field, [min, max]] of Object.entries(SHARE_BOUNDS)) {
    const value = Number(merged[field]);
    merged[field] = Number.isFinite(value) ? Math.min(max, Math.max(min, value)) : DEFAULT_SHARE[field];
  }
  return merged;
}

/**
 * Give and take with one partner over the last week, from our side.
 * factor: how much of the configured caps they get (1 = all of it).
 * state: new | balanced | giving_more (we give more) | taking_more (we take more)
 */
function balance(linkId, now = Date.now()) {
  const { given, taken } = traffic.window(linkId, BALANCE.days, now);
  const ratio = given.requests / Math.max(taken.requests, 1);
  const factor = given.requests >= BALANCE.minRequests && ratio > BALANCE.tolerated
    ? Math.max(BALANCE.floor, BALANCE.tolerated / ratio) : 1;
  const state = given.requests + taken.requests < 10 ? 'new'
    : ratio > 2 ? 'giving_more'
      : taken.requests > 2 * given.requests ? 'taking_more' : 'balanced';
  return { given, taken, factor: Number(factor.toFixed(3)), state };
}

function capsFor(linkId, settings, now = Date.now()) {
  const { factor } = balance(linkId, now);
  return {
    requests: Math.max(1, Math.floor(settings.requestsPerDay * factor)),
    tokens: Math.max(1, Math.floor(settings.tokensPerDay * factor)),
    concurrent: settings.concurrent,
    factor,
  };
}

const inflight = new Map();

/**
 * Lets one partner request in, or refuses it (429 with Retry-After when the
 * day's share is used up or too many run at once). Returns rate-limit headers
 * for the partner's router and a release function for when it is done.
 */
function admit(key, now = Date.now()) {
  const link = store.linkById(key.peer_link_id);
  const pool = link ? store.pool(key.user_id, link.poolId) : null;
  if (!link || !pool) throw new HttpError(403, 'POOL_NOT_SHARING', 'This partner is not sharing with you.');
  // Paused for now: temporary, so the partner's router retries later rather than giving up.
  if (pool.paused || link.status !== 'active') throw new HttpError(503, 'POOL_PAUSED', 'This partner paused sharing for now.', { retryAfterSeconds: 300 });
  const settings = shareSettings(pool);
  const caps = capsFor(link.id, settings, now);
  const used = traffic.today(link.id, 'given', now);
  const resetSeconds = Math.max(1, Math.ceil((traffic.nextReset(now) - now) / 1000));
  if (used.requests >= caps.requests || used.tokens >= caps.tokens) {
    throw new HttpError(429, 'POOL_SHARE_USED_UP', 'Today’s share from this partner is used up.', { retryAfterSeconds: resetSeconds });
  }
  const running = inflight.get(link.id) || 0;
  if (running >= caps.concurrent) {
    throw new HttpError(429, 'POOL_SHARE_BUSY', 'Too many requests to this partner at once.', { retryAfterSeconds: BUSY_RETRY_SECONDS });
  }
  inflight.set(link.id, running + 1);
  traffic.add(link.id, 'given', 1, 0, now);
  let released = false;
  return {
    reserve: settings.reserve,
    poolId: link.poolId,
    headers: {
      'x-ratelimit-limit-requests-day': String(caps.requests),
      'x-ratelimit-remaining-requests-day': String(Math.max(0, caps.requests - used.requests - 1)),
      'x-ratelimit-reset-requests-day': `${resetSeconds}s`,
      'x-ratelimit-limit-tokens-day': String(caps.tokens),
      'x-ratelimit-remaining-tokens-day': String(Math.max(0, caps.tokens - used.tokens)),
      'x-ratelimit-reset-tokens-day': `${resetSeconds}s`,
    },
    release() {
      if (released) return;
      released = true;
      inflight.set(link.id, Math.max(0, (inflight.get(link.id) || 1) - 1));
    },
  };
}

const sharedCache = new Map();
const SHARED_TTL_MS = 5000;

// How this connection is shared in this pool: null or { paid } (each pool has its own list).
function shareOf(poolId, accountId, now = Date.now()) {
  const id = `${poolId}|${accountId}`;
  const hit = sharedCache.get(id);
  if (hit && now - hit.at < SHARED_TTL_MS) return hit.value;
  const value = store.shareOf(poolId, accountId);
  if (sharedCache.size > 5000) sharedCache.clear();
  sharedCache.set(id, { at: now, value });
  return value;
}

// Whether partners in this pool are offered this model at all.
function offers(poolId, entry, now = Date.now()) {
  if (!poolId || isPeerEntry(entry) || entry.isPlan) return false;
  const share = shareOf(poolId, entry.accountId, now);
  return Boolean(share && (entry.isFree || share.paid));
}

// The pool a partner key belongs to (null when its link is gone).
function poolOfKey(key) {
  return key?.peer_link_id ? store.linkById(key.peer_link_id)?.poolId || null : null;
}

/**
 * For requests that come in through a partner's key: why this model may not
 * serve it (or null). Partner work never goes on to another partner.
 */
function rejection(entry, { inputTokens = 0, reserve = DEFAULT_SHARE.reserve, poolId = null } = {}, now = Date.now()) {
  if (isPeerEntry(entry)) return 'peer_no_forward';
  if (!offers(poolId, entry, now)) return 'not_shared';
  if (quota.evaluate(entry, inputTokens, now).headroom < reserve) return 'kept_for_owner';
  const parallel = concurrency.limitFor(entry, now);
  if (parallel) {
    const allowed = Math.max(1, Math.floor(parallel.limit * (1 - reserve)));
    if (loadTracker.inflight(entry).account >= allowed) return 'kept_for_owner';
  }
  return null;
}

// Whether this partner key gets anything right now (pool and link not paused).
function sharingWith(key) {
  const link = store.linkById(key.peer_link_id);
  const pool = link ? store.pool(key.user_id, link.poolId) : null;
  return Boolean(link && pool && !pool.paused && link.status === 'active');
}

function setAccountShared(userId, poolId, accountId, shared, paid) {
  const own = getDatabase().prepare("SELECT 1 FROM provider_accounts WHERE id = ? AND user_id = ? AND definition_id != 'neorouter_peer'").get(accountId, userId);
  if (!own) return false;
  store.setShared(userId, poolId, accountId, shared, paid);
  sharedCache.delete(`${poolId}|${accountId}`);
  return true;
}

function forget(linkId) {
  inflight.delete(linkId);
}

function resetForTests() {
  inflight.clear();
  sharedCache.clear();
}

module.exports = { DEFAULT_SHARE, SHARE_BOUNDS, BALANCE, shareSettings, balance, capsFor, admit, rejection, shareOf, offers, poolOfKey, sharingWith, setAccountShared, forget, resetForTests };
