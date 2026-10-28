'use strict';

// Artificial Analysis also runs blind-vote arenas for media models (ELO per
// model). NeoRouter uses them as the quality signal for image, speech and
// video models, the way the intelligence index serves chat models.
const ARENAS = { image: 'text-to-image', speech: 'text-to-speech', video: 'text-to-video' };
const BASE_URL = 'https://artificialanalysis.ai/api/v2/data/media/';

async function fetchSource({ apiKey, signal } = {}) {
  const result = {};
  for (const [kind, arena] of Object.entries(ARENAS)) {
    const response = await fetch(`${BASE_URL}${arena}`, { headers: { 'x-api-key': apiKey }, signal: signal || AbortSignal.timeout(60_000) });
    if (!response.ok) throw new Error(`Artificial Analysis (${arena}) returned HTTP ${response.status}`);
    const data = await response.json();
    result[kind] = (data?.data || []).filter((item) => Number.isFinite(Number(item.elo))).map((item) => ({
      slug: item.slug, name: item.name, creator: item.model_creator?.name || null, elo: Number(item.elo), releaseDate: item.release_date || null,
    }));
  }
  return result;
}

// Arena slugs carry host prefixes and dates ("flux_flux-2-klein-4b",
// "fal_flux-2-dev-turbo", "gemini-3-8-flash-tts"); provider ids carry
// vendors and versions with dots ("black-forest-labs/flux.2-klein-4b").
const NOISE = new Set(['fal', 'replicate', 'together', 'black', 'forest', 'labs', 'bfl', 'google', 'openai', 'preview', 'latest', 'free', 'v1', 'api']);

function mediaKey(text) {
  const tokens = String(text || '').toLowerCase()
    .replace(/\[|\]|\(|\)/g, ' ')
    .split(/[^a-z0-9]+/)
    .filter((token) => token && !NOISE.has(token) && !/^\d{4,8}$/.test(token) && !/^(19|20)\d{2}$/.test(token));
  // "flux_flux-2" → "flux-2": drop a token that repeats its predecessor.
  return tokens.filter((token, index) => token !== tokens[index - 1]).join('-');
}

function buildIndex(records = []) {
  const map = new Map();
  for (const record of records) {
    for (const key of [mediaKey(record.slug), mediaKey(record.name)]) if (key && !map.has(key)) map.set(key, record);
  }
  return map;
}

// Arena ELO → 0..1 within that arena (the best model is 1, the weakest 0.2).
function lookup(indexByKind, kind, modelIds) {
  const index = indexByKind?.[kind];
  if (!index?.map?.size) return null;
  for (const id of modelIds) {
    for (const text of [id, String(id).split('/').pop()]) {
      const record = index.map.get(mediaKey(text));
      if (record) return { ...record, score: 0.2 + 0.8 * ((record.elo - index.min) / Math.max(1, index.max - index.min)) };
    }
  }
  return null;
}

function buildIndexes(source = {}) {
  const result = {};
  for (const [kind, records] of Object.entries(source || {})) {
    if (!Array.isArray(records) || !records.length) continue;
    result[kind] = { map: buildIndex(records), min: Math.min(...records.map((item) => item.elo)), max: Math.max(...records.map((item) => item.elo)) };
  }
  return result;
}

module.exports = { name: 'artificial_analysis_media', fetchSource, buildIndexes, lookup, mediaKey, ARENAS };
