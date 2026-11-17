'use strict';
const { getDatabase } = require('../../db/database');
const consent = require('./consent');
const { VERSION } = require('./evaluation/judge');
const POLICY = require('./policy');
let revision = 0;

function rebuild(now = Date.now(), affected = null) {
  const db = getDatabase();
  if (affected?.length > POLICY.aggregationBatchSize) {
    for (let offset = 0; offset < affected.length; offset += POLICY.aggregationBatchSize) rebuild(now, affected.slice(offset, offset + POLICY.aggregationBatchSize));
    return;
  }
  const groups = new Map();
  const filter = affected?.length ? ` AND (${affected.map(() => '(canonical_id = ? AND task = ?)').join(' OR ')})` : '';
  if (affected && !affected.length) return;
  const parameters = affected ? affected.flatMap((item) => [item.canonical, item.task]) : [];
  for (const row of db.prepare(`SELECT * FROM benchmark_contributions WHERE evaluator_version = ?${filter}`).iterate(VERSION, ...parameters)) {
    const key = `${row.canonical_id}:${row.task}`;
    if (!groups.has(key)) groups.set(key, { canonical: row.canonical_id, task: row.task, count: 0, weight: 0, effective: 0, score: 0, confidence: 0, evaluatedAt: 0, judges: new Set() });
    const group = groups.get(key);
    const freshness = 2 ** (-Math.max(0, now - row.evaluated_at) / POLICY.halfLifeMs);
    const weight = freshness * row.confidence;
    group.count++; group.effective += freshness; group.weight += weight;
    group.score += row.score * weight; group.confidence += row.confidence * freshness;
    group.evaluatedAt = Math.max(group.evaluatedAt, row.evaluated_at);
    for (const judge of JSON.parse(row.judges_json)) group.judges.add(judge);
  }
  revision++;
  db.transaction(() => {
    if (!affected) db.prepare('DELETE FROM internal_benchmarks').run();
    else for (const item of affected) db.prepare('DELETE FROM internal_benchmarks WHERE canonical_id = ? AND task = ?').run(item.canonical, item.task);
    const insert = db.prepare(`INSERT INTO internal_benchmarks
      (canonical_id, task, score, samples, effective_samples, confidence, evaluated_at, updated_at, evaluator_version, judges_json)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`);
    for (const group of groups.values()) {
      if (group.count < POLICY.minSamples || !group.weight) continue;
      insert.run(group.canonical, group.task, group.score / group.weight, group.count, group.effective,
        group.confidence / group.effective, group.evaluatedAt, now, VERSION, JSON.stringify([...group.judges]));
    }
  })();
}
function add(row, result, now = Date.now()) {
  if (!consent.allowed(row.user_id, row.key_id, JSON.parse(row.consent_json))) return false;
  if (result.evaluatorVersion !== VERSION || ![...require('../routing/task/classifier').TASK_KINDS, 'general'].includes(result.task)
      || result.confidence < POLICY.minConfidence || !Number.isFinite(result.confidence) || result.confidence > 1
      || ![0, 0.5, 1].includes(result.score) || new Set(result.judges).size !== 2
      || result.candidates.length !== 2 || result.candidates[0].canonicalId === result.candidates[1].canonicalId
      || result.candidates.some((model) => result.judges.includes(model.canonicalId))) return false;
  const db = getDatabase();
  db.transaction(() => {
    const insert = db.prepare(`INSERT OR IGNORE INTO benchmark_contributions
      (user_id, fingerprint, canonical_id, task, score, confidence, evaluated_at, evaluator_version, judges_json)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`);
    for (let i = 0; i < 2; i++) {
      insert.run(row.user_id, row.fingerprint, result.candidates[i].canonicalId, result.task, i === 0 ? result.score : 1 - result.score,
        result.confidence, now, VERSION, JSON.stringify(result.judges));
    }
    const affected = result.candidates.map((model) => ({ canonical: model.canonicalId, task: result.task }));
    // Bounded rolling evidence keeps rebuild work independent of installation age.
    for (const item of affected) db.prepare(`DELETE FROM benchmark_contributions WHERE canonical_id = ? AND task = ? AND evaluator_version = ?
      AND rowid NOT IN (SELECT rowid FROM benchmark_contributions WHERE canonical_id = ? AND task = ? AND evaluator_version = ? ORDER BY evaluated_at DESC, rowid DESC LIMIT ?)`)
      .run(item.canonical, item.task, VERSION, item.canonical, item.task, VERSION, POLICY.maxEvidencePerGroup);
    rebuild(now, affected);
  })();
  return true;
}
function list({ limit = -1, offset = 0 } = {}) {
  return getDatabase().prepare(`SELECT canonical_id model, task, score, samples, confidence,
    evaluated_at evaluatedAt, evaluator_version evaluatorVersion FROM internal_benchmarks ORDER BY samples DESC, model, task LIMIT ? OFFSET ?`).all(limit, offset);
}
const count = () => getDatabase().prepare('SELECT COUNT(*) count FROM internal_benchmarks').get().count;
module.exports = { add, rebuild, list, count, revision: () => revision };
