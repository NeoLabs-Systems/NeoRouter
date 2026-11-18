'use strict';
const processing = require('../processing/processing_service');
const redaction = require('../processing/redaction');
const sensitivity = require('../processing/sensitivity');
const { invalidToolCalls } = require('../../capabilities/chat/canonical');
const POLICY = require('./policy');
const MODES = Object.fromEntries(redaction.CATEGORIES.map((id) => [id, 'hide']));
const SENSITIVE = new Set(['secret', 'card', 'id']);
const REQUEST_FIELDS = ['messages', 'tools', 'tool_choice', 'response_format', 'temperature', 'top_p', 'max_tokens', 'stop'];

// Keep relevant tool context, never attachments, cache IDs, reasoning traces,
// routing secrets, provider URLs or a placeholder restoration vault.
async function sanitize(userId, canonical, message, findings = []) {
  if (!canonical?.messages?.length || !message || invalidToolCalls(message.tool_calls)) return null;
  const request = Object.fromEntries(REQUEST_FIELDS.filter((key) => canonical[key] !== undefined).map((key) => [key, canonical[key]]));
  request.messages = request.messages.map((message) => Object.fromEntries(
    ['role', 'content', 'name', 'tool_calls', 'tool_call_id'].filter((key) => message[key] !== undefined).map((key) => [key, message[key]]),
  ));
  const response = { role: 'assistant', content: message.content ?? null, ...(message.tool_calls?.length ? { tool_calls: message.tool_calls } : {}) };
  const { messages, ...context } = request;
  const joined = { ...request, messages: [...messages, response, { role: 'user', content: JSON.stringify(context) }] };
  if (Buffer.byteLength(JSON.stringify(joined)) > POLICY.maxPayloadBytes) return null;
  if (request.messages.some((item) => Array.isArray(item.content) && item.content.some((part) => part.type !== 'text'))) return null;
  if (findings.some((item) => SENSITIVE.has(item.category)) || sensitivity.assess(joined).level === 'high') return null;
  // The ordinary shield may be off. The same processing infrastructure runs
  // here with every category mandatory, no allow-list bypass and no compression.
  const own = processing.getSettings(userId);
  const result = await processing.apply(userId, joined, { isolatedPrivacy: true, settings: {
    privacy: { ...own.privacy, enabled: true, modes: MODES, allowTerms: [],
      ai: { enabled: require('../processing/privacy_model').enabled() } },
    compression: { ...processing.DEFAULT_SETTINGS.compression, level: 'off', enabled: false },
  } });
  const vault = result.vault || new redaction.Vault(MODES);
  const walk = (node) => typeof node === 'string' ? vault.redactText(node, { customTerms: own.privacy.customTerms, allowTerms: [] })
    : Array.isArray(node) ? node.map(walk)
      : node && typeof node === 'object' ? Object.fromEntries(Object.entries(node).map(([key, value]) => [walk(key), walk(value)])) : node;
  const clean = result.canonical.messages.map((item) => walk(item));
  const extra = walk(JSON.parse(clean.pop().content));
  const output = clean.pop();
  if (vault.findings().some((item) => SENSITIVE.has(item.category)) ||
    [...clean, output].some((item) => invalidToolCalls(item.tool_calls))) return null;
  return { canonical: { ...extra, messages: clean }, response: output };
}
module.exports = { sanitize };
