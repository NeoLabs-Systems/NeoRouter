'use strict';
const metadata = require('../catalog/metadata_service');
const { aggregate } = require('./benchmarks/aggregate');
const POLICY = require('./policy');
const { getDatabase } = require('../../db/database');
let cache;

function snapshot() {
  const current = metadata.discoverySnapshot();
  const version = JSON.stringify(current.fetchedAt);
  const refs = [current.artificialAnalysis, current.openrouterBenchmarks, current.artificialAnalysisMedia, current.mteb];
  if (!cache || cache.version !== version || refs.some((ref, i) => ref !== cache.refs[i])) {
    cache = { version, refs, data: aggregate(current, metadata.benchmarkIdentity) };
  }
  return cache.data;
}

// Free chat models, keyed by benchmark id. OpenRouter counts only with every
// published charge confirmed zero (absence of prices is unknown); connected
// providers count when their own catalog marks the model free (Nous and Kilo
// ":free" variants, which OpenRouter may list as paid).
function freeListings() {
  const found = new Map();
  const add = (model) => {
    const id = `chat:${metadata.benchmarkIdentity(model.id, 'chat')}`;
    if (!found.has(id)) found.set(id, model);
  };
  for (const model of metadata.discoverySnapshot().openrouter || []) if (model.isFree === true) add(model);
  let rows = [];
  try {
    rows = getDatabase().prepare(`SELECT model_id id, COALESCE(display_name, model_id) name, MAX(context_window) contextWindow
      FROM models WHERE is_free = 1 AND available = 1 AND enabled = 1 AND capability = 'chat' GROUP BY model_id`).all();
  } catch { /* no catalog yet */ }
  for (const row of rows) add({ id: row.id, name: row.name, contextWindow: row.contextWindow, isFree: true });
  return found;
}

// Free models with benchmark evidence: their own results, or for a variant
// with none yet (a new "-flash" of a measured model) the base model's score
// scaled down by how much variants usually give up. Estimated rows say so.
function freeCandidates() {
  const byId = new Map(snapshot().models.map((model) => [model.id, model]));
  const out = [];
  for (const [id, listing] of freeListings()) {
    const own = byId.get(id);
    if (own) {
      out.push({ ...own, listing, estimated: false });
      continue;
    }
    const family = metadata.variantBase(listing.id);
    const base = family && byId.get(`chat:${metadata.benchmarkIdentity(family.base, 'chat')}`);
    if (!base) continue;
    out.push({ ...base, id, canonicalId: id.slice('chat:'.length), name: listing.name || listing.id,
      score: base.score * family.share, listing, estimated: true });
  }
  return out;
}

// Cheapest known blended price per million tokens (3 input : 1 output), for the cost chart.
function prices() {
  const found = new Map();
  for (const model of metadata.discoverySnapshot().openrouter || []) {
    if (!Number.isFinite(model.priceIn) || !Number.isFinite(model.priceOut)) continue;
    const blended = (3 * model.priceIn + model.priceOut) / 4;
    if (blended <= 0) continue;
    const id = `chat:${metadata.benchmarkIdentity(model.id, 'chat')}`;
    if (!found.has(id) || found.get(id) > blended) found.set(id, blended);
  }
  return found;
}

function list({ query = '', capability = 'chat', source = '', free = false, offset = 0, limit = POLICY.pageSize, compare = [] } = {}) {
  const data = snapshot();
  const freeModels = freeListings();
  const priced = prices();
  const present = (model) => ({ ...model, isFree: freeModels.has(model.id), price: priced.get(model.id) ?? null,
    vendor: model.canonicalId.includes('/') ? model.canonicalId.split('/')[0] : null });
  const search = query.trim().toLowerCase();
  const models = data.models.filter((model) => (!capability || model.capability === capability)
    && (!source || model.sourceScores.some((item) => item.sourceId === source))
    && (!free || freeModels.has(model.id))
    && (!search || `${model.name} ${model.canonicalId}`.toLowerCase().includes(search)));
  return {
    models: models.slice(offset, offset + limit).map(present), total: models.length, offset, limit,
    comparison: compare.slice(0, 2).map((id) => data.models.find((model) => model.id === id)).filter(Boolean).map(present),
    sources: data.sources, capabilities: [...new Set(data.models.map((model) => model.capability))],
  };
}
module.exports = { snapshot, freeListings, freeCandidates, list };
