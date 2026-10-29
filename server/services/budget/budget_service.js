'use strict';

const { getDatabase } = require('../../db/database');

// Spend is the sum of ledger rows in the key's current budget period. While a
// request is in flight its worst-case cost is reserved so concurrent requests
// cannot jointly overshoot the budget.
const reservations = new Map();

function periodStart(period, now = Date.now()) {
  const date = new Date(now);
  if (period === 'daily') return Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate());
  if (period === 'monthly') return Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), 1);
  return 0;
}

function periodEnd(period, now = Date.now()) {
  const date = new Date(now);
  if (period === 'daily') return Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate() + 1);
  if (period === 'monthly') return Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + 1, 1);
  return null;
}

function spent(key, now = Date.now()) {
  const row = getDatabase().prepare('SELECT COALESCE(SUM(amount_usd), 0) total FROM budget_ledger WHERE router_key_id = ? AND created_at >= ?')
    .get(key.id, periodStart(key.budget_period, now));
  return row.total;
}

function reserved(keyId) {
  return reservations.get(keyId) || 0;
}

function remaining(key, now = Date.now()) {
  if (key.budget_usd === null || key.budget_usd === undefined) return null;
  return key.budget_usd - spent(key, now) - reserved(key.id);
}

function costOf(entry, usage) {
  if (entry.isFree || entry.isPlan || !usage) return 0;
  if (entry.priceIn === null || entry.priceOut === null) return 0;
  return ((usage.prompt_tokens || 0) * entry.priceIn + (usage.completion_tokens || 0) * entry.priceOut) / 1e6;
}

function estimateMaxCost(entry, inputTokens, maxOutputTokens) {
  if (entry.isFree || entry.isPlan) return 0;
  if (entry.priceIn === null || entry.priceOut === null) return null;
  return (inputTokens * entry.priceIn + maxOutputTokens * entry.priceOut) / 1e6;
}

function reserve(key, amount) {
  if (!amount) return { settle: (actual, requestId) => record(key, actual, requestId), release: () => {} };
  reservations.set(key.id, reserved(key.id) + amount);
  let done = false;
  const release = () => {
    if (done) return;
    done = true;
    const next = reserved(key.id) - amount;
    if (next <= 1e-12) reservations.delete(key.id);
    else reservations.set(key.id, next);
  };
  return {
    release,
    settle: (actual, requestId) => {
      release();
      record(key, actual, requestId);
    },
  };
}

function record(key, amount, requestId, now = Date.now()) {
  if (!amount) return;
  getDatabase().prepare('INSERT INTO budget_ledger (router_key_id, request_id, amount_usd, created_at) VALUES (?, ?, ?, ?)')
    .run(key.id, requestId || null, amount, now);
}

module.exports = { periodStart, periodEnd, spent, remaining, reserved, costOf, estimateMaxCost, reserve, record };
