'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { getDatabase } = require('../../db/database');
const { ensureRuntimeDirs } = require('../../../runtime/paths');
const { getConfig } = require('../../config/env');
const settings = require('../settings/settings_service');
const { encryptString, decryptString } = require('../../utils/crypto');
const { canonicalId, parameterCount } = require('./model_identity');
const { createLogger } = require('../../utils/logger');
const modelsDev = require('./metadata/models_dev');
const openrouterPublic = require('./metadata/openrouter_public');
const artificialAnalysis = require('./metadata/artificial_analysis');

const logger = createLogger('metadata');
const AA_SETTING = 'artificial_analysis_key';

// Heuristic weights when no benchmark index is available. Each signal is
// normalized to [0, 1]; missing signals drop out and lower the confidence.
const HEURISTIC_WEIGHTS = Object.freeze({ price: 0.45, params: 0.3, recency: 0.15, reasoning: 0.1 });
const PRICE_RANGE = [0.1, 75];
const PARAMS_RANGE = [1, 1000];
const RECENCY_START = Date.parse('2023-01-01');
const AA_INDEX_SCALE = 75;
const HEURISTIC_PRIOR = 0.4;

let state = null;

function cacheFile() {
  return path.join(ensureRuntimeDirs().data, 'metadata_cache.json');
}

function loadState() {
  if (state) return state;
  try {
    state = JSON.parse(fs.readFileSync(cacheFile(), 'utf8'));
  } catch {
    state = { modelsDev: {}, openrouter: [], artificialAnalysis: [], fetchedAt: {} };
  }
  buildIndexes();
  return state;
}

function saveState() {
  const file = cacheFile();
  const temporary = `${file}.${process.pid}.tmp`;
  fs.writeFileSync(temporary, JSON.stringify(state), { mode: 0o600 });
  fs.renameSync(temporary, file);
}

let indexes = { byCanonical: new Map() };

function addToIndex(map, key, record) {
  if (!map.has(key)) map.set(key, []);
  map.get(key).push(record);
}

function buildIndexes() {
  const byCanonical = new Map();
  for (const [providerId, models] of Object.entries(state.modelsDev || {})) {
    for (const [modelId, record] of Object.entries(models)) {
      addToIndex(byCanonical, canonicalId(modelId), { source: 'models.dev', providerId, modelId, ...record });
    }
  }
  for (const record of state.openrouter || []) addToIndex(byCanonical, canonicalId(record.id), { source: 'openrouter', ...record });
  const aa = new Map();
  for (const record of state.artificialAnalysis || []) {
    for (const key of [canonicalId(record.slug), canonicalId(record.name)]) if (key && !aa.has(key)) aa.set(key, record);
  }
  indexes = { byCanonical, aa };
}

function getArtificialAnalysisKey() {
  const value = settings.getGlobal(AA_SETTING);
  return value ? decryptString(value) : null;
}

function setArtificialAnalysisKey(key) {
  if (!key) settings.setGlobal(AA_SETTING, null);
  else settings.setGlobal(AA_SETTING, encryptString(key));
}

async function refreshSources() {
  loadState();
  const tasks = [
    ['modelsDev', () => modelsDev.fetchSource()],
    ['openrouter', () => openrouterPublic.fetchSource()],
  ];
  const aaKey = getArtificialAnalysisKey();
  if (aaKey) tasks.push(['artificialAnalysis', () => artificialAnalysis.fetchSource({ apiKey: aaKey })]);
  const results = {};
  for (const [key, run] of tasks) {
    try {
      state[key] = await run();
      state.fetchedAt[key] = new Date().toISOString();
      results[key] = 'ok';
    } catch (error) {
      results[key] = error.message;
      logger.warn('Metadata source failed', { source: key, error: error.message });
    }
  }
  buildIndexes();
  saveState();
  recomputeAll();
  return results;
}

function median(values) {
  const sorted = values.filter((value) => Number.isFinite(value)).sort((a, b) => a - b);
  if (!sorted.length) return null;
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

function logScale(value, [low, high]) {
  if (!Number.isFinite(value) || value <= 0) return null;
  const scaled = (Math.log10(value) - Math.log10(low)) / (Math.log10(high) - Math.log10(low));
  return Math.min(1, Math.max(0, scaled));
}

function firstTruthy(records, field) {
  for (const record of records) if (record[field] !== null && record[field] !== undefined) return record[field];
  return null;
}

// Combines every known source into one canonical metadata record.
function computeCanonical(canonical, sampleModelIds = []) {
  loadState();
  const records = indexes.byCanonical.get(canonical) || [];
  const aa = indexes.aa?.get(canonical) || null;
  const paidPrices = records.map((record) => record.priceOut).filter((price) => Number.isFinite(price) && price > 0);
  const marketPrice = median(paidPrices);
  const params = parameterCount(sampleModelIds[0] || '') ?? sampleModelIds.map(parameterCount).find(Boolean) ?? null;
  const releaseDate = aa?.releaseDate || firstTruthy(records, 'releaseDate') || firstTruthy(records, 'created');
  const reasoning = firstTruthy(records, 'reasoning');

  let intelligence;
  let source;
  let confidence;
  if (aa?.intelligenceIndex) {
    intelligence = Math.min(1, aa.intelligenceIndex / AA_INDEX_SCALE);
    source = 'artificial_analysis';
    confidence = 0.9;
  } else {
    const signals = {
      price: logScale(marketPrice, PRICE_RANGE),
      params: logScale(params, PARAMS_RANGE),
      recency: releaseDate ? Math.min(1, Math.max(0, (Date.parse(releaseDate) - RECENCY_START) / (Date.now() - RECENCY_START))) : null,
      reasoning: reasoning === null ? null : reasoning ? 1 : 0,
    };
    let weightSum = 0;
    let total = 0;
    for (const [name, value] of Object.entries(signals)) {
      if (value === null || Number.isNaN(value)) continue;
      weightSum += HEURISTIC_WEIGHTS[name];
      total += HEURISTIC_WEIGHTS[name] * value;
    }
    // Missing signals count as the neutral prior, so a model known only to
    // be new and "reasoning" cannot outrank well-measured flagships.
    intelligence = total + HEURISTIC_PRIOR * (1 - weightSum);
    source = weightSum ? 'heuristic' : 'default';
    confidence = Math.min(0.7, weightSum * 0.7);
  }
  return {
    canonical_id: canonical,
    intelligence: Number(intelligence.toFixed(4)),
    intelligence_source: source,
    intelligence_confidence: Number(confidence.toFixed(3)),
    speed_tps: aa?.outputTps || null,
    context_window: firstTruthy(records, 'contextWindow'),
    max_output: firstTruthy(records, 'maxOutput'),
    price_in: median(records.map((record) => record.priceIn).filter((price) => Number.isFinite(price) && price > 0)),
    price_out: marketPrice,
    supports_tools: toFlag(firstTruthy(records, 'tools')),
    supports_vision: toFlag(firstTruthy(records, 'vision')),
    supports_reasoning: toFlag(reasoning),
    release_date: releaseDate || null,
    params_b: params,
    sources_json: JSON.stringify([...new Set([...records.map((record) => record.source), ...(aa ? ['artificial_analysis'] : [])])]),
  };
}

function toFlag(value) {
  return value === null || value === undefined ? null : value ? 1 : 0;
}

function upsertCanonical(record) {
  getDatabase().prepare(`INSERT INTO model_metadata
    (canonical_id, intelligence, intelligence_source, intelligence_confidence, speed_tps, context_window, max_output,
     price_in, price_out, supports_tools, supports_vision, supports_reasoning, release_date, params_b, sources_json, updated_at)
    VALUES (@canonical_id, @intelligence, @intelligence_source, @intelligence_confidence, @speed_tps, @context_window, @max_output,
     @price_in, @price_out, @supports_tools, @supports_vision, @supports_reasoning, @release_date, @params_b, @sources_json,
     strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    ON CONFLICT(canonical_id) DO UPDATE SET intelligence = excluded.intelligence, intelligence_source = excluded.intelligence_source,
      intelligence_confidence = excluded.intelligence_confidence, speed_tps = excluded.speed_tps, context_window = excluded.context_window,
      max_output = excluded.max_output, price_in = excluded.price_in, price_out = excluded.price_out,
      supports_tools = excluded.supports_tools, supports_vision = excluded.supports_vision, supports_reasoning = excluded.supports_reasoning,
      release_date = excluded.release_date, params_b = excluded.params_b, sources_json = excluded.sources_json, updated_at = excluded.updated_at`)
    .run(record);
}

function recomputeCanonical(canonical) {
  const samples = getDatabase().prepare('SELECT model_id FROM models WHERE canonical_id = ? LIMIT 5').all(canonical).map((row) => row.model_id);
  upsertCanonical(computeCanonical(canonical, samples));
}

function recomputeAll() {
  const canonicals = getDatabase().prepare('SELECT DISTINCT canonical_id FROM models').all().map((row) => row.canonical_id);
  getDatabase().transaction(() => {
    for (const canonical of canonicals) recomputeCanonical(canonical);
  })();
}

// Facts for exactly this provider's model (used for prices, never borrowed
// from another host because budgets must reflect what this host charges).
function providerFacts(definitionId, modelId) {
  loadState();
  const models = state.modelsDev?.[modelsDev.providerKey(definitionId)];
  if (!models) return null;
  return models[modelId] || models[String(modelId).replace(/^models\//, '')] || null;
}

function status() {
  loadState();
  return {
    fetchedAt: state.fetchedAt,
    artificialAnalysisConfigured: Boolean(getArtificialAnalysisKey()),
    sourcesEnabled: getConfig().metadataSourcesEnabled,
  };
}

function resetForTests(seed) {
  state = { modelsDev: {}, openrouter: [], artificialAnalysis: [], fetchedAt: {}, ...seed };
  buildIndexes();
}

module.exports = {
  refreshSources,
  recomputeCanonical,
  recomputeAll,
  computeCanonical,
  providerFacts,
  status,
  setArtificialAnalysisKey,
  resetForTests,
};
