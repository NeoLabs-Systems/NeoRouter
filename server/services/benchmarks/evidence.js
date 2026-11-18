'use strict';

const { getDatabase } = require('../../db/database');
const consent = require('./consent');
const policy = require('./policy');
const objective = require('./evaluation/objective');
const judge = require('./evaluation/judge');
const EVALUATOR_VERSIONS = [objective.VERSION, judge.VERSION];

let cache = null;
const invalidate = () => { cache = null; };
consent.events.on('revoke', invalidate);

function add(sample, candidates, verdict, now = Date.now()) {
  if (!sample.ticket || consent.ticket(sample.userId, sample.keyId) !== sample.ticket ||
    !EVALUATOR_VERSIONS.includes(verdict.version) || ![0, 0.5, 1].includes(verdict.score) ||
    !Number.isFinite(verdict.confidence) || verdict.confidence <= 0 || verdict.confidence > 1 ||
    candidates.length !== 2 || !candidates.every((entry) => entry.canonicalId) || candidates[0].canonicalId === candidates[1].canonicalId) return false;
  const db = getDatabase();
  const bucket = Math.floor(now / policy.bucketMs) * policy.bucketMs;
  const write = db.prepare(`INSERT INTO benchmark_evidence VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
    ON CONFLICT DO UPDATE SET samples = samples + 1, confidence_sum = confidence_sum + excluded.confidence_sum,
      score_sum = score_sum + excluded.score_sum WHERE samples < ?`);
  db.transaction(() => {
    for (const [index, entry] of candidates.entries()) {
      const score = index === 0 ? verdict.score : 1 - verdict.score;
      write.run(sample.userId, entry.canonicalId, sample.task, verdict.evaluator, verdict.version, bucket,
        verdict.confidence, verdict.confidence * score, policy.maxDailySamplesPerGroup);
    }
  })();
  invalidate();
  return true;
}

function purge(userId) {
  consent.revoke(userId);
  getDatabase().prepare('DELETE FROM benchmark_evidence WHERE user_id = ?').run(userId);
  invalidate();
}

function prune(now = Date.now()) {
  if (getDatabase().prepare('DELETE FROM benchmark_evidence WHERE bucket <= ?').run(now - policy.evidenceLifetimeMs).changes) invalidate();
}

function snapshot(now) {
  if (cache && now >= cache.at && now - cache.at < policy.tickMs) return cache;
  const enabled = consent.globalSettings().enabled;
  const groups = new Map();
  if (enabled) {
    const rows = getDatabase().prepare(`SELECT canonical_id, task, evaluator, version, bucket, SUM(samples) samples,
      SUM(confidence_sum) confidence, SUM(score_sum) score FROM benchmark_evidence WHERE bucket > ? AND version IN (?, ?)
      GROUP BY canonical_id, task, evaluator, version, bucket`).iterate(now - policy.evidenceLifetimeMs, ...EVALUATOR_VERSIONS);
    for (const row of rows) {
      const key = JSON.stringify([row.canonical_id, row.task]);
      const group = groups.get(key) || { model: row.canonical_id, task: row.task, samples: 0, effective: 0, weight: 0, score: 0, evaluatedAt: 0, evaluators: new Set() };
      const freshness = 2 ** (-Math.max(0, now - row.bucket) / policy.halfLifeMs);
      group.samples += row.samples;
      group.effective += row.samples * freshness;
      group.weight += row.confidence * freshness;
      group.score += row.score * freshness;
      group.evaluatedAt = Math.max(group.evaluatedAt, row.bucket);
      group.evaluators.add(JSON.stringify([row.evaluator, row.version]));
      groups.set(key, group);
    }
  }
  for (const [key, group] of groups) {
    if (group.samples < policy.minSamples || !(group.weight > 0)) {
      groups.delete(key);
      continue;
    }
    group.score /= group.weight;
    group.confidence = group.weight / group.effective;
  }
  cache = { at: now, groups };
  return cache;
}

function countScores(userId = null, now = Date.now()) {
  return getDatabase().prepare(`SELECT COALESCE(SUM(samples), 0) count FROM benchmark_evidence
    WHERE bucket > ? AND version IN (?, ?) AND (? IS NULL OR user_id = ?)`)
    .get(now - policy.evidenceLifetimeMs, ...EVALUATOR_VERSIONS, userId, userId).count;
}

function list(now = Date.now()) {
  return [...snapshot(now).groups.values()].map(({ model, task, samples, score, confidence, evaluatedAt, evaluators }) => ({
    model, task, samples, score, confidence, evaluatedAt,
    evaluators: [...evaluators].map((value) => {
      const [model, version] = JSON.parse(value);
      return { model, version };
    }),
  })).sort((a, b) => b.samples - a.samples || a.model.localeCompare(b.model) || a.task.localeCompare(b.task));
}

function blend(prior, canonicalId, task, now = Date.now()) {
  if (!canonicalId || !task) return prior;
  try {
    const row = snapshot(now).groups.get(JSON.stringify([canonicalId, task]));
    if (!row || !Number.isFinite(row.score) || row.score < 0 || row.score > 1) return prior;
    const share = policy.maxRoutingShare * row.confidence * row.effective / (row.effective + policy.priorSamples);
    const direction = 2 * row.score - 1;
    return prior + ((direction > 0 ? 1 : 0) - prior) * share * Math.abs(direction);
  } catch {
    return prior;
  }
}

module.exports = { add, purge, prune, countScores, list, blend, invalidate };
