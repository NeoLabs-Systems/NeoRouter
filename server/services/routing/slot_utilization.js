'use strict';

// Time integrals of router-observed concurrency, without request contents.
// Restarting begins a new observation interval; downtime is never called idle.
const { getDatabase } = require('../../db/database');
const { getConfig } = require('../../config/env');
const { FLAT_RATE } = require('../providers/billing_modes');
const logger = require('../../utils/logger').createLogger('slot-usage');
const states = new Map();
let timer = null;

function write(accountId, bucket, elapsed, busy, limit) {
  getDatabase().prepare(`INSERT INTO slot_utilization (account_id, bucket, observed_ms, busy_ms, capacity_ms, unknown_ms, peak)
    VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(account_id, bucket) DO UPDATE SET
    observed_ms = observed_ms + excluded.observed_ms, busy_ms = busy_ms + excluded.busy_ms,
    capacity_ms = capacity_ms + excluded.capacity_ms, unknown_ms = unknown_ms + excluded.unknown_ms,
    peak = MAX(peak, excluded.peak)`)
    .run(accountId, bucket, elapsed, busy * elapsed, (limit || 0) * elapsed, limit ? 0 : elapsed, busy);
}
function flush(accountId, now = Date.now()) {
  const state = states.get(accountId);
  if (!state || now < state.at) return;
  if (!getDatabase().prepare('SELECT 1 FROM provider_accounts WHERE id = ?').get(accountId)) { states.delete(accountId); return; }
  const { slotUsageBucketMs: bucketMs, slotUsageWindowMs: windowMs } = getConfig().routing;
  let at = Math.max(state.at, now - windowMs);
  getDatabase().transaction(() => {
    while (at < now) {
      const bucket = Math.floor(at / bucketMs) * bucketMs;
      const end = Math.min(now, bucket + bucketMs);
      write(accountId, bucket, end - at, state.busy, state.limit);
      at = end;
    }
  })();
  state.at = now;
}
function watch(accountId, now = Date.now()) {
  const row = getDatabase().prepare('SELECT definition_id, prepaid, enabled FROM provider_accounts WHERE id = ?').get(accountId);
  if (!row) { states.delete(accountId); return; }
  flush(accountId, now);
  if (row.prepaid !== FLAT_RATE || !row.enabled) { states.delete(accountId); return; }
  const entry = { accountId, definitionId: row.definition_id };
  const limit = require('./concurrency').limitFor(entry, now)?.limit || null;
  const busy = require('./load_tracker').inflight(entry).account;
  states.set(accountId, { at: now, busy, limit });
}
function transition(entry, busy, now = Date.now()) {
  if (!entry.isFlatRate && !states.has(entry.accountId)) return;
  if (!states.has(entry.accountId)) watch(entry.accountId, now);
  const state = states.get(entry.accountId);
  if (!state) return;
  flush(entry.accountId, now);
  state.busy = busy;
  state.limit = require('./concurrency').limitFor(entry, now)?.limit || null;
  write(entry.accountId, Math.floor(now / getConfig().routing.slotUsageBucketMs) * getConfig().routing.slotUsageBucketMs, 0, busy, state.limit);
}
function initialize() {
  for (const row of getDatabase().prepare('SELECT id FROM provider_accounts WHERE prepaid = ? AND enabled = 1').all(FLAT_RATE)) {
    if (!states.has(row.id)) watch(row.id);
  }
  if (getConfig().workersEnabled && !timer) {
    timer = setInterval(() => {
      for (const accountId of states.keys()) observe(() => watch(accountId));
    }, getConfig().routing.slotUsageBucketMs);
    timer.unref();
  }
}
function snapshot(accountId, now = Date.now()) {
  watch(accountId, now);
  const since = Math.floor((now - getConfig().routing.slotUsageWindowMs) / getConfig().routing.slotUsageBucketMs) * getConfig().routing.slotUsageBucketMs;
  getDatabase().prepare('DELETE FROM slot_utilization WHERE account_id = ? AND bucket < ?').run(accountId, since);
  const series = getDatabase().prepare('SELECT * FROM slot_utilization WHERE account_id = ? AND bucket >= ? ORDER BY bucket').all(accountId, since);
  const totals = series.reduce((sum, row) => ({ observed: sum.observed + row.observed_ms, busy: sum.busy + row.busy_ms, capacity: sum.capacity + row.capacity_ms, unknown: sum.unknown + row.unknown_ms, peak: Math.max(sum.peak, row.peak) }), { observed: 0, busy: 0, capacity: 0, unknown: 0, peak: 0 });
  return {
    current: states.get(accountId)?.busy || 0,
    limit: states.get(accountId)?.limit || null,
    observedMs: totals.observed,
    averageBusy: totals.observed ? totals.busy / totals.observed : null,
    peakBusy: totals.peak,
    idleShare: totals.capacity && !totals.unknown ? Math.max(0, 1 - totals.busy / totals.capacity) : null,
    series: series.map((row) => ({ bucket: row.bucket, averageBusy: row.observed_ms ? row.busy_ms / row.observed_ms : null, peakBusy: row.peak })),
  };
}
function observe(operation) {
  try { return operation(); } catch (error) { logger.warn('Could not record slot utilization', { error }); return null; }
}
module.exports = { initialize, watch: (...args) => observe(() => watch(...args)), transition: (...args) => observe(() => transition(...args)), snapshot };
