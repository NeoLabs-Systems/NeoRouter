'use strict';

// MTEB, the standard embedding benchmark, from its public results dataset
// (Hugging Face datasets-server, mteb/results). Models are tested on very
// different task sets, so only a fixed set of common English retrieval and
// similarity tasks is averaged, and a score needs MIN_TASKS of them. The
// service is occasionally flaky: failures keep the previous score.
const API = 'https://datasets-server.huggingface.co/filter';
const TASKS = ['ArguAna', 'SciFact', 'NFCorpus', 'FiQA2018', 'SCIDOCS', 'TRECCOVID', 'STSBenchmark', 'SICK-R', 'STS12', 'STS13', 'STS14', 'STS15', 'STS16', 'BIOSSES', 'SprintDuplicateQuestions', 'TwitterSemEval2015'];
const MIN_TASKS = 6;
const MAX_AGE_MS = 7 * 86_400_000;
// Provider ids → the names models are published under on Hugging Face.
const ORGS = { gemini: 'google', 'text-embedding': 'openai', 'mistral-embed': 'mistralai', 'embed-': 'Cohere', 'arctic-embed': 'Snowflake', 'bge-': 'BAAI', 'nomic-embed': 'nomic-ai', 'e5-': 'intfloat', 'gte-': 'Alibaba-NLP' };

function names(modelId) {
  const parts = String(modelId).replace(/^@cf\//, '').replace(/:free$/, '').split('/');
  const base = parts.pop();
  const list = [];
  if (parts.length) list.push(`${parts[parts.length - 1]}/${base}`);
  for (const [prefix, org] of Object.entries(ORGS)) if (base.startsWith(prefix) || base.includes(prefix)) list.push(`${org}/${base}`);
  if (/^arctic-embed/.test(base)) list.push(`Snowflake/snowflake-${base}`);
  return [...new Set(list)];
}

async function query(modelName, signal) {
  // The filter syntax has no IN; ORs work. The index sometimes needs a moment to load.
  const where = `"model_name"='${modelName.replace(/'/g, "''")}' AND "split"='test' AND (${TASKS.map((task) => `"task_name"='${task}'`).join(' OR ')})`;
  const url = `${API}?dataset=mteb%2Fresults&config=default&split=train&length=100&where=${encodeURIComponent(where)}`;
  for (let attempt = 0; attempt < 4; attempt += 1) {
    const response = await fetch(url, { signal: signal || AbortSignal.timeout(60_000) }).catch(() => null);
    const data = response ? await response.json().catch(() => null) : null;
    if (Array.isArray(data?.rows)) return data.rows.map((item) => item.row);
    await new Promise((resolve) => setTimeout(resolve, 10_000));
  }
  return null;
}

// Mean over the fixed tasks (English subsets only), or null.
function scoreOf(rows) {
  const byTask = new Map();
  for (const row of rows) {
    if (!Number.isFinite(row.score) || (row.subset && !['default', 'en', 'en-en', 'eng-Latn'].includes(row.subset))) continue;
    const list = byTask.get(row.task_name) || [];
    list.push(row.score);
    byTask.set(row.task_name, list);
  }
  if (byTask.size < MIN_TASKS) return null;
  const means = [...byTask.values()].map((list) => list.reduce((a, b) => a + b, 0) / list.length);
  return { score: means.reduce((a, b) => a + b, 0) / means.length, tasks: byTask.size };
}

/** Refreshes scores for the given embedding model ids; returns the updated map. */
async function fetchScores(modelIds, previous = {}, now = Date.now()) {
  const result = { ...previous };
  for (const modelId of modelIds) {
    const known = result[modelId];
    if (known && now - known.at < MAX_AGE_MS) continue;
    let found = null;
    for (const name of names(modelId)) {
      const rows = await query(name).catch(() => null);
      if (!rows) continue;
      const score = scoreOf(rows);
      if (score) {
        found = { ...score, model: name, at: now };
        break;
      }
    }
    result[modelId] = found || { score: null, at: now };
  }
  return result;
}

// An MTEB average of ~0.45 is weak today, ~0.75 is state of the art.
function intelligence(score) {
  return Math.min(1, Math.max(0.1, (score - 0.45) / 0.3));
}

module.exports = { fetchScores, scoreOf, names, intelligence, TASKS };
