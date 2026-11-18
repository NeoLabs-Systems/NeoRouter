'use strict';

const { HttpError } = require('../../middleware/error_handler');

const active = new Map();
const inFlight = (key) => active.get(key.id) || 0;

// Count whole client requests, including streams and internal retries.
// Track uncapped keys so a cap added mid-flight applies immediately.
function acquire(key) {
  const count = inFlight(key);
  if (key.concurrent_limit && count >= key.concurrent_limit) {
    throw new HttpError(429, 'KEY_CONCURRENCY_LIMITED', 'This router key has reached its limit for simultaneous requests.');
  }
  active.set(key.id, count + 1);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    const remaining = inFlight(key) - 1;
    if (remaining) active.set(key.id, remaining);
    else active.delete(key.id);
  };
}

async function run(key, callback) {
  const release = acquire(key);
  try { return await callback(); } finally { release(); }
}

module.exports = { acquire, inFlight, run };
