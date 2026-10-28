'use strict';

// Optional: Artificial Analysis publishes an intelligence index and measured
// output speed per model. Requires a (free) API key configured by an admin.
const SOURCE_URL = 'https://artificialanalysis.ai/api/v2/data/llms/models';

async function fetchSource({ apiKey, signal } = {}) {
  const response = await fetch(SOURCE_URL, {
    headers: { 'x-api-key': apiKey },
    signal: signal || AbortSignal.timeout(60_000),
  });
  if (!response.ok) throw new Error(`Artificial Analysis returned HTTP ${response.status}`);
  const data = await response.json();
  return (data?.data || []).map((model) => ({
    slug: model.slug || model.name,
    name: model.name,
    intelligenceIndex: Number(model.evaluations?.artificial_analysis_intelligence_index) || null,
    outputTps: Number(model.median_output_tokens_per_second) || null,
    ttftSeconds: Number(model.median_time_to_first_token_seconds) || null,
    releaseDate: model.release_date || null,
  }));
}

module.exports = { name: 'artificial_analysis', fetchSource };
