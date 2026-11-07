'use strict';

// Whether a user's requests are outrunning what their models can take, from how
// their last requests ended. While they are, waiting rarely gets an answer and
// a client is better off told at once, and a second attempt on the side only
// spends free quota somebody else is queueing for.
const WINDOW = 200;
const MIN_SAMPLES = 50;
const SHORT_OF_CAPACITY = 0.3;
const SHORT_WAIT_MS = 5000;

const users = new Map();

function record(userId, gotAnAnswer) {
  if (!users.has(userId)) users.set(userId, []);
  const recent = users.get(userId);
  recent.push(gotAnAnswer);
  if (recent.length > WINDOW) recent.shift();
}

function isShort(userId) {
  const recent = users.get(userId);
  if (!recent || recent.length < MIN_SAMPLES) return false;
  return recent.filter((ok) => !ok).length / recent.length >= SHORT_OF_CAPACITY;
}

// How long a request may wait for a model to free up. A high-priority key still
// waits in full: lower priorities leave what it waits for alone.
function queueWaitMs(key, configured) {
  return isShort(key.user_id) && key.priority !== 'high' ? Math.min(configured, SHORT_WAIT_MS) : configured;
}

function resetForTests() {
  users.clear();
}

module.exports = { record, isShort, queueWaitMs, SHORT_WAIT_MS, MIN_SAMPLES, resetForTests };
