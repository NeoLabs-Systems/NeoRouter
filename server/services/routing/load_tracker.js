'use strict';

// In-flight request counts per model and per provider account. Routing
// reads them synchronously so a burst of requests spreads out immediately,
// before any of them finishes and updates the learned stats.
// Real (foreground) traffic, overall and per provider account. Background
// benchmark work yields only to traffic on the account it would use.
const foregroundListeners = new Set();
const foregroundByAccount = new Map();
let foregroundCount = 0;
function foreground(entry = null) {
  foregroundCount++;
  const account = entry?.accountId;
  if (account) foregroundByAccount.set(account, (foregroundByAccount.get(account) || 0) + 1);
  for (const notify of foregroundListeners) notify(account || null);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    foregroundCount--;
    if (account) {
      const next = (foregroundByAccount.get(account) || 1) - 1;
      if (next <= 0) foregroundByAccount.delete(account); else foregroundByAccount.set(account, next);
    }
  };
}
// notify(accountId) on each new foreground request; accountId is null before routing.
function onForeground(notify) { foregroundListeners.add(notify); return () => foregroundListeners.delete(notify); }
const accountBusy = (accountId) => (foregroundByAccount.get(accountId) || 0) > 0;

const byModel = new Map();
const byAccount = new Map();

function increment(map, key, delta) {
  const next = (map.get(key) || 0) + delta;
  if (next <= 0) map.delete(key);
  else map.set(key, next);
}

function lease(entry, { background = false } = {}) {
  const releaseForeground = background ? null : foreground(entry);
  increment(byModel, entry.ref, 1);
  increment(byAccount, entry.accountId, 1);
  if (!background) require('./slot_utilization').transition(entry, byAccount.get(entry.accountId) || 0);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    releaseForeground?.();
    increment(byModel, entry.ref, -1);
    increment(byAccount, entry.accountId, -1);
    if (!background) require('./slot_utilization').transition(entry, byAccount.get(entry.accountId) || 0);
  };
}

function inflight(entry) {
  return { model: byModel.get(entry.ref) || 0, account: byAccount.get(entry.accountId) || 0 };
}

function snapshot() {
  return { models: Object.fromEntries(byModel), accounts: Object.fromEntries(byAccount) };
}

module.exports = { lease, inflight, snapshot, foreground, onForeground, accountBusy, hasForeground: () => foregroundCount > 0 };
