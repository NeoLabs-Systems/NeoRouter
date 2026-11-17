'use strict';
const { getDatabase } = require('../../db/database');
const consent = require('./consent');
const POLICY = require('./policy');
const { VERSION } = require('./evaluation/judge');
const aggregates = require('./aggregates');
let cachedAt = null, enabled = false, revision = -1, rows = new Map();
consent.events.on('revoke', () => { cachedAt = null; });
function snapshot(now) {
  // The switch is read at most once a second: scoring runs per candidate per request.
  if (cachedAt === null || now - cachedAt >= 1000 || now < cachedAt) {
    cachedAt = now;
    enabled = !!consent.globalSettings().enabled;
  }
  if (enabled && revision !== aggregates.revision()) {
    rows = new Map(getDatabase().prepare('SELECT * FROM internal_benchmarks WHERE evaluator_version = ?').all(VERSION).map((row) => [`${row.canonical_id}:${row.task}`, row]));
    revision = aggregates.revision();
  }
  return enabled;
}

// Additional task quality evidence only. External intelligence, production
// experience, reliability, speed, price and quotas keep their existing roles.
function adjust(prior, canonicalId, task, now) {
  if (!task || !snapshot(now)) return prior;
  const row = rows.get(`${canonicalId}:${task}`);
  if (!row || row.samples < POLICY.minSamples || !Number.isFinite(row.score) || row.score < 0 || row.score > 1
    || !Number.isFinite(row.confidence) || row.confidence < 0 || row.confidence > 1
    || !Number.isFinite(row.effective_samples) || row.effective_samples <= 0) return prior;
  const effective = row.effective_samples * 2 ** (-Math.max(0, now - row.updated_at) / POLICY.halfLifeMs);
  const share = POLICY.maxRoutingShare * row.confidence * effective / (effective + POLICY.priorSamples);
  // Preference is relative, not absolute intelligence: neutral ties must not
  // pull strong externally measured models towards an arbitrary midpoint.
  const direction = 2 * row.score - 1;
  const target = direction >= 0 ? 1 : 0;
  return prior + (target - prior) * share * Math.abs(direction);
}
function blend(prior, canonicalId, task, now = Date.now()) {
  try { return adjust(prior, canonicalId, task, now); }
  catch {
    // Benchmark storage is supplementary; preserve established routing if it
    // is unavailable. Do not log anything from the request or staged content.
    enabled = false; cachedAt = now;
    return prior;
  }
}
module.exports = { blend };
