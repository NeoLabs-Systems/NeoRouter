'use strict';

const { getDatabase } = require('../../db/database');

// What a person stated for an account. Read on every routing decision, so the
// rows are cached per account and dropped on every write.
const METRICS = ['rpm', 'rpd', 'tpm', 'tpd', 'concurrent'];
const cache = new Map();

function get(accountId) {
  if (!cache.has(accountId)) {
    const rows = getDatabase().prepare('SELECT metric, value FROM account_limits WHERE account_id = ?').all(accountId);
    cache.set(accountId, Object.fromEntries(rows.map((row) => [row.metric, row.value])));
  }
  return cache.get(accountId);
}

/** patch: { rpm: 20, concurrent: 4, rpd: null }. A number sets, null clears, absent keeps. */
function set(accountId, patch, now = Date.now()) {
  const db = getDatabase();
  db.transaction(() => {
    for (const metric of METRICS) {
      if (!(metric in patch)) continue;
      const value = patch[metric];
      if (value === null) db.prepare('DELETE FROM account_limits WHERE account_id = ? AND metric = ?').run(accountId, metric);
      else db.prepare(`INSERT INTO account_limits (account_id, metric, value, updated_at) VALUES (?, ?, ?, ?)
        ON CONFLICT(account_id, metric) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`).run(accountId, metric, Math.round(value), now);
    }
  })();
  cache.delete(accountId);
  return get(accountId);
}

function resetForTests() {
  cache.clear();
}

module.exports = { get, set, resetForTests, METRICS };
