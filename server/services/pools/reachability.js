'use strict';

// Whether a pool partner answered the last time we tried, so the pool page
// shows "not reachable" as soon as a partner goes down, and "online" again as
// soon as it answers. Kept in memory: a restart starts optimistic.
const { KINDS } = require('../../providers/provider_error');

const UNREACHABLE = new Set([KINDS.NETWORK, KINDS.TIMEOUT]);
const FORGET_AFTER_MS = 30 * 60_000;
const down = new Map();

function noteSuccess(accountId) {
  down.delete(accountId);
}

function noteFailure(accountId, kind, now = Date.now()) {
  if (UNREACHABLE.has(kind)) down.set(accountId, now);
}

function isDown(accountId, now = Date.now()) {
  const since = down.get(accountId);
  if (since === undefined) return false;
  if (now - since > FORGET_AFTER_MS) {
    down.delete(accountId);
    return false;
  }
  return true;
}

module.exports = { noteSuccess, noteFailure, isDown };
