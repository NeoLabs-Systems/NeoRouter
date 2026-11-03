'use strict';

// A connection that keeps failing, whatever the model: a provider asking for
// phone verification, a free tier that refuses this account. Each of its models
// has its own cooldown, so a provider with a hundred models would otherwise
// use up every request's attempts. After enough failures in a row without one
// success the whole connection rests, longer each time; one success ends it.
// Kept in memory: a restart simply tries again.
const FAILURES_TO_REST = 30;
const FIRST_REST_MS = 5 * 60 * 1000;
const LONGEST_REST_MS = 60 * 60 * 1000;
// After a rest this many failures send it back, so it is only probed lightly.
const PROBES_AFTER_REST = 3;

const accounts = new Map();

function onFailure(accountId, now = Date.now()) {
  const item = accounts.get(accountId) || { failures: 0, rests: 0, until: 0 };
  item.failures += 1;
  if (item.failures >= FAILURES_TO_REST) {
    item.until = now + Math.min(LONGEST_REST_MS, FIRST_REST_MS * 2 ** item.rests);
    item.rests += 1;
    item.failures = FAILURES_TO_REST - PROBES_AFTER_REST;
  }
  accounts.set(accountId, item);
}

function onSuccess(accountId) {
  accounts.delete(accountId);
}

function restingUntil(accountId, now = Date.now()) {
  const until = accounts.get(accountId)?.until || 0;
  return until > now ? until : null;
}

function resetForTests() {
  accounts.clear();
}

module.exports = { onFailure, onSuccess, restingUntil, FAILURES_TO_REST, FIRST_REST_MS, resetForTests };
