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
const artificialAnalysisMedia = require('./metadata/artificial_analysis_media');
const mteb = require('./metadata/mteb');

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
    state = { modelsDev: {}, openrouter: [], artificialAnalysis: [], artificialAnalysisMedia: {}, mteb: {}, fetchedAt: {} };
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

// Models without the combined index: each benchmark score is translated to
// the index by a monotone mapping learned on the models that have both
// (isotonic fit, interpolated; below the known range it scales down in
// proportion instead of extrapolating a line into nonsense). A model's
// estimate is the average over its benchmarks, weighted by how well each
// benchmark's ranking agrees with the index (Spearman ρ²). At least
// MIN_DERIVED_BENCHMARKS are required.
const MIN_DERIVED_BENCHMARKS = 2;
const MIN_RANK_AGREEMENT = 0.3;
const MIN_FIT_PAIRS = 8;

function ranks(values) {
  const order = values.map((value, index) => [value, index]).sort((a, b) => a[0] - b[0]);
  const result = new Array(values.length);
  for (let i = 0; i < order.length;) {
    let j = i;
    while (j + 1 < order.length && order[j + 1][0] === order[i][0]) j += 1;
    for (let k = i; k <= j; k += 1) result[order[k][1]] = (i + j) / 2;
    i = j + 1;
  }
  return result;
}

function spearman(pairs) {
  const rx = ranks(pairs.map(([x]) => x));
  const ry = ranks(pairs.map(([, y]) => y));
  const n = pairs.length;
  const mean = (n - 1) / 2;
  let sxy = 0;
  let sxx = 0;
  let syy = 0;
  for (let i = 0; i < n; i += 1) {
    sxy += (rx[i] - mean) * (ry[i] - mean);
    sxx += (rx[i] - mean) ** 2;
    syy += (ry[i] - mean) ** 2;
  }
  return sxx && syy ? sxy / Math.sqrt(sxx * syy) : 0;
}

// Pool-adjacent-violators: the best non-decreasing fit of y over sorted x.
function isotonic(pairs) {
  const sorted = [...pairs].sort((a, b) => a[0] - b[0]);
  const blocks = [];
  for (const [x, y] of sorted) {
    blocks.push({ x: [x], sum: y, count: 1 });
    while (blocks.length > 1 && blocks[blocks.length - 2].sum / blocks[blocks.length - 2].count > blocks[blocks.length - 1].sum / blocks[blocks.length - 1].count) {
      const last = blocks.pop();
      const previous = blocks[blocks.length - 1];
      previous.x.push(...last.x);
      previous.sum += last.sum;
      previous.count += last.count;
    }
  }
  return blocks.map((block) => [block.x.reduce((a, b) => a + b, 0) / block.x.length, block.sum / block.count]);
}

function mapScore(curve, x) {
  const [x0, y0] = curve[0];
  const [xn, yn] = curve[curve.length - 1];
  if (x <= x0) return x0 > 0 ? y0 * Math.max(0, x / x0) : y0;
  if (x >= xn) return yn;
  for (let i = 1; i < curve.length; i += 1) {
    const [xa, ya] = curve[i - 1];
    const [xb, yb] = curve[i];
    if (x <= xb) return xb === xa ? yb : ya + ((x - xa) / (xb - xa)) * (yb - ya);
  }
  return yn;
}

function deriveIndexes(records) {
  const indexed = records.filter((record) => record.intelligenceIndex && !record.indexDerived && record.benchmarks);
  const fits = {};
  for (const name of artificialAnalysis.BENCHMARKS) {
    const pairs = indexed.filter((record) => Number.isFinite(record.benchmarks[name])).map((record) => [record.benchmarks[name], record.intelligenceIndex]);
    if (pairs.length < MIN_FIT_PAIRS) continue;
    const rho = spearman(pairs);
    if (rho * rho >= MIN_RANK_AGREEMENT && rho > 0) fits[name] = { curve: isotonic(pairs), weight: rho * rho };
  }
  for (const record of records) {
    if (record.intelligenceIndex || !record.benchmarks) continue;
    let weight = 0;
    let total = 0;
    let used = 0;
    for (const [name, fit] of Object.entries(fits)) {
      const value = record.benchmarks[name];
      if (!Number.isFinite(value)) continue;
      total += fit.weight * mapScore(fit.curve, value);
      weight += fit.weight;
      used += 1;
    }
    if (used >= MIN_DERIVED_BENCHMARKS && weight > 0) {
      record.intelligenceIndex = Math.max(1, total / weight);
      record.indexDerived = true;
    }
  }
}

function buildIndexes() {
  const byCanonical = new Map();
  for (const [providerId, models] of Object.entries(state.modelsDev || {})) {
    for (const [modelId, record] of Object.entries(models)) {
      addToIndex(byCanonical, canonicalId(modelId), { source: 'models.dev', providerId, modelId, ...record });
    }
  }
  for (const record of state.openrouter || []) addToIndex(byCanonical, canonicalId(record.id), { source: 'openrouter', ...record });
  const aaMax = Math.max(0, ...(state.artificialAnalysis || []).filter((record) => !record.indexDerived).map((record) => record.intelligenceIndex || 0));
  deriveIndexes(state.artificialAnalysis || []);
  const aa = new Map();
  for (const record of state.artificialAnalysis || []) {
    for (const key of [canonicalId(record.slug), canonicalId(record.name)]) if (key && !aa.has(key)) aa.set(key, record);
  }
  indexes = { byCanonical, aa, aaScale: aaMax > 0 ? aaMax : AA_INDEX_SCALE, calibration: null, benchmark: buildBenchmarkKeys(state.artificialAnalysis || []), media: artificialAnalysisMedia.buildIndexes(state.artificialAnalysisMedia) };
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
    .replace(/non-reasoning/g, 'nonreasoning')
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
  const ordered = [...records].sort((a, b) => Number(/-(xhigh|high|medium|low|minimal|non-reasoning)$/.test(a.slug || '')) - Number(/-(xhigh|high|medium|low|minimal|non-reasoning)$/.test(b.slug || '')));
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
        else if (existing && existing.intelligenceIndex !== record.intelligenceIndex && existing.slug.replace(/-(xhigh|high|medium|low|minimal|non-reasoning)$/, '') !== record.slug.replace(/-(xhigh|high|medium|low|minimal|non-reasoning)$/, '')) loose.set(withoutParams, null);
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

const SEARCH_QUALITY = {
  'exa-search': 0.85, 'parallel-search': 0.85, 'tavily-search': 0.8, 'linkup-standard': 0.8, 'serper-google': 0.75,
  'brave-web-search': 0.75, 'firecrawl-search': 0.7, 'jina-search': 0.65, searxng: 0.6,
};

// Parameter ranges (billions) from small to large within each kind.
const KIND_PARAMS = { embedding: [0.1, 10], moderation: [0.02, 20], transcription: [0.05, 5], speech: [0.05, 5], image: [1, 30], video: [1, 30] };

function kindHeuristic(capability, params, releaseDate) {
  const signals = [
    [0.6, logScale(params, KIND_PARAMS[capability] || PARAMS_RANGE)],
    [0.4, releaseDate ? Math.min(1, Math.max(0, (Date.parse(releaseDate) - RECENCY_START) / (Date.now() - RECENCY_START))) : null],
  ];
  let weightSum = 0;
  let total = 0;
  for (const [weight, value] of signals) {
    if (value === null || Number.isNaN(value)) continue;
    weightSum += weight;
    total += weight * value;
  }
  return { value: Math.min(0.85, Math.max(0.1, total + 0.5 * (1 - weightSum))), weightSum };
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
  if (aaKey) {
    tasks.push(['artificialAnalysis', () => artificialAnalysis.fetchSource({ apiKey: aaKey })]);
    tasks.push(['artificialAnalysisMedia', () => artificialAnalysisMedia.fetchSource({ apiKey: aaKey })]);
  }
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
  // MTEB is slow and sometimes flaky, so it runs after the other sources are
  // saved and applied: embedding models are rated once it is done.
  try {
    const ids = getDatabase().prepare("SELECT DISTINCT model_id FROM models WHERE capability = 'embedding' AND available = 1").all().map((row) => row.model_id);
    state.mteb = await mteb.fetchScores(ids, state.mteb || {});
    state.fetchedAt.mteb = new Date().toISOString();
    saveState();
    recomputeAll();
    results.mteb = 'ok';
  } catch (error) {
    results.mteb = error.message;
    logger.warn('Metadata source failed', { source: 'mteb', error: error.message });
  }
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
function computeCanonical(canonical, sampleModelIds = [], capability = 'chat') {
  loadState();
  const records = indexes.byCanonical.get(canonical) || [];
  // Media models are scored by their arena ELO, never by the chat index.
  const arena = capability !== 'chat' ? artificialAnalysisMedia.lookup(indexes.media, capability, sampleModelIds) : null;
  const aa = capability === 'chat' ? lookupBenchmark(canonical, sampleModelIds) : null;
  const paidPrices = records.map((record) => record.priceOut).filter((price) => Number.isFinite(price) && price > 0);
  const marketPrice = median(paidPrices);
  const params = parameterCount(sampleModelIds[0] || '') ?? sampleModelIds.map(parameterCount).find(Boolean) ?? null;
  const releaseDate = aa?.releaseDate || firstTruthy(records, 'releaseDate') || firstTruthy(records, 'created');
  const reasoning = firstTruthy(records, 'reasoning');

  let intelligence;
  let source;
  let confidence;
  if (arena) {
    intelligence = arena.score;
    source = 'artificial_analysis_arena';
    confidence = 0.85;
  } else if (capability === 'embedding' && sampleModelIds.some((id) => state.mteb?.[id]?.score)) {
    // MTEB, the embedding benchmark (average over a fixed set of English tasks).
    const scored = sampleModelIds.map((id) => state.mteb?.[id]).find((item) => item?.score);
    intelligence = mteb.intelligence(scored.score);
    source = 'mteb';
    confidence = 0.8;
  } else if (capability === 'search' && SEARCH_QUALITY[sampleModelIds[0]]) {
    // No public index compares search APIs; a curated ranking from agent
    // search evaluations (accuracy of answers built on the results).
    intelligence = SEARCH_QUALITY[sampleModelIds[0]];
    source = 'curated';
    confidence = 0.5;
  } else if (capability !== 'chat') {
    // Without arena data, size and age within the kind are the best guide;
    // the chat calibration means nothing for embedders or guards.
    const guess = kindHeuristic(capability, params, releaseDate);
    intelligence = guess.value;
    source = guess.weightSum ? 'heuristic' : 'default';
    confidence = Math.min(0.5, guess.weightSum * 0.5);
  } else if (aa?.intelligenceIndex) {
    intelligence = Math.min(1, aa.intelligenceIndex / (indexes.aaScale || AA_INDEX_SCALE));
    source = aa.indexDerived ? 'artificial_analysis_benchmarks' : 'artificial_analysis';
    confidence = aa.indexDerived ? 0.8 : 0.9;
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
    release_date: releaseDate || arena?.releaseDate || null,
    params_b: params,
    sources_json: JSON.stringify([...new Set([...records.map((record) => record.source), ...(aa ? ['artificial_analysis'] : []), ...(arena ? ['artificial_analysis_arena'] : [])])]),
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
  const rows = getDatabase().prepare('SELECT model_id, capability FROM models WHERE canonical_id = ? LIMIT 5').all(canonical);
  upsertCanonical(computeCanonical(canonical, rows.map((row) => row.model_id), rows[0]?.capability || 'chat'));
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
  state = { modelsDev: {}, openrouter: [], artificialAnalysis: [], artificialAnalysisMedia: {}, mteb: {}, fetchedAt: {}, ...seed };
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
