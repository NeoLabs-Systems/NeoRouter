'use strict';

// How many requests a provider account takes at once. Providers rarely
// document it and nobody knows it for their own key, so it is learned:
// a 429 while several requests run on the account shows the limit was
// passed (fewer than that many fit), and steady success at the learned
// limit probes one higher again. A provider definition may state a known
// limit (`limits.account.concurrent`). Learned limits fade after an hour
// without new evidence, so a single burst never caps an account for good.
const registry = require('../../providers/registry');
const loadTracker = require('./load_tracker');

const FORGET_AFTER_MS = 60 * 60 * 1000;
const SUCCESSES_TO_PROBE_UP = 10;
// Error texts that name concurrency outright (as opposed to a rate per minute).
const CONCURRENCY_PATTERN = /concurren|parallel requests|simultaneous|in.?flight|too many (open|active) (requests|connections)/i;

const learned = new Map();

function documented(entry) {
  return registry.get(entry.definitionId)?.limits?.account?.concurrent || null;
}

function current(accountId, now) {
  const item = learned.get(accountId);
  if (item && now - item.at > FORGET_AFTER_MS) {
    learned.delete(accountId);
    return null;
  }
  return item || null;
}

/** { limit, source: 'learned' | 'documented' } or null when unknown. */
function limitFor(entry, now = Date.now()) {
  const item = current(entry.accountId, now);
  const known = documented(entry);
  if (item) return { limit: known ? Math.min(item.limit, known) : item.limit, source: 'learned' };
  return known ? { limit: known, source: 'documented' } : null;
}

const isConcurrencyError = (error) => CONCURRENCY_PATTERN.test(String(error?.message || ''));

/**
 * After a rate-limit error. `running` counts the account's requests in
 * flight, the failed one included. Returns true when it looked like a
 * concurrency limit rather than a per-minute rate.
 */
function onRateLimited(entry, error, { running = loadTracker.inflight(entry).account, now = Date.now() } = {}) {
  const explicit = isConcurrencyError(error);
  if (!explicit && running < 2) return false;
  const item = current(entry.accountId, now);
  const limit = Math.max(1, running - 1);
  if (!item || limit < item.limit || explicit) learned.set(entry.accountId, { limit: Math.min(limit, item?.limit ?? limit), at: now, successes: 0 });
  else item.at = now;
  return explicit;
}

/** After a success; `running` includes the request that just finished. */
function onSuccess(entry, { running = loadTracker.inflight(entry).account, now = Date.now() } = {}) {
  const item = current(entry.accountId, now);
  if (!item || running < item.limit) return;
  item.successes += 1;
  if (item.successes >= SUCCESSES_TO_PROBE_UP) {
    item.limit += 1;
    item.successes = 0;
    item.at = now;
  }
}

function resetForTests() {
  learned.clear();
}

module.exports = { limitFor, onRateLimited, onSuccess, isConcurrencyError, resetForTests };
