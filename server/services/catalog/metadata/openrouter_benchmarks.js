'use strict';

// OpenRouter runs benchmarks itself on its public endpoints and publishes the
// results daily: GPQA Diamond (hard reasoning) and tau2-bench airline (agent
// work with tools), kept to the newest 90 days. Source: OpenRouter
// (openrouter.ai), licensed under CC BY 4.0.
const BASE_URL = 'https://openrouter.ai/api/v1/datasets/exports/benchmark-leaderboard';
const BENCHMARKS = Object.freeze({ gpqa_diamond: 'gpqa', tau_bench_verified_airline: 'tau' });
// One row per model and host; a host with few questions says little.
const MIN_QUESTIONS = 30;
const DAY_MS = 86_400_000;

const median = (values) => {
  const sorted = [...values].sort((a, b) => a - b);
  const middle = sorted.length >> 1;
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
};

// One score per model and benchmark: its newest version, the middle host. A
// host serving a broken or heavily cut-down copy must not decide for the model,
// and the luckiest host must not either.
function toModels(rows) {
  const versions = new Map();
  for (const row of rows || []) {
    const benchmark = BENCHMARKS[row.benchmark];
    const slug = row.model_slug;
    if (!benchmark || !slug || !(row.accuracy > 0) || !(row.total_questions >= MIN_QUESTIONS)) continue;
    const key = `${slug}|${benchmark}`;
    if (!versions.has(key)) versions.set(key, new Map());
    const version = versions.get(key);
    if (!version.has(row.model_permaslug)) version.set(row.model_permaslug, { rows: [], lastRun: '' });
    const item = version.get(row.model_permaslug);
    item.rows.push(row);
    if (row.last_run_timestamp > item.lastRun) item.lastRun = row.last_run_timestamp;
  }
  const models = new Map();
  for (const [key, version] of versions) {
    const [slug, benchmark] = key.split('|');
    const newest = [...version.values()].sort((a, b) => b.lastRun.localeCompare(a.lastRun))[0];
    if (!models.has(slug)) models.set(slug, { slug, name: newest.rows[0].model_name || slug, benchmarks: {}, lastRun: '' });
    const model = models.get(slug);
    model.benchmarks[benchmark] = median(newest.rows.map((row) => row.accuracy));
    if (newest.lastRun > model.lastRun) model.lastRun = newest.lastRun;
  }
  return [...models.values()];
}

// The newest snapshot: each UTC day has its own file, which may not be out yet.
async function fetchSource({ signal, previous = null, now = Date.now() } = {}) {
  const today = new Date(now).toISOString().slice(0, 10);
  // Dated files never change, and the rest of the catalog refreshes hourly.
  if (previous?.snapshotDate === today) return previous;
  const yesterday = new Date(now - DAY_MS).toISOString().slice(0, 10);
  let lastStatus = null;
  for (const name of [today, yesterday, 'latest']) {
    const response = await fetch(`${BASE_URL}/${name}.json`, { signal: signal || AbortSignal.timeout(60_000) });
    if (response.ok) {
      const data = await response.json();
      const models = toModels(data?.rows);
      if (!models.length) throw new Error('OpenRouter benchmarks had no usable rows');
      return { snapshotDate: data.snapshot_date || name, models };
    }
    lastStatus = response.status;
  }
  throw new Error(`OpenRouter benchmarks returned HTTP ${lastStatus}`);
}

module.exports = { name: 'openrouter_benchmarks', fetchSource, toModels };
