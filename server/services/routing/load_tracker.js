'use strict';

// In-flight request counts per model and per provider account. Routing
// reads them synchronously so a burst of requests spreads out immediately,
// before any of them finishes and updates the learned stats.
const byModel = new Map();
const byAccount = new Map();

function increment(map, key, delta) {
  const next = (map.get(key) || 0) + delta;
  if (next <= 0) map.delete(key);
  else map.set(key, next);
}

function lease(entry) {
  increment(byModel, entry.ref, 1);
  increment(byAccount, entry.accountId, 1);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    increment(byModel, entry.ref, -1);
    increment(byAccount, entry.accountId, -1);
  };
}

function inflight(entry) {
  return { model: byModel.get(entry.ref) || 0, account: byAccount.get(entry.accountId) || 0 };
}

function snapshot() {
  return { models: Object.fromEntries(byModel), accounts: Object.fromEntries(byAccount) };
}

module.exports = { lease, inflight, snapshot };
