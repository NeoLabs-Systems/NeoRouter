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
// "Marked as fixed" by a person: the next few failures of the connection are
// forgiven (no cooldown, no rest, no penalty) so it gets a fair new start.
const CHANCES_AFTER_FIXED = 6;
const fixed = new Map();

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
  fixed.delete(accountId);
}

/** The person says the problem is solved: forget the rest and give new chances. */
function markFixed(accountId) {
  accounts.delete(accountId);
  fixed.set(accountId, CHANCES_AFTER_FIXED);
}

/** Uses one of the new chances; true while the failure should be forgiven. */
function takeChance(accountId) {
  const left = fixed.get(accountId) || 0;
  if (left <= 0) return false;
  if (left === 1) fixed.delete(accountId);
  else fixed.set(accountId, left - 1);
  return true;
}

const chancesLeft = (accountId) => fixed.get(accountId) || 0;

function restingUntil(accountId, now = Date.now()) {
  const until = accounts.get(accountId)?.until || 0;
  return until > now ? until : null;
}

function resetForTests() {
  accounts.clear();
  fixed.clear();
}

module.exports = { onFailure, onSuccess, markFixed, takeChance, chancesLeft, restingUntil, CHANCES_AFTER_FIXED, FAILURES_TO_REST, FIRST_REST_MS, resetForTests };
