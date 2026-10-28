'use strict';

// Maps provider-specific model ids onto one canonical id so metadata and
// learned behaviour can be shared, e.g. Groq "llama-3.3-70b-versatile" and
// OpenRouter "meta-llama/llama-3.3-70b-instruct:free" both become "llama3370b".

const PREFIXES = [
  /^@cf\//, /^@hf\//, /^models\//, /^accounts\/[^/]+\/models\//, /^openrouter\//,
];
const SUFFIX_TOKENS = new Set([
  'instruct', 'chat', 'it', 'versatile', 'instant', 'fp8', 'fp16', 'bf16', 'awq', 'gptq', 'int4', 'int8', 'hf',
  'free', 'latest', 'online', 'beta', 'api', 'tput', 'serverless', 'fast', 'preview', 'exp',
]);
const VENDOR_PREFIXES = new Set(['meta', 'mistralai', 'google', 'qwen', 'deepseek-ai', 'microsoft', 'nvidia', 'openai', 'anthropic']);

function canonicalId(modelId) {
  let id = String(modelId || '').trim().toLowerCase();
  for (const pattern of PREFIXES) id = id.replace(pattern, '');
  id = id.replace(/:(free|beta|extended|thinking|nitro|floor|exacto)$/, '');
  // Keep only the last path segment: vendor namespaces differ per host.
  id = id.split('/').pop();
  // Ollama-style tags: "llama3.3:70b" -> "llama3.3-70b".
  id = id.replace(/:/g, '-');
  const tokens = id.split(/[-_\s]+/).filter(Boolean);
  while (tokens.length > 1 && VENDOR_PREFIXES.has(tokens[0])) tokens.shift();
  while (tokens.length > 1) {
    const last = tokens[tokens.length - 1];
    // Date suffixes like 2024-08-06 split into three numeric tokens.
    if (tokens.length > 3 && /^\d{4}$/.test(tokens[tokens.length - 3]) && /^\d{2}$/.test(tokens[tokens.length - 2]) && /^\d{2}$/.test(last)) {
      tokens.splice(-3, 3);
      continue;
    }
    // Two-part dates such as "preview-05-20".
    if (tokens.length > 2 && /^\d{2}$/.test(tokens[tokens.length - 2]) && /^\d{2}$/.test(last)) {
      tokens.splice(-2, 2);
      continue;
    }
    // Compact dates (20241022, 2507, 0613) and context markers (32768).
    if (SUFFIX_TOKENS.has(last) || /^\d{4,8}$/.test(last)) {
      tokens.pop();
      continue;
    }
    break;
  }
  return tokens.join('').replace(/[^a-z0-9]/g, '') || String(modelId || '').toLowerCase();
}

// Parameter count in billions parsed from the id ("70b", "8x7b", "a3b" active).
function parameterCount(modelId) {
  const id = String(modelId || '').toLowerCase();
  const moe = id.match(/(\d+)x(\d+(?:\.\d+)?)b\b/);
  if (moe) return Number(moe[1]) * Number(moe[2]);
  const matches = [...id.matchAll(/(?<![a-z])(\d+(?:\.\d+)?)b(?![a-z])/g)].map((match) => Number(match[1]));
  if (!matches.length) return null;
  return Math.max(...matches);
}

module.exports = { canonicalId, parameterCount };
