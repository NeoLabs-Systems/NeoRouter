'use strict';

// How much of a shared connection its owner keeps for themself today, from
// their own use of it over the last week: what they usually still send from
// this time of day until midnight (UTC), on a busy day, plus a margin.
// Partners get the rest. A connection the owner hardly uses is almost all
// theirs to share; one the owner leans on stays theirs until they are usually
// done for the day.
const { getDatabase } = require('../../db/database');

const DAY_MS = 86_400_000;
const DAYS = 7;
// A busy day, not the busiest: the second highest of the last week, so one
// unusual spike does not lock partners out for a week.
const BUSY_RANK = 2;
const MARGIN = 1.2;
// Kept free in windows without a daily count (per minute, tokens): a margin.
const MIN_HEADROOM = 0.1;
const CACHE_MS = 5 * 60_000;
const cache = new Map();

/** Requests the owner is expected to still send on this connection today. */
function expectedOwnUse(entry, now = Date.now()) {
  const key = `${entry.accountId}|${Math.floor(now / CACHE_MS)}`;
  if (cache.has(key)) return cache.get(key);
  const dayStart = Math.floor(now / DAY_MS) * DAY_MS;
  const sinceStartOfDay = now - dayStart;
  // The owner's own requests (not partners') on each of the last full days,
  // from this time of day on.
  // CAST: numbers from JavaScript arrive as reals, which would make the day a fraction.
  const rows = getDatabase().prepare(`SELECT CAST((r.created_at - ?) / ${DAY_MS} AS INTEGER) day, COUNT(*) requests
    FROM requests r LEFT JOIN router_keys k ON k.id = r.router_key_id
    WHERE r.user_id = ? AND r.account_id = ? AND r.created_at >= ? AND r.created_at < ? AND k.peer_link_id IS NULL
      AND CAST(r.created_at - ? AS INTEGER) % ${DAY_MS} >= ?
    GROUP BY day`).all(dayStart - DAYS * DAY_MS, entry.userId, entry.accountId, dayStart - DAYS * DAY_MS, dayStart, dayStart - DAYS * DAY_MS, sinceStartOfDay);
  const perDay = Array.from({ length: DAYS }, (_, day) => rows.find((row) => row.day === day)?.requests || 0).sort((a, b) => b - a);
  const expected = Math.ceil(perDay[BUSY_RANK - 1] * MARGIN);
  if (cache.size > 5000) cache.clear();
  cache.set(key, expected);
  return expected;
}

/**
 * Why a partner may not use this connection right now, or null. quota: the
 * result of quota.evaluate for it. Daily request counts keep what the owner is
 * expected to need today; other windows keep a small margin.
 */
function keptForOwner(entry, quota, now = Date.now()) {
  if (quota.headroom < MIN_HEADROOM) return true;
  const daily = quota.details.filter((item) => item.metric === 'rpd' && item.remaining !== null && item.remaining !== undefined);
  if (!daily.length) return false;
  const keep = expectedOwnUse(entry, now);
  return daily.some((item) => item.remaining - keep < 1);
}

function resetForTests() {
  cache.clear();
}

module.exports = { expectedOwnUse, keptForOwner, MIN_HEADROOM, DAYS, MARGIN, resetForTests };
