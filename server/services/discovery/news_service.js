'use strict';
const { getDatabase } = require('../../db/database');
const metadata = require('../catalog/metadata_service');
const benchmarks = require('./benchmark_service');
const POLICY = require('./policy');

// Called after metadata refresh, not by page reads. A model gets one persistent
// detection event, even if feeds restart, disappear temporarily or are reordered.
function observe(now = Date.now()) {
  const sourceAt = Date.parse(metadata.discoverySnapshot().fetchedAt.openrouter || '');
  if (!Number.isFinite(sourceAt)) return;
  const free = benchmarks.freeListings();
  const candidates = benchmarks.snapshot().models.filter((model) => free.has(model.id));
  const threshold = new Map();
  for (const capability of new Set(candidates.map((model) => model.capability))) {
    const scores = candidates.filter((model) => model.capability === capability).map((model) => model.score).sort((a, b) => a - b);
    threshold.set(capability, scores[Math.ceil((scores.length - 1) * POLICY.newsQuantile)]);
  }
  const db = getDatabase();
  const available = db.prepare('UPDATE discovery_news SET available = 1, last_seen_at = ? WHERE id = ?');
  const upsert = db.prepare(`INSERT INTO discovery_news
    (id, canonical_id, capability, model_id, name, score, source_count, context_window, detected_at, last_seen_at, available)
    VALUES (@id, @canonicalId, @capability, @modelId, @name, @score, @sourceCount, @contextWindow, @detectedAt, @lastSeenAt, 1)
    ON CONFLICT(id) DO UPDATE SET name = excluded.name, model_id = excluded.model_id,
      score = excluded.score, source_count = excluded.source_count, context_window = excluded.context_window,
      last_seen_at = excluded.last_seen_at, available = 1`);
  db.transaction(() => {
    db.prepare('UPDATE discovery_news SET available = 0').run();
    for (const [id] of free) available.run(sourceAt, id);
    for (const model of candidates) {
      if (model.score < threshold.get(model.capability)) continue;
      const listing = free.get(model.id);
      upsert.run({ id: model.id, canonicalId: model.canonicalId, capability: model.capability,
        modelId: listing.id, name: model.name, score: model.score, sourceCount: model.sourceCount,
        contextWindow: listing.contextWindow ?? null, detectedAt: now, lastSeenAt: sourceAt });
    }
  })();
}

function list({ query = '', offset = 0, limit = POLICY.pageSize, asOf = Date.now() } = {}) {
  // instr is literal search: '%' and '_' from a search box are not wildcards.
  const search = query.trim().toLowerCase();
  const db = getDatabase();
  const where = 'WHERE detected_at <= ? AND (? = \'\' OR instr(lower(name || \' \' || model_id), ?) > 0)';
  const total = db.prepare(`SELECT COUNT(*) count FROM discovery_news ${where}`).get(asOf, search, search).count;
  const items = db.prepare(`SELECT * FROM discovery_news ${where} ORDER BY detected_at DESC, id LIMIT ? OFFSET ?`).all(asOf, search, search, limit, offset).map((row) => ({
    id: row.id, modelId: row.model_id, name: row.name, capability: row.capability,
    score: row.score, sourceCount: row.source_count, contextWindow: row.context_window,
    detectedAt: row.detected_at, lastSeenAt: row.last_seen_at, available: !!row.available,
    url: `https://openrouter.ai/${encodeURIComponent(row.model_id).replace(/%2F/g, '/')}`,
  }));
  return { items, total, offset, limit, asOf, updatedAt: metadata.discoverySnapshot().fetchedAt.openrouter || null };
}
module.exports = { observe, list };
