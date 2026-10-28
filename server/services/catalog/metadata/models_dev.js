'use strict';

// models.dev publishes per-provider model facts (price, limits, capabilities,
// release date) as one JSON document.
const SOURCE_URL = 'https://models.dev/api.json';

// NeoRouter definition id -> models.dev provider id, where they differ.
const PROVIDER_IDS = {
  together: 'togetherai',
  fireworks: 'fireworks-ai',
  cloudflare: 'cloudflare-workers-ai',
  moonshot: 'moonshotai',
  dashscope: 'alibaba',
  zhipu: 'zhipuai',
  novita: 'novita-ai',
  ollama_cloud: 'ollama-cloud',
};

function num(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function toRecord(model) {
  const inputs = model.modalities?.input || [];
  const outputs = model.modalities?.output || [];
  return {
    name: model.name || null,
    priceIn: num(model.cost?.input),
    priceOut: num(model.cost?.output),
    contextWindow: num(model.limit?.context),
    maxOutput: num(model.limit?.output),
    tools: model.tool_call === undefined ? null : Boolean(model.tool_call),
    vision: inputs.length ? inputs.includes('image') : model.attachment === undefined ? null : Boolean(model.attachment),
    reasoning: model.reasoning === undefined ? null : Boolean(model.reasoning),
    releaseDate: model.release_date || null,
    textOutput: outputs.length ? outputs.includes('text') : true,
  };
}

async function fetchSource({ signal } = {}) {
  const response = await fetch(SOURCE_URL, { signal: signal || AbortSignal.timeout(60_000) });
  if (!response.ok) throw new Error(`models.dev returned HTTP ${response.status}`);
  const data = await response.json();
  const byProvider = {};
  for (const [providerId, provider] of Object.entries(data || {})) {
    const models = {};
    for (const [modelId, model] of Object.entries(provider?.models || {})) models[modelId] = toRecord({ ...model, id: modelId });
    byProvider[providerId] = models;
  }
  return byProvider;
}

function providerKey(definitionId) {
  return PROVIDER_IDS[definitionId] || definitionId;
}

module.exports = { name: 'models.dev', fetchSource, providerKey };
