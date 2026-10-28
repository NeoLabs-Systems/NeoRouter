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
// Benchmark indexes are scaled so the best model in the data is 1.0; the
// fixed scale is only a fallback before any benchmark data is loaded.
const AA_INDEX_SCALE = 75;
const HEURISTIC_PRIOR = 0.4;
// Guesses are mapped onto the benchmark scale with a linear fit over models
// that have both; without enough pairs the raw guess is used.
const MIN_CALIBRATION_PAIRS = 12;
const HEURISTIC_RANGE = [0.05, 0.9];

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
  const aaMax = Math.max(0, ...(state.artificialAnalysis || []).map((record) => record.intelligenceIndex || 0));
  indexes = { byCanonical, aa, aaScale: aaMax > 0 ? aaMax : AA_INDEX_SCALE, calibration: null, benchmark: buildBenchmarkKeys(state.artificialAnalysis || []) };
  indexes.calibration = fitCalibration();
}

// Least-squares line from the raw guess to the benchmark score, over every
// model that has both.
// Benchmark names rarely equal provider ids ("nemotron-3-5-super-vl" vs
// "nvidia/nemotron-3.5-super-vl-120b-a12b"). Both sides are reduced to word
// tokens without dates, reasoning-effort levels and serving suffixes; one key
// keeps the parameter sizes, a looser one drops them.
const EFFORT_TOKENS = new Set(['xhigh', 'high', 'medium', 'low', 'minimal', 'default', 'fallback', 'nonreasoning']);
const SERVING_TOKENS = new Set(['instruct', 'it', 'chat', 'preview', 'latest', 'free', 'exp', 'versatile', 'instant', 'fp8', 'bf16']);
const PARAM_TOKEN = /^([ae]?\d+(\.\d+)?b|\d+x\d+b)$/;

function benchmarkKeys(text) {
  const tokens = String(text || '').toLowerCase()
    .replace(/\([^)]*\)/g, ' ')
    .replace(/:(free|beta|thinking|extended|nitro)$/, '')
    .split(/[\s./:_-]+/)
    .filter((token) => token && !EFFORT_TOKENS.has(token) && !SERVING_TOKENS.has(token) && !/^\d{4,8}$/.test(token));
  const withParams = tokens.join('-');
  const withoutParams = tokens.filter((token) => !PARAM_TOKEN.test(token)).join('-');
  return { withParams, withoutParams };
}

function buildBenchmarkKeys(records) {
  const exact = new Map();
  const loose = new Map();
  // Records without an effort suffix are the provider default; index them first.
  const ordered = [...records].sort((a, b) => Number(/-(xhigh|high|medium|low|minimal)$/.test(a.slug || '')) - Number(/-(xhigh|high|medium|low|minimal)$/.test(b.slug || '')));
  for (const record of ordered) {
    if (!record.intelligenceIndex) continue;
    const seen = new Set();
    for (const text of [record.slug, record.name]) {
      const { withParams, withoutParams } = benchmarkKeys(text);
      if (withParams && !exact.has(withParams)) exact.set(withParams, record);
      if (withoutParams && !seen.has(withoutParams)) {
        seen.add(withoutParams);
        // Two different models collapsing onto one size-less key make it ambiguous.
        const existing = loose.get(withoutParams);
        if (existing === undefined) loose.set(withoutParams, record);
        else if (existing && existing.intelligenceIndex !== record.intelligenceIndex && existing.slug.replace(/-(xhigh|high|medium|low|minimal)$/, '') !== record.slug.replace(/-(xhigh|high|medium|low|minimal)$/, '')) loose.set(withoutParams, null);
      }
    }
  }
  return { exact, loose };
}

// The benchmark record for a model, by canonical id or by any of its ids.
function lookupBenchmark(canonical, modelIds = []) {
  const direct = indexes.aa?.get(canonical);
  if (direct?.intelligenceIndex) return direct;
  if (!indexes.benchmark) return direct || null;
  for (const id of modelIds) {
    const candidates = [id, String(id).split('/').pop()];
    for (const text of candidates) {
      const { withParams } = benchmarkKeys(text);
      if (indexes.benchmark.exact.has(withParams)) return indexes.benchmark.exact.get(withParams);
    }
    for (const text of candidates) {
      const { withoutParams } = benchmarkKeys(text);
      const match = indexes.benchmark.loose.get(withoutParams);
      if (match) return match;
    }
  }
  return direct || null;
}

function fitCalibration() {
  const pairs = [];
  for (const [canonical, records] of indexes.byCanonical) {
    const ids = records.map((item) => item.modelId || item.id).filter(Boolean);
    const record = lookupBenchmark(canonical, ids);
    if (!record?.intelligenceIndex) continue;
    const guess = heuristicIntelligence(records, ids, record);
    if (guess.weightSum < 0.5) continue;
    pairs.push([guess.value, Math.min(1, record.intelligenceIndex / indexes.aaScale)]);
  }
  if (pairs.length < MIN_CALIBRATION_PAIRS) return null;
  const meanX = pairs.reduce((sum, [x]) => sum + x, 0) / pairs.length;
  const meanY = pairs.reduce((sum, [, y]) => sum + y, 0) / pairs.length;
  const covariance = pairs.reduce((sum, [x, y]) => sum + (x - meanX) * (y - meanY), 0);
  const variance = pairs.reduce((sum, [x]) => sum + (x - meanX) ** 2, 0);
  if (variance <= 0 || covariance <= 0) return null;
  const slope = covariance / variance;
  return { slope, intercept: meanY - slope * meanX, pairs: pairs.length };
}

// Price, size, recency and reasoning as a rough 0..1 guess of capability.
function heuristicIntelligence(records, sampleModelIds = [], aa = null) {
  const paidPrices = records.map((record) => record.priceOut).filter((price) => Number.isFinite(price) && price > 0);
  const marketPrice = median(paidPrices);
  const params = parameterCount(sampleModelIds[0] || '') ?? sampleModelIds.map(parameterCount).find(Boolean) ?? null;
  const releaseDate = aa?.releaseDate || firstTruthy(records, 'releaseDate') || firstTruthy(records, 'created');
  const reasoning = firstTruthy(records, 'reasoning');
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
  // Missing signals count as the neutral prior, so a model known only to be
  // new and "reasoning" cannot outrank well-measured flagships.
  return { value: total + HEURISTIC_PRIOR * (1 - weightSum), weightSum };
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
  const aa = lookupBenchmark(canonical, sampleModelIds);
  const paidPrices = records.map((record) => record.priceOut).filter((price) => Number.isFinite(price) && price > 0);
  const marketPrice = median(paidPrices);
  const params = parameterCount(sampleModelIds[0] || '') ?? sampleModelIds.map(parameterCount).find(Boolean) ?? null;
  const releaseDate = aa?.releaseDate || firstTruthy(records, 'releaseDate') || firstTruthy(records, 'created');
  const reasoning = firstTruthy(records, 'reasoning');

  let intelligence;
  let source;
  let confidence;
  if (aa?.intelligenceIndex) {
    intelligence = Math.min(1, aa.intelligenceIndex / (indexes.aaScale || AA_INDEX_SCALE));
    source = 'artificial_analysis';
    confidence = 0.9;
  } else {
    const guess = heuristicIntelligence(records, sampleModelIds, aa);
    const calibration = indexes.calibration;
    intelligence = calibration
      ? Math.min(HEURISTIC_RANGE[1], Math.max(HEURISTIC_RANGE[0], calibration.slope * guess.value + calibration.intercept))
      : guess.value;
    source = guess.weightSum ? (calibration ? 'calibrated' : 'heuristic') : 'default';
    confidence = Math.min(0.7, guess.weightSum * 0.7);
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
