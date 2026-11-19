'use strict';

const { randomInt } = require('node:crypto');
const capture = require('./capture');
const consent = require('./consent');
const evidence = require('./evidence');
const privacy = require('./privacy');
const transport = require('./transport');
const objective = require('./evaluation/objective');
const judge = require('./evaluation/judge');
const policy = require('./policy');
const logger = require('../../utils/logger').createLogger('local-benchmarks');
const judgeIdentity = (entry) => JSON.stringify([entry.providerKey, entry.modelId]);

const running = new Map();
const calibration = new Map();
let lastOutcome = 'idle';
let lastRunAt = null;
let networkRetryAt = 0;
let lastPrunedAt = 0;
let enabled = false;
let refill = null;

consent.events.on('revoke', ({ userId, keyId }) => {
  for (const [sample, controller] of running) {
    if ((!userId || sample.userId === userId) && (!keyId || sample.keyId === keyId)) controller.abort();
  }
  if (!keyId) {
    if (userId) calibration.delete(userId);
    else calibration.clear();
  }
});

function next() {
  const now = Date.now();
  const busy = new Set([...running.keys()].map((sample) => sample.userId));
  const ready = [];
  for (const [userId, samples] of capture.staged) {
    if (busy.has(userId)) continue;
    for (const sample of samples.values()) {
      if (sample.expiresAt <= now) capture.remove(sample);
      else if (sample.nextAt <= now) ready.push(sample);
    }
  }
  return ready.sort((a, b) => b.answers.length - a.answers.length || (a.lastStepAt || 0) - (b.lastStepAt || 0))[0];
}

function fill() {
  refill = null;
  if (!enabled || !consent.globalSettings().enabled || Date.now() < networkRetryAt) return;
  while (running.size < policy.parallel) {
    const sample = next();
    if (!sample) return;
    const controller = new AbortController();
    running.set(sample, controller);
    const expiry = AbortSignal.timeout(Math.max(0, sample.expiresAt - Date.now()));
    void run(sample, AbortSignal.any([controller.signal, expiry]));
  }
}

function tick(now = Date.now()) {
  enabled = true;
  if (now - lastPrunedAt >= policy.bucketMs) {
    evidence.prune(now);
    capture.prune(now);
    transport.prune(now);
    for (const [userId, results] of calibration) {
      for (const [model, result] of results) if (result.until <= now) results.delete(model);
      if (!results.size) calibration.delete(userId);
    }
    lastPrunedAt = now;
  }
  fill();
}

function stop() {
  enabled = false;
  clearImmediate(refill);
  refill = null;
  consent.revoke();
}

function health() {
  const available = capture.count();
  return { enabled: consent.globalSettings().enabled, active: running.size > 0, running: running.size,
    counts: { pending: Math.max(0, available - running.size), running: running.size, validated: evidence.countScores() },
    lastRunAt, lastCode: lastOutcome, aggregates: evidence.list().length };
}

async function run(sample, signal) {
  lastRunAt = Date.now();
  sample.lastStepAt = lastRunAt;
  let finished = false;
  try {
    if (!transport.active(sample, signal)) throw new transport.Deferred('consent_revoked');
    sample.result ??= await step(sample, signal);
    const result = sample.result;
    if (result) {
      finished = true;
      lastOutcome = result.rejected || (evidence.add(sample, sample.answers.map(({ entry }) => entry), result)
        ? 'validated' : 'evidence_not_saved');
      logger.info('Benchmark evaluation', { stage: 'sample', code: lastOutcome });
    }
    sample.failures = 0;
  } catch (error) {
    const retry = error instanceof transport.Deferred ? error : new transport.Deferred('evaluation_failed', { failed: true });
    lastOutcome = retry.code;
    if (retry.code === 'judge_invalid') logger.info('Benchmark evaluation', { stage: 'verdict', code: retry.code });
    if (retry.failed) sample.failures++;
    finished = signal.aborted || retry.code === 'consent_revoked';
    const now = Date.now();
    const delay = retry.code === 'evaluation_failed'
      ? Math.min(policy.maxRetryMs, policy.retryMs * 2 ** sample.failures)
      : retry.failed ? policy.tickMs : policy.retryMs;
    sample.nextAt = retry.retryAt ? Math.max(retry.retryAt, now + policy.tickMs) : now + delay;
    if (retry.code === 'network_unavailable') networkRetryAt = Math.max(networkRetryAt, sample.nextAt);
  } finally {
    if (finished) capture.remove(sample);
    running.delete(sample);
    if (enabled && !refill) refill = setImmediate(fill);
  }
}

async function step(sample, signal) {
  const { canonical, answers } = sample;
  if (answers.length < 2) {
    const [entry] = transport.candidates(sample, canonical, 'auto', (candidate) =>
      !answers.some((answer) => answer.entry.canonicalId === candidate.canonicalId));
    if (!entry) throw new transport.Deferred('waiting_free_models');
    const result = await transport.call(sample, canonical, { signal, model: entry.id, ref: entry.ref });
    const clean = await privacy.sanitize(sample.userId, canonical, result.message);
    if (!clean) return { rejected: 'privacy_rejected' };
    answers.push({ entry: result.entry, message: clean.response, valid: objective.valid(canonical, result.message) });
    return null;
  }

  const [a, b] = answers;
  if (!a.valid && !b.valid) return { rejected: 'objective_constraint_failed' };
  if (a.valid !== b.valid) return { score: Number(a.valid), confidence: 1, evaluator: 'objective', version: objective.VERSION };

  const flipped = Boolean(randomInt(2));
  const request = judge.request(canonical, answers.map(({ message }) => message), flipped);
  const independent = (entry) => !answers.some((answer) => answer.entry.canonicalId === entry.canonicalId);
  const byModel = calibration.get(sample.userId) || new Map();
  calibration.set(sample.userId, byModel);
  const now = Date.now();
  for (const [model, result] of byModel) if (result.until <= now || result.version !== judge.VERSION) byModel.delete(model);
  const calibrated = (entry) => independent(entry) && byModel.get(judgeIdentity(entry))?.passed === true;

  if (!transport.candidates(sample, request, 'auto:smart', calibrated).length) {
    const available = transport.candidates(sample, request, 'auto:smart', (candidate) => {
      const state = byModel.get(judgeIdentity(candidate));
      return independent(candidate) && (!state || state.pending);
    });
    const [entry] = available;
    if (!entry) throw new transport.Deferred('waiting_calibrated_judge');
    await calibrate(sample, entry, byModel, signal);
    return null;
  }

  const result = await transport.call(sample, request, { signal, stage: 'verdict', filter: calibrated });
  const verdict = judge.verdict(result.message, flipped);
  if (verdict.code === 'judge_invalid') {
    byModel.set(judgeIdentity(result.entry), { passed: false, version: judge.VERSION, until: Date.now() + policy.retryMs });
    throw new transport.Deferred(verdict.code, { failed: true });
  }
  return verdict.code ? { rejected: verdict.code }
    : { score: verdict.score, confidence: policy.judgeConfidence, evaluator: result.entry.canonicalId, version: judge.VERSION };
}

async function calibrate(sample, entry, byModel, signal) {
  const identity = judgeIdentity(entry);
  const pending = byModel.get(identity)?.pending;
  const calibration = pending?.[0] || judge.calibration(objective.calibration(), Boolean(randomInt(2)));
  const result = await transport.call(sample, calibration.request, { signal, stage: 'calibration', ref: entry.ref });
  const passed = calibration.passes(result.message);
  const remaining = pending ? (passed ? pending.slice(1) : []) : (passed ? [] : calibration.checks);
  if (remaining.length) {
    byModel.set(identity, { pending: remaining, version: judge.VERSION, until: Date.now() + policy.sampleLifetimeMs });
    if (!pending) logger.info('Benchmark judge calibration', { stage: 'calibration', model: entry.canonicalId, code: 'single_check_fallback' });
    return;
  }
  byModel.set(identity, { passed, version: judge.VERSION,
    until: Date.now() + (passed ? policy.sampleLifetimeMs : policy.maxRetryMs) });
  logger.info('Benchmark judge calibration', { stage: 'calibration', model: entry.canonicalId, code: passed ? 'passed' : 'rejected' });
}

module.exports = { tick, stop, health };
