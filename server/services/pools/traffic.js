'use strict';

// What each partner took from us ("given") and what we took from each partner
// ("taken"), per UTC day, so both sides agree on where a day ends. Counts
// only, never content.
const { getDatabase } = require('../../db/database');
const store = require('./store');

const DAY_MS = 86_400_000;
const dayOf = (now = Date.now()) => new Date(now).toISOString().slice(0, 10);

function add(linkId, direction, requests, tokens, now = Date.now()) {
  if (!linkId || (!requests && !tokens)) return;
  getDatabase().prepare(`INSERT INTO peer_usage_daily (link_id, day, direction, requests, tokens) VALUES (?, ?, ?, ?, ?)
    ON CONFLICT (link_id, day, direction) DO UPDATE SET requests = requests + excluded.requests, tokens = tokens + excluded.tokens`)
    .run(linkId, dayOf(now), direction, requests, Math.max(0, Math.round(tokens)));
}

function today(linkId, direction, now = Date.now()) {
  const row = getDatabase().prepare('SELECT requests, tokens FROM peer_usage_daily WHERE link_id = ? AND day = ? AND direction = ?').get(linkId, dayOf(now), direction);
  return { requests: row?.requests || 0, tokens: row?.tokens || 0 };
}

// Totals over the last `days` days, today included.
function window(linkId, days = 7, now = Date.now()) {
  const since = dayOf(now - (days - 1) * DAY_MS);
  const rows = getDatabase().prepare('SELECT direction, SUM(requests) requests, SUM(tokens) tokens FROM peer_usage_daily WHERE link_id = ? AND day >= ? GROUP BY direction').all(linkId, since);
  const pick = (direction) => {
    const row = rows.find((item) => item.direction === direction);
    return { requests: row?.requests || 0, tokens: row?.tokens || 0 };
  };
  return { given: pick('given'), taken: pick('taken') };
}

// After a request finished: tokens a partner used on our connections, and
// what we used on a partner's (a request through their account).
function recordFinished({ key, accountId, success, tokens }) {
  if (key?.peer_link_id) add(key.peer_link_id, 'given', 0, tokens);
  if (accountId && success) {
    const link = store.linkForAccount(accountId);
    if (link) add(link.id, 'taken', 1, tokens);
  }
}

// When the day's share resets (next UTC midnight).
function nextReset(now = Date.now()) {
  return Math.ceil((now + 1) / DAY_MS) * DAY_MS;
}

module.exports = { add, today, window, recordFinished, nextReset, dayOf };
