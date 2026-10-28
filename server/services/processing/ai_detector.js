'use strict';

// Optional second opinion for the privacy shield: a fast model lists names,
// addresses, organizations and ID numbers that patterns cannot catch. It runs
// through NeoRouter's own routing (free models by default), with a short
// deadline, and only on texts it has not seen yet, so long conversations do
// not pay for their history again. Any failure simply falls back to the
// pattern detectors.
const crypto = require('node:crypto');
const accounts = require('../providers/account_service');
const { RoutingPlan } = require('../routing/router');
const learning = require('../routing/learning');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('ai-detector');
const CACHE_LIMIT = 1000;
const MAX_TEXT = 6000;
const MIN_TEXT = 12;
const DEADLINE_MS = 6000;
const MAX_ATTEMPTS = 3;
const CATEGORY_OF = { person: 'person', name: 'person', address: 'address', location: 'address', organization: 'organization', company: 'organization', id: 'id', id_number: 'id', account: 'id' };

const cache = new Map();

const PROMPT = 'You find private information in text. Return only JSON: {"items":[{"text":"<exact substring>","type":"person|address|organization|id"}]}. '
  + 'person = names of real people; address = street addresses, cities with streets, postal codes with a place; organization = names of companies, employers, schools or clinics tied to the writer; '
  + 'id = customer, contract, case, insurance, tax, passport or account numbers. Copy each text exactly as it appears. '
  + 'Ignore placeholders in square brackets, public figures, well-known brands, product and programming names. Return {"items":[]} if there is nothing.';

function cacheKey(text) {
  return crypto.createHash('sha256').update(text).digest('hex');
}

function remember(key, value) {
  cache.set(key, value);
  if (cache.size > CACHE_LIMIT) cache.delete(cache.keys().next().value);
}

function parseItems(content, text) {
  const json = String(content || '').match(/\{[\s\S]*\}/);
  if (!json) return [];
  let parsed;
  try { parsed = JSON.parse(json[0]); } catch { return []; }
  return (Array.isArray(parsed.items) ? parsed.items : [])
    .map((item) => ({ text: String(item?.text || '').trim(), category: CATEGORY_OF[String(item?.type || '').toLowerCase()] }))
    // Only exact substrings count: the model must not rewrite or invent values.
    .filter((item) => item.category && item.text.length >= 2 && item.text.length <= 200 && text.includes(item.text));
}

// One call to the fastest suitable model for this user.
async function ask(userId, settings, text, deadline) {
  const key = { id: null, user_id: userId, mode: settings.freeOnly === false ? 'both' : 'free_only', profile: 'fast', allowed_models_json: '[]', budget_usd: null };
  const request = {
    messages: [{ role: 'system', content: PROMPT }, { role: 'user', content: text.slice(0, MAX_TEXT) }],
    max_tokens: 600,
    temperature: 0,
  };
  const plan = new RoutingPlan({ userId, key, request, requestedModel: settings.model || 'auto:fast', needs: {}, inputTokens: Math.ceil(text.length / 4) + 200, freeOnly: settings.freeOnly !== false });
  for (let attempt = 0; attempt < MAX_ATTEMPTS && Date.now() < deadline; attempt += 1) {
    const choice = plan.next();
    if (!choice) break;
    const runtime = accounts.getRuntime(choice.entry.accountId);
    if (!runtime) continue;
    const started = Date.now();
    try {
      const result = await accounts.adapterFor(runtime).chat(request, { modelId: choice.entry.modelId, timeoutMs: Math.max(500, deadline - Date.now()), model: choice.entry });
      const latencyMs = Date.now() - started;
      learning.onSuccess(choice.entry, { latencyMs, ttftMs: null, outputTokens: 0, generationMs: latencyMs }, result.headers);
      return parseItems(result.response?.choices?.[0]?.message?.content, text);
    } catch (error) {
      learning.onFailure(choice.entry, error);
    }
  }
  return null;
}

// Entities per message index; texts already seen come from the cache.
async function detectEntities(userId, settings, canonical) {
  const deadline = Date.now() + (settings.timeoutMs || DEADLINE_MS);
  const results = new Map();
  const jobs = [];
  canonical.messages.forEach((message, index) => {
    if (message.role === 'system' || message.role === 'assistant') return;
    const text = typeof message.content === 'string'
      ? message.content
      : (Array.isArray(message.content) ? message.content.filter((part) => part.type === 'text').map((part) => part.text || '').join('\n') : '');
    if (text.trim().length < MIN_TEXT) return;
    const key = cacheKey(`${settings.model || ''}\u0000${text}`);
    if (cache.has(key)) {
      results.set(index, cache.get(key));
      return;
    }
    jobs.push(ask(userId, settings, text, deadline).then((items) => {
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

module.exports = { detectEntities, parseItems, clearCacheForTests };
