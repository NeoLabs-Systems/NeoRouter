'use strict';

const { getDatabase } = require('../../db/database');

// How each connection really does, from the last day's attempts. Its stored
// status only says the key was accepted; a connection can pass that check and
// still refuse every request. Reading a day of requests is not free, so the
// answer is kept for a few minutes.
const DAY = 86_400_000;
const CACHE_MS = 5 * 60 * 1000;
// Enough attempts to mean something, and next to nothing coming back.
const MIN_ATTEMPTS = 20;
const FAILING_BELOW = 0.05;
const MIN_REQUESTS = 50;
const FAILED_SHARE_ALERT = 0.3;

const NOT_THE_CONNECTION = new Set(['hedge_lost', 'rate_limit', 'quota_exhausted', 'context_length', 'bad_request', 'truncated', 'aborted', 'stream_aborted']);

const cache = new Map();

function compute(userId, now) {
  const accounts = getDatabase().prepare('SELECT id, slug, label FROM provider_accounts WHERE user_id = ? AND enabled = 1').all(userId);
  const bySlug = new Map(accounts.map((account) => [account.slug, { accountId: account.id, slug: account.slug, label: account.label, attempts: 0, successes: 0, lastProblem: null, lastProblemAt: 0 }]));
  let requests = 0;
  let failed = 0;
  let noCapacity = 0;
  const rows = getDatabase().prepare('SELECT created_at, status, error_code, trail_json FROM requests WHERE user_id = ? AND created_at >= ?');
  for (const row of rows.iterate(userId, now - DAY)) {
    // A client hanging up is not the router failing it.
    if (row.error_code !== 'client_closed') {
      requests += 1;
      if (row.status === 'error') failed += 1;
      if (/no_model_available/i.test(row.error_code || '')) noCapacity += 1;
    }
    for (const step of JSON.parse(row.trail_json || '[]')) {
      // Only what says the connection itself is broken counts. A long shot
      // was expected to fail; limits, a request the model cannot take (too
      // long, an unsupported option, too few output tokens) or a lost race
      // say nothing about the connection.
      if (step.luck || NOT_THE_CONNECTION.has(step.outcome)) continue;
      const item = bySlug.get(String(step.model).split('/')[0]);
      if (!item) continue;
      item.attempts += 1;
      if (step.outcome === 'success') item.successes += 1;
      else if (step.message && row.created_at >= item.lastProblemAt) {
        item.lastProblem = step.message;
        item.lastProblemAt = row.created_at;
      }
    }
  }
  const connections = [...bySlug.values()].filter((item) => item.attempts > 0);
  const failing = connections
    .filter((item) => item.attempts >= MIN_ATTEMPTS && item.successes / item.attempts < FAILING_BELOW)
    .sort((a, b) => b.attempts - a.attempts)
    .map(({ lastProblemAt, ...item }) => item);
  return {
    requests,
    failed,
    noCapacity,
    // Most requests going unanswered is worth saying, whatever the reason.
    shortOfCapacity: requests >= MIN_REQUESTS && failed / requests >= FAILED_SHARE_ALERT,
    failing,
  };
}

function health(userId, now = Date.now()) {
  const cached = cache.get(userId);
  if (cached && now - cached.at < CACHE_MS) return cached.value;
  const value = compute(userId, now);
  cache.set(userId, { at: now, value });
  return value;
}

module.exports = { health };
