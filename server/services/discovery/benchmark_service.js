'use strict';
const metadata = require('../catalog/metadata_service');
const { aggregate } = require('./benchmarks/aggregate');
const POLICY = require('./policy');
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

// Only explicitly confirmed zero prices qualify; absence of prices is unknown.
function freeListings() {
  const found = new Map();
  for (const model of metadata.discoverySnapshot().openrouter || []) {
    if (model.isFree !== true) continue;
    const canonical = metadata.benchmarkIdentity(model.id, 'chat');
    const id = `chat:${canonical}`;
    if (!found.has(id)) found.set(id, model);
  }
  return found;
}

function list({ query = '', capability = 'chat', source = '', free = false, offset = 0, limit = POLICY.pageSize, compare = [] } = {}) {
  const data = snapshot();
  const freeModels = freeListings();
  const present = (model) => ({ ...model, isFree: freeModels.has(model.id) });
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
module.exports = { snapshot, freeListings, list };
