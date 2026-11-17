'use strict';
const { getDatabase } = require('../../db/database');
const consent = require('./consent');
const store = require('./private_store');
const privacy = require('./privacy');
const transport = require('./transport');
const pipeline = require('./evaluation/pipeline');
const aggregates = require('./aggregates');
const POLICY = require('./policy');
const sanitizationEpoch = require('node:crypto').randomUUID();
// Samples being evaluated right now, a few at a time; the router's low
// priority keeps them behind real traffic.
const active = new Map();
const PARALLEL = 4;
let lastRunAt = null, lastCode = 'idle';
// Outcome counts for a once-a-minute summary line: codes only, never content.
const outcomes = new Map();
let summaryAt = Date.now();
const logger = require('../../utils/logger').createLogger('local-benchmarks');
function note(code) {
  outcomes.set(code, (outcomes.get(code) || 0) + 1);
  if (Date.now() - summaryAt < 60_000) return;
  logger.info('Benchmark worker', { outcomes: Object.fromEntries(outcomes), counts: store.counts() });
  outcomes.clear();
  summaryAt = Date.now();
}
consent.events.on('revoke', ({ userId, keyId }) => {
  for (const item of active.values()) {
    if ((!userId || item.row.user_id === userId) && (!keyId || item.row.key_id === keyId)) item.controller.abort(new transport.Deferred('consent_revoked'));
  }
});

// Samples already under way go first, so work finishes instead of every
// sample creeping forward one step at a time.
function claim(now) {
  const db = getDatabase();
  return db.transaction(() => {
    const row = db.prepare(`SELECT * FROM benchmark_samples WHERE payload IS NOT NULL AND expires_at > ? AND next_run_at <= ?
      AND (status = 'pending' OR (status = 'running' AND lease_until <= ?)) ORDER BY (code IS NULL AND calls > 0) DESC, next_run_at, created_at LIMIT 1`).get(now, now, now);
    if (!row) return null;
    db.prepare("UPDATE benchmark_samples SET status = 'running', lease_until = ? WHERE id = ?").run(now + POLICY.leaseMs, row.id);
    return row;
  })();
}
// Fills the free slots and waits for them; pruning runs on the scheduler's own timer.
async function tick() {
  if (!consent.globalSettings().enabled) return;
  const runs = [];
  while (active.size + runs.length < PARALLEL) {
    const row = claim(Date.now());
    if (!row) break;
    runs.push(runOne(row));
  }
  await Promise.all(runs);
}
async function runOne(row) {
  const controller = new AbortController();
  active.set(row.id, { row, controller, startedAt: Date.now() });
  lastRunAt = Date.now();
  try {
    transport.assertActive(row, controller.signal);
    const payload = store.decrypt(row.user_id, row.payload);
    const privacyRevision = store.fingerprint(row.user_id, {
      epoch: sanitizationEpoch,
      settings: require('../processing/processing_service').getSettings(row.user_id).privacy,
      localModel: require('../processing/privacy_model').enabled(),
    });
    if (payload.progress.privacyRevision !== privacyRevision) {
      const clean = await privacy.sanitize(row.user_id, payload.sample.canonical, payload.sample.response);
      if (!clean) { store.finish(row, 'privacy_rejected'); return; }
      if (JSON.stringify(clean.canonical) !== JSON.stringify(payload.sample.canonical) || JSON.stringify(clean.response) !== JSON.stringify(payload.sample.response)) {
        payload.sample = { ...payload.sample, ...clean }; payload.progress = {};
      }
      payload.progress.privacyRevision = privacyRevision;
    }
    transport.assertActive(row, controller.signal);
    const result = await pipeline.step(row, payload, controller.signal);
    transport.assertActive(row, controller.signal);
    lastCode = result.state;
    note(result.state === 'rejected' ? `rejected:${result.code}` : result.state);
    if (result.state === 'continue') store.checkpoint(row, result.payload);
    else if (result.state === 'validated') store.finish(row, aggregates.add(row, result.result) ? 'validated' : 'validation_rejected');
    else store.finish(row, result.code);
  } catch (error) {
    const code = error instanceof transport.Deferred ? error.code : 'evaluation_failed';
    lastCode = code;
    note(code);
    if (code === 'consent_revoked') { store.finish(row, code); return; }
    if (['incomplete_output', 'oversized_output', 'billed_model'].includes(code)) { store.finish(row, code); return; }
    const failure = ['provider_failure', 'provider_timeout', 'evaluation_failed'].includes(code);
    const attempts = row.attempts + (failure ? 1 : 0);
    if (attempts >= POLICY.maxAttempts) { store.finish(row, 'attempts_exhausted'); return; }
    const retryAt = error.retryAt || Date.now() + (code === 'traffic_yield' ? 0 : Math.min(POLICY.maxRetryMs, POLICY.retryMs * 2 ** attempts));
    getDatabase().prepare("UPDATE benchmark_samples SET status = 'pending', attempts = ?, next_run_at = ?, lease_until = 0, code = ? WHERE id = ? AND user_id = ?")
      .run(attempts, retryAt, code, row.id, row.user_id);
  } finally { active.delete(row.id); }
}
function health() {
  return { enabled: consent.globalSettings().enabled, active: active.size > 0, running: active.size, startedAt: active.size ? Math.min(...[...active.values()].map((item) => item.startedAt)) : null,
    lastRunAt, lastCode, counts: store.counts(), calls: getDatabase().prepare('SELECT COALESCE(SUM(calls), 0) count FROM benchmark_samples').get().count, aggregates: aggregates.count() };
}
function stop() { for (const item of active.values()) item.controller.abort(new transport.Deferred('traffic_yield')); }
module.exports = { tick, health, stop, claim };
