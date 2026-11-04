'use strict';

// Optional: Artificial Analysis publishes an intelligence index and measured
// output speed per model. Requires an API key configured by an admin. A Free key
// gets the /free lists: headline indexes and speed, no per-benchmark scores. Those
// come with Pro; scores already known are kept (metadata_service turns them into
// an estimate for models without an index, and into skills).
const API_URL = 'https://artificialanalysis.ai/api/v2';
const BENCHMARKS = ['gpqa', 'hle', 'mmlu_pro', 'livecodebench', 'scicode', 'aime_25', 'aime', 'math_500', 'ifbench', 'lcr', 'terminalbench_hard', 'tau2'];
// What the current API (Pro) calls the benchmarks the skills are built on.
const BENCHMARK_ALIASES = { gpqa_diamond: 'gpqa', aa_lcr: 'lcr', terminalbench_v4_0: 'terminalbench_hard' };
// Lists are paged. A guard, not an expectation: the quota is what really limits us.
const MAX_PAGES = 20;
// A key can be upgraded: asked again for the full list after this long.
const TIER_MEMORY_MS = 24 * 60 * 60 * 1000;

let freeTierUntil = 0;

async function getPage(path, page, { apiKey, signal }) {
  const query = page > 1 ? `?page=${page}` : '';
  return fetch(`${API_URL}/${path}${query}`, { headers: { 'x-api-key': apiKey }, signal: signal || AbortSignal.timeout(60_000) });
}

/**
 * Every item of a model list, from the full list or, on a Free key, its /free
 * sibling. One failed page fails the whole list: half a list must not replace
 * a complete one.
 */
async function fetchList(path, { apiKey, signal, now = Date.now() }) {
  let listPath = now < freeTierUntil ? `${path}/free` : path;
  let response = await getPage(listPath, 1, { apiKey, signal });
  if (response.status === 403 && listPath === path) {
    freeTierUntil = now + TIER_MEMORY_MS;
    listPath = `${path}/free`;
    response = await getPage(listPath, 1, { apiKey, signal });
  }
  const items = [];
  for (let page = 1; page <= MAX_PAGES; page += 1) {
    if (page > 1) response = await getPage(listPath, page, { apiKey, signal });
    if (!response.ok) throw new Error(`Artificial Analysis (${listPath}, page ${page}) returned HTTP ${response.status}`);
    const data = await response.json();
    items.push(...(data?.data || []));
    if (!data?.pagination?.has_more) break;
  }
  return items;
}

function toRecord(model) {
  const evaluations = model.evaluations || {};
  const performance = model.performance || {};
  const scores = {};
  for (const [name, value] of Object.entries(evaluations)) {
    const benchmark = BENCHMARK_ALIASES[name] || name;
    if (BENCHMARKS.includes(benchmark) && value !== null && value !== undefined && Number.isFinite(Number(value))) scores[benchmark] = Number(value);
  }
  return {
    id: model.id || null,
    slug: model.slug || model.name,
    name: model.name,
    intelligenceIndex: Number(evaluations.artificial_analysis_intelligence_index) || null,
    benchmarks: scores,
    outputTps: Number(performance.median_output_tokens_per_second) || null,
    ttftSeconds: Number(performance.median_time_to_first_token_seconds) || null,
    releaseDate: model.release_date || null,
  };
}

// The new list on top of what was known. Per-benchmark scores a list no longer
// carries stay, and so do models it dropped, for those scores alone: an index is
// only ever taken from the current list, since its scale changes between versions.
function mergeKnown(records, previous = []) {
  const known = new Map();
  for (const record of previous) if (record?.slug) known.set(record.slug, record);
  const merged = records.map((record) => {
    const earlier = known.get(record.slug);
    known.delete(record.slug);
    return { ...record, benchmarks: { ...earlier?.benchmarks, ...record.benchmarks } };
  });
  for (const earlier of known.values()) {
    if (Object.keys(earlier.benchmarks || {}).length) {
      merged.push({ id: earlier.id || null, slug: earlier.slug, name: earlier.name, intelligenceIndex: null, benchmarks: earlier.benchmarks, outputTps: earlier.outputTps || null, ttftSeconds: earlier.ttftSeconds || null, releaseDate: earlier.releaseDate || null });
    }
  }
  return merged;
}

async function fetchSource({ apiKey, signal, previous = [] } = {}) {
  const items = await fetchList('language/models', { apiKey, signal });
  if (!items.length) throw new Error('Artificial Analysis returned no models');
  return mergeKnown(items.map(toRecord), previous);
}

function resetForTests() {
  freeTierUntil = 0;
}

module.exports = { name: 'artificial_analysis', fetchSource, fetchList, toRecord, mergeKnown, BENCHMARKS, resetForTests };
