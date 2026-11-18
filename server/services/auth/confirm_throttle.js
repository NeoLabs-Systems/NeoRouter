'use strict';

const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');

// Asking a signed-in account for its password or a code again is throttled
// per account, not only per address, so a stolen session can't guess the
// password faster by spreading its tries over addresses. From the 5th wrong
// answer in 15 minutes each next try waits twice as long (2 s, 4 s, ...); the
// 10th locks confirmations until the oldest of those leaves the window. The
// count is the account's own: signing in is not affected, so a stolen session
// can't lock the owner out.
const FAILURE_WINDOW_MS = 15 * 60 * 1000;
const FAILURES_BEFORE_DELAY = 5;
const FAILURES_BEFORE_LOCK = 10;
const BASE_DELAY_MS = 2000;

function locked(retryAfterMs) {
  const retryAfter = Math.max(1, Math.ceil(retryAfterMs / 1000));
  const wait = retryAfter >= 90 ? `${Math.ceil(retryAfter / 60)} minutes` : `${retryAfter} seconds`;
  const error = new HttpError(429, 'CONFIRM_LOCKED', `Too many wrong passwords or codes for this account. Try again in ${wait}.`, { retryAfter });
  error.retryAfter = retryAfter;
  return error;
}

/** Refuses a try while the account is throttled: 429 CONFIRM_LOCKED with `retryAfter` seconds. */
function assertAllowed(userId, now = Date.now()) {
  const failures = getDatabase().prepare('SELECT failed_at_ms FROM confirm_failures WHERE user_id = ? AND failed_at_ms > ? ORDER BY failed_at_ms')
    .all(userId, now - FAILURE_WINDOW_MS).map((row) => row.failed_at_ms);
  if (failures.length >= FAILURES_BEFORE_LOCK) {
    throw locked(failures[failures.length - FAILURES_BEFORE_LOCK] + FAILURE_WINDOW_MS - now);
  }
  if (failures.length >= FAILURES_BEFORE_DELAY) {
    const next = failures[failures.length - 1] + BASE_DELAY_MS * 2 ** (failures.length - FAILURES_BEFORE_DELAY);
    if (now < next) throw locked(next - now);
  }
}

/**
 * Runs `check` (resolving true when the password or code is right) under the
 * account's count. The try counts as wrong before it is checked, so parallel
 * guesses count too; a right answer takes back that kind's failures.
 */
async function guard(userId, kind, check, now = Date.now()) {
  assertAllowed(userId, now);
  const db = getDatabase();
  db.prepare('DELETE FROM confirm_failures WHERE failed_at_ms <= ?').run(now - FAILURE_WINDOW_MS);
  db.prepare('INSERT INTO confirm_failures (user_id, kind, failed_at_ms) VALUES (?, ?, ?)').run(userId, kind, now);
  const ok = await check();
  if (ok) db.prepare('DELETE FROM confirm_failures WHERE user_id = ? AND kind = ?').run(userId, kind);
  return ok;
}

module.exports = { assertAllowed, guard, FAILURE_WINDOW_MS };
