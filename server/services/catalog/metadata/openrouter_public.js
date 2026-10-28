'use strict';

// OpenRouter's public model list is a broad market price reference: the same
// open model priced across hosts is a useful capability signal.
const SOURCE_URL = 'https://openrouter.ai/api/v1/models';

async function fetchSource({ signal } = {}) {
  const response = await fetch(SOURCE_URL, { signal: signal || AbortSignal.timeout(60_000) });
  if (!response.ok) throw new Error(`OpenRouter returned HTTP ${response.status}`);
  const data = await response.json();
  return (data?.data || []).map((model) => ({
    id: model.id,
    priceIn: Number(model.pricing?.prompt) * 1e6,
    priceOut: Number(model.pricing?.completion) * 1e6,
    contextWindow: model.context_length || null,
    created: model.created ? new Date(model.created * 1000).toISOString().slice(0, 10) : null,
    reasoning: (model.supported_parameters || []).includes('reasoning'),
    tools: (model.supported_parameters || []).includes('tools'),
    vision: (model.architecture?.input_modalities || []).includes('image'),
  }));
}

module.exports = { name: 'openrouter', fetchSource };
