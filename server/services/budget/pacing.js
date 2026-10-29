'use strict';

// Budget pacing: a key with a daily or monthly budget should still have
// money at the end of the period. From the spend so far and the key's recent
// usage, pacing forecasts the spend at period end. Over pace, routing leans
// toward cheaper models and caps what one request may cost; well under pace,
// it may spend more on better models. A one-time budget has no end date,
// so only the per-request cap (a share of what is left) applies.
const { getDatabase } = require('../../db/database');
const budget = require('./budget_service');

const HISTORY_MS = 7 * 86_400_000;
const MIN_OBSERVED_HOURS = 6;
const CACHE_MS = 30_000;
// Cost-weight multiplier bounds: from "spend freely" to "cheapest first".
const MIN_COST_SHIFT = 0.6;
const MAX_COST_SHIFT = 4;
// A single request may cost this many times its fair share of what is left.
const PER_REQUEST_HEADROOM = 4;
const ONE_TIME_SHARE = 0.25;
const cache = new Map();

function history(keyId, since) {
  return getDatabase().prepare('SELECT COUNT(*) requests, MIN(created_at) first FROM requests WHERE router_key_id = ? AND created_at >= ?').get(keyId, since);
}

function spendSince(keyId, since) {
  return getDatabase().prepare('SELECT COALESCE(SUM(amount_usd), 0) total, MIN(created_at) first FROM budget_ledger WHERE router_key_id = ? AND created_at >= ?').get(keyId, since);
}

/**
 * { state: 'none'|'ahead'|'on_track'|'tight'|'over', costShift, perRequestCap,
 *   spent, remaining, forecast, resetsAt } for a key, or null without a budget.
 */
function pacing(key, now = Date.now()) {
  if (key.budget_usd === null || key.budget_usd === undefined) return null;
  return compute(key, now);
}

// The week of history changes slowly, so it is cached briefly; the spend in
// the current period is always read fresh.
function recentHistory(keyId, now) {
  const cached = cache.get(keyId);
  if (cached && now - cached.at < CACHE_MS) return cached;
  const since = now - HISTORY_MS;
  const value = { at: now, since, recent: history(keyId, since), recentSpend: spendSince(keyId, since) };
  cache.set(keyId, value);
  return value;
}

function compute(key, now) {
  const spent = budget.spent(key, now);
  const remaining = Math.max(0, key.budget_usd - spent);
  const end = budget.periodEnd(key.budget_period, now);
  // Recent usage: requests and spend per hour over the last week (or since the key's first request).
  const { since, recent, recentSpend } = recentHistory(key.id, now);
  // A few minutes of history would extrapolate wildly; count at least MIN_OBSERVED_HOURS.
  const firstSeen = Math.min(recent.first || now, recentSpend.first || now);
  const observedHours = Math.max(MIN_OBSERVED_HOURS, (now - Math.max(since, firstSeen)) / 3_600_000);
  const requestsPerHour = recent.requests / observedHours;
  const spendPerHour = recentSpend.total / observedHours;

  if (!end) {
    // One-time budget: no forecast, just keep any single request modest.
    const perRequestCap = remaining * ONE_TIME_SHARE;
    return { state: remaining <= 0 ? 'over' : 'none', costShift: 1, perRequestCap, spent, remaining, forecast: null, resetsAt: null };
  }
  const start = budget.periodStart(key.budget_period, now);
  const hoursLeft = Math.max(0.25, (end - now) / 3_600_000);
  const elapsed = Math.max(1 / 60, (now - start) / 3_600_000);
  // Forecast: blend this period's own rate with the weekly history, so a
  // new period with little data still has a sensible guess.
  const periodRate = spent / elapsed;
  const rate = recentSpend.total > 0 ? 0.5 * periodRate + 0.5 * spendPerHour : periodRate;
  const forecast = spent + rate * hoursLeft;
  const ratio = key.budget_usd > 0 ? forecast / key.budget_usd : Infinity;
  // Smooth and monotone: 0.6 with nothing spent, 1 exactly on pace, 4 at twice the budget.
  const costShift = Math.min(MAX_COST_SHIFT, ratio <= 1 ? MIN_COST_SHIFT + (1 - MIN_COST_SHIFT) * ratio : ratio ** 2);
  const expectedRequests = Math.max(1, requestsPerHour * hoursLeft);
  const perRequestCap = (remaining / expectedRequests) * PER_REQUEST_HEADROOM;
  const state = remaining <= 0 ? 'over' : ratio > 1.05 ? 'tight' : ratio < 0.5 ? 'ahead' : 'on_track';
  return { state, costShift, perRequestCap, spent, remaining, forecast, resetsAt: end };
}

function resetForTests() {
  cache.clear();
}

module.exports = { pacing, resetForTests };
