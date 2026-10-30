'use strict';

// Optional second opinion for the privacy shield: OpenAI's privacy filter,
// running on this server (see privacy_model.js), lists names, addresses and
// account numbers that patterns cannot catch. It only reads texts it has not
// seen yet, so long conversations do not pay for their history again, and any
// failure simply falls back to the pattern detectors.
const crypto = require('node:crypto');
const privacyModel = require('./privacy_model');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('ai-detector');
const CACHE_LIMIT = 1000;
const MIN_TEXT = 12;
// Generous: the first request after idling loads the model.
const DEADLINE_MS = 30_000;

const cache = new Map();

function cacheKey(text) {
  return crypto.createHash('sha256').update(text).digest('hex');
}

function remember(key, value) {
  cache.set(key, value);
  if (cache.size > CACHE_LIMIT) cache.delete(cache.keys().next().value);
}

// Entities per message index; texts already seen come from the cache.
async function detectEntities(canonical, { timeoutMs = DEADLINE_MS } = {}) {
  const deadline = Date.now() + timeoutMs;
  const results = new Map();
  const jobs = [];
  canonical.messages.forEach((message, index) => {
    if (message.role === 'system' || message.role === 'assistant') return;
    const text = typeof message.content === 'string'
      ? message.content
      : (Array.isArray(message.content) ? message.content.filter((part) => part.type === 'text').map((part) => part.text || '').join('\n') : '');
    if (text.trim().length < MIN_TEXT) return;
    const key = cacheKey(text);
    if (cache.has(key)) {
      results.set(index, cache.get(key));
      return;
    }
    jobs.push(privacyModel.detect(text).then((items) => {
      if (items) remember(key, items);
      results.set(index, items || []);
    }).catch((error) => {
      logger.warn('AI detection failed', { error: error.message });
      results.set(index, []);
    }));
  });
  await Promise.race([Promise.all(jobs), new Promise((resolve) => setTimeout(resolve, Math.max(0, deadline - Date.now()) + 50))]);
  return (index) => results.get(index) || [];
}

function clearCacheForTests() {
  cache.clear();
}

module.exports = { detectEntities, clearCacheForTests };
