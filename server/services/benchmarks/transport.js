'use strict';
const { getDatabase } = require('../../db/database');
const catalog = require('../catalog/catalog_service');
const priority = require('../routing/priority');
const { RoutingPlan, rejection } = require('../routing/router');
const { isPeerEntry } = require('../pools/links');
const { requirements, estimateInputTokens, invalidToolCalls } = require('../../capabilities/chat/canonical');
const consent = require('./consent');
const POLICY = require('./policy');
const logger = require('../../utils/logger').createLogger('local-benchmarks');

class Deferred extends Error {
  constructor(code = 'waiting_capacity', retryAt = null) { super(code); this.code = code; this.retryAt = retryAt; }
}
function keyFor(row) {
  const key = getDatabase().prepare('SELECT * FROM router_keys WHERE id = ? AND user_id = ?').get(row.key_id, row.user_id);
  if (!key) throw new Deferred('consent_revoked');
  return { ...key, mode: 'free_only', allow_free: 1, allow_plan: 0, pool_access: 'own', priority: 'low' };
}
function assertActive(row, signal) {
  signal?.throwIfAborted();
  const exists = getDatabase().prepare('SELECT 1 FROM benchmark_samples WHERE id = ? AND user_id = ? AND expires_at > ? AND payload IS NOT NULL').get(row.id, row.user_id, Date.now());
  if (!exists || !consent.allowed(row.user_id, row.key_id, JSON.parse(row.consent_json))) throw new Deferred('consent_revoked');
}
function automaticallyFree(entry) {
  if (!entry.isFree || entry.isPlan || entry.isFlatRate || isPeerEntry(entry)) return false;
  if (entry.freeReason === 'manual_free') return false;
  const row = getDatabase().prepare('SELECT prepaid FROM provider_accounts WHERE id = ?').get(entry.accountId);
  return row && !row.prepaid;
}
function context(row, canonical) {
  const plan = new RoutingPlan({ userId: row.user_id, key: keyFor(row), request: canonical,
    requestedModel: 'auto', needs: requirements(canonical), inputTokens: estimateInputTokens(canonical),
    freeOnly: true, keepPrivate: true });
  // Benchmark work always respects learned demand/reset pacing, even when the
  // user lets foreground requests run unpaced. No benchmark reserve percentage.
  return { ...plan.context(), smoothing: true };
}
function candidates(row, canonical) {
  const ctx = context(row, canonical);
  return catalog.listForUser(row.user_id).filter((entry) => automaticallyFree(entry) && !rejection(entry, ctx, Date.now()) && !priority.hasWaiters(entry));
}

// A response object that keeps what the executor sends (as web search does).
function capture() {
  const headers = {};
  return {
    headers, body: null, statusCode: 200,
    set(name, value) { if (typeof name === 'object') Object.assign(headers, name); else headers[String(name).toLowerCase()] = value; return this; },
    get(name) { return headers[String(name).toLowerCase()]; },
    status(code) { this.statusCode = code; return this; },
    json(body) { this.body = body; return this; },
    sse: '',
    on() { return this; }, write(chunk) { this.sse += String(chunk); return true; }, end() { return this; }, flushHeaders() {},
  };
}

// The streamed answer put back together, as an OpenAI client would.
function assemble(sse) {
  const message = { role: 'assistant', content: '' };
  const calls = [];
  let finishReason = null;
  for (const line of sse.split('\n')) {
    if (!line.startsWith('data: ') || line === 'data: [DONE]') continue;
    let chunk;
    try { chunk = JSON.parse(line.slice(6)); } catch { continue; }
    if (chunk.error) return null;
    const choice = chunk.choices?.[0];
    if (!choice) continue;
    if (typeof choice.delta?.content === 'string') message.content += choice.delta.content;
    for (const call of choice.delta?.tool_calls || []) {
      const slot = calls[call.index ?? calls.length] ||= { id: call.id, type: 'function', function: { name: '', arguments: '' } };
      if (call.id) slot.id = call.id;
      if (call.function?.name) slot.function.name += call.function.name;
      if (call.function?.arguments) slot.function.arguments += call.function.arguments;
    }
    if (choice.finish_reason) finishReason = choice.finish_reason;
  }
  if (calls.length) message.tool_calls = calls.filter(Boolean);
  if (!message.content && message.tool_calls) message.content = null;
  return { message, finish_reason: finishReason };
}

// One replay through the router's own chat flow: the chosen model, as an
// internal, low-priority, free-only request of the sample's key. Queueing,
// output room, timeouts, quotas and pacing are the router's.
async function call(row, ref, request, signal) {
  assertActive(row, signal);
  const entry = catalog.getModel(row.user_id, ref);
  if (!entry || !automaticallyFree(entry)) throw new Deferred('model_unavailable');
  const { execute } = require('../../capabilities/chat/executor');
  const res = capture();
  const req = { id: null, headers: {}, get: () => null, on() { return this; } };
  getDatabase().prepare('UPDATE benchmark_samples SET calls = calls + 1 WHERE id = ? AND user_id = ?').run(row.id, row.user_id);
  const began = Date.now();
  try {
    await execute({ req, res, key: keyFor(row), format: 'openai_chat', freeOnly: true, internal: true,
      canonical: { ...request, model: entry.id, stream: true, max_tokens: request.max_tokens || POLICY.maxOutputTokens } });
  } catch (error) {
    // The router's own error code and how long it took: never content.
    logger.info('Benchmark call failed', { model: entry.canonicalId, status: error.status || null, code: error.code || error.kind || null, ms: Date.now() - began,
      trail: (error.details?.trail || []).map((item) => ({ outcome: item.outcome, ms: item.ms ?? null })) });
    const retryAfter = error.details?.retryAfterSeconds;
    if (error.status === 429 || error.status === 503) throw new Deferred('waiting_quota', retryAfter ? Date.now() + retryAfter * 1000 : null);
    throw new Deferred('provider_failure');
  }
  assertActive(row, signal);
  // Streamed like real traffic, so the router's timeout covers the first
  // token, not a long answer from a slow model.
  const choice = res.body?.choices?.[0] || assemble(res.sse);
  if (!choice || !['stop', 'tool_calls'].includes(choice.finish_reason) || invalidToolCalls(choice.message?.tool_calls)) throw new Deferred('incomplete_output');
  if (Buffer.byteLength(JSON.stringify(choice.message)) > POLICY.maxPayloadBytes) throw new Deferred('oversized_output');
  // Do not persist hidden reasoning or model/provider response metadata.
  return { role: 'assistant', content: choice.message.content ?? null, ...(choice.message.tool_calls?.length ? { tool_calls: choice.message.tool_calls } : {}) };
}

module.exports = { call, candidates, assertActive, automaticallyFree, Deferred };
