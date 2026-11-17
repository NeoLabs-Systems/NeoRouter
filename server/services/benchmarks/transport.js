'use strict';
const { getDatabase } = require('../../db/database');
const accounts = require('../providers/account_service');
const catalog = require('../catalog/catalog_service');
const quota = require('../quota/quota_service');
const billing = require('../providers/billing_check');
const load = require('../routing/load_tracker');
const pressure = require('../routing/capacity');
const smoothing = require('../routing/smoothing');
const priority = require('../routing/priority');
const { RoutingPlan, rejection } = require('../routing/router');
const { isPeerEntry } = require('../pools/links');
const { requirements, estimateInputTokens, normalizeUsage, reportedCost, invalidToolCalls } = require('../../capabilities/chat/canonical');
const consent = require('./consent');
const POLICY = require('./policy');

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

// Uses the same eligibility, quota windows, pools, health and provider adapters,
// but deliberately never calls the executor, production learning or analytics.
async function call(row, ref, request, signal) {
  assertActive(row, signal);
  if (pressure.isShort(row.user_id)) throw new Deferred();
  const entry = catalog.getModel(row.user_id, ref);
  // Real traffic on this account goes first; other accounts are fair game.
  if (!entry || load.accountBusy(entry.accountId)) throw new Deferred();
  const ctx = context(row, request);
  if (!automaticallyFree(entry) || priority.hasWaiters(entry)) throw new Deferred();
  const reason = rejection(entry, ctx, Date.now());
  if (reason === 'paced') throw new Deferred('waiting_quota', smoothing.nextAt(entry, { userId: row.user_id, inputTokens: ctx.inputTokens, maxOutput: request.max_tokens, priority: 'low' }));
  if (reason?.startsWith('quota_')) throw new Deferred('waiting_quota', quota.evaluate(entry, ctx.inputTokens).blocked?.resetAt);
  if (reason) throw new Deferred();
  const input = estimateInputTokens(request);
  const output = Math.min(request.max_tokens || POLICY.maxOutputTokens, entry.maxOutput || POLICY.maxOutputTokens, POLICY.maxOutputTokens);
  const reserved = input + output;
  const capacity = quota.evaluate(entry, reserved);
  if (capacity.blocked) throw new Deferred('waiting_quota', capacity.blocked.resetAt);
  const runtime = accounts.getRuntime(entry.accountId);
  if (!runtime || runtime.prepaid) throw new Deferred('waiting_free_models');
  const controller = new AbortController();
  const stopListening = load.onForeground((accountId) => {
    if (accountId === entry.accountId) controller.abort(new Deferred('traffic_yield'));
  });
  const combined = AbortSignal.any([signal, controller.signal, AbortSignal.timeout(POLICY.timeoutMs)].filter(Boolean));
  let release;
  try {
    assertActive(row, combined);
    const adapter = accounts.adapterFor(runtime);
    release = load.lease(entry, { background: true });
    quota.recordRequest(entry, reserved);
    getDatabase().prepare('UPDATE benchmark_samples SET calls = calls + 1 WHERE id = ? AND user_id = ?').run(row.id, row.user_id);
    // Persist the reservation before a crash can cause a replay without quota accounting.
    quota.flush({ prune: false });
    const { response, headers } = await adapter.chat({ ...request, stream: false, max_tokens: output }, {
      modelId: entry.modelId, model: entry, signal: combined, timeoutMs: POLICY.timeoutMs, benchmark: true,
    });
    const usage = normalizeUsage(response.usage);
    if (usage) quota.recordTokens(entry, usage.prompt_tokens + usage.completion_tokens - reserved);
    quota.observeHeaders(entry, headers);
    const charged = reportedCost(response.usage);
    billing.observeCost(entry, charged);
    if (charged > 0) throw new Deferred('billed_model');
    assertActive(row, combined);
    const choice = response.choices?.[0];
    if (!choice || !['stop', 'tool_calls'].includes(choice.finish_reason) || invalidToolCalls(choice.message?.tool_calls)) throw new Deferred('incomplete_output');
    if (Buffer.byteLength(JSON.stringify(choice.message)) > POLICY.maxPayloadBytes) throw new Deferred('oversized_output');
    // Do not persist hidden reasoning or model/provider response metadata.
    return { role: 'assistant', content: choice.message.content ?? null, ...(choice.message.tool_calls?.length ? { tool_calls: choice.message.tool_calls } : {}) };
  } catch (error) {
    if (combined.aborted) throw combined.reason instanceof Deferred ? combined.reason : new Deferred('provider_timeout');
    if (error instanceof Deferred) throw error;
    if (error.headers) quota.observeHeaders(entry, error.headers);
    if (error.kind === 'rate_limit' || error.kind === 'quota_exhausted') {
      const resetAt = error.retryAfterMs ? Date.now() + error.retryAfterMs : Date.now() + POLICY.retryMs;
      quota.markExhausted(entry, error.learned?.metric || 'rpm', resetAt);
      throw new Deferred('waiting_quota', resetAt);
    }
    // Provider errors may echo request text. Only a closed error code survives.
    throw new Deferred('provider_failure');
  } finally { stopListening(); release?.(); quota.flush({ prune: false }); }
}
module.exports = { call, candidates, assertActive, automaticallyFree, Deferred };
