'use strict';
const { getDatabase } = require('../../db/database');
const accounts = require('../providers/account_service');
const catalog = require('../catalog/catalog_service');
const quota = require('../quota/quota_service');
const billing = require('../providers/billing_check');
const load = require('../routing/load_tracker');
const concurrency = require('../routing/concurrency');
const stats = require('../routing/stats_store');
const smoothing = require('../routing/smoothing');
const priority = require('../routing/priority');
const { RoutingPlan, rejection } = require('../routing/router');
const { isPeerEntry } = require('../pools/links');
const { requirements, estimateInputTokens, normalizeUsage, reportedCost, invalidToolCalls } = require('../../capabilities/chat/canonical');
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

// Output room for a replay. Reasoning models think before they answer, so the
// small budgets of judge prompts and of the original request are raised to
// what the model is known to need: the room live routing learned for it, or
// what an earlier replay ran out of (kept here, so replays never write to
// production learning).
const MAX_ROOM = 32000;
const NETWORK_PAUSE_MS = 60_000;
const needed = new Map();
function outputRoom(entry, request) {
  const asked = Math.min(request.max_tokens || POLICY.maxOutputTokens, POLICY.maxOutputTokens);
  const cap = Math.min(MAX_ROOM, entry.maxOutput || MAX_ROOM);
  return Math.min(cap, Math.max(asked, stats.outputFloor(entry.ref), needed.get(entry.ref) || 0));
}

// Time for one replay: the output room at the model's measured speed (or a
// slow 30 tokens/s when unmeasured), plus its usual time to first token and
// half again as margin. Never under the base timeout, never over 5 minutes.
const MAX_REPLAY_MS = 5 * 60_000;
function replayTimeout(entry, outputTokens) {
  const measured = stats.snapshot(entry.ref);
  const tps = measured.tps > 0 ? measured.tps : 30;
  const expected = (measured.ttft_ms || 0) + (outputTokens / tps) * 1000;
  return Math.round(Math.min(MAX_REPLAY_MS, Math.max(POLICY.timeoutMs, expected * 1.5)));
}

// Full when the in-flight requests (real and this one) reach the account's
// stated, documented or learned concurrency limit. Unknown limits never block:
// rate limits and cooldowns are still enforced by rejection() and quotas.
function accountFull(entry) {
  const known = concurrency.limitFor(entry);
  return !!known && load.inflight(entry).account >= known.limit;
}

// Uses the same eligibility, quota windows, pools, health and provider adapters,
// but deliberately never calls the executor, production learning or analytics.
async function call(row, ref, request, signal) {
  assertActive(row, signal);
  const entry = catalog.getModel(row.user_id, ref);
  if (!entry || !automaticallyFree(entry)) throw new Deferred('model_unavailable');
  // Real traffic goes first: wait only while the account has no room left
  // (its known concurrency limit is reached) or requests queue for it.
  if (accountFull(entry)) throw new Deferred('account_busy');
  if (priority.hasWaiters(entry)) throw new Deferred('requests_waiting');
  const ctx = context(row, request);
  const reason = rejection(entry, ctx, Date.now());
  if (reason === 'paced') throw new Deferred('waiting_quota', smoothing.nextAt(entry, { userId: row.user_id, inputTokens: ctx.inputTokens, maxOutput: request.max_tokens, priority: 'low' }));
  if (reason?.startsWith('quota_')) throw new Deferred('waiting_quota', quota.evaluate(entry, ctx.inputTokens).blocked?.resetAt);
  if (reason) throw new Deferred('model_unavailable');
  const input = estimateInputTokens(request);
  const output = outputRoom(entry, request);
  const reserved = input + output;
  const capacity = quota.evaluate(entry, reserved);
  if (capacity.blocked) throw new Deferred('waiting_quota', capacity.blocked.resetAt);
  const runtime = accounts.getRuntime(entry.accountId);
  if (!runtime || runtime.prepaid) throw new Deferred('waiting_free_models');
  const controller = new AbortController();
  // A real request that would find the account full takes the slot back.
  const stopListening = load.onForeground((accountId) => {
    if (accountId === entry.accountId && accountFull(entry)) controller.abort(new Deferred('traffic_yield'));
  });
  const timeoutMs = replayTimeout(entry, output);
  const combined = AbortSignal.any([signal, controller.signal, AbortSignal.timeout(timeoutMs)].filter(Boolean));
  let release;
  const began = Date.now();
  let outcome = 'answered';
  try {
    assertActive(row, combined);
    const adapter = accounts.adapterFor(runtime);
    release = load.lease(entry, { background: true });
    quota.recordRequest(entry, reserved);
    getDatabase().prepare('UPDATE benchmark_samples SET calls = calls + 1 WHERE id = ? AND user_id = ?').run(row.id, row.user_id);
    // Persist the reservation before a crash can cause a replay without quota accounting.
    quota.flush({ prune: false });
    const { response, headers } = await adapter.chat({ ...request, stream: false, max_tokens: output }, {
      modelId: entry.modelId, model: entry, signal: combined, timeoutMs, benchmark: true,
    });
    const usage = normalizeUsage(response.usage);
    if (usage) quota.recordTokens(entry, usage.prompt_tokens + usage.completion_tokens - reserved);
    quota.observeHeaders(entry, headers);
    const charged = reportedCost(response.usage);
    billing.observeCost(entry, charged);
    if (charged > 0) throw new Deferred('billed_model');
    assertActive(row, combined);
    const choice = response.choices?.[0];
    // Cut off while thinking or answering: try again with twice the room, up to
    // the cap. Only a model that runs out even then makes the sample incomplete.
    if (choice?.finish_reason === 'length') {
      const cap = Math.min(MAX_ROOM, entry.maxOutput || MAX_ROOM);
      if (output >= cap) throw new Deferred('incomplete_output');
      needed.set(entry.ref, Math.min(cap, output * 2));
      throw new Deferred('needs_more_room', Date.now());
    }
    if (!choice || !['stop', 'tool_calls'].includes(choice.finish_reason) || invalidToolCalls(choice.message?.tool_calls)) throw new Deferred('incomplete_output');
    if (Buffer.byteLength(JSON.stringify(choice.message)) > POLICY.maxPayloadBytes) throw new Deferred('oversized_output');
    // Do not persist hidden reasoning or model/provider response metadata.
    return { role: 'assistant', content: choice.message.content ?? null, ...(choice.message.tool_calls?.length ? { tool_calls: choice.message.tool_calls } : {}) };
  } catch (error) {
    outcome = combined.aborted ? (combined.reason instanceof Deferred ? combined.reason.code : 'provider_timeout')
      : error instanceof Deferred ? error.code : `provider_${error.kind || 'failure'}`;
    if (combined.aborted) throw combined.reason instanceof Deferred ? combined.reason : new Deferred('provider_timeout');
    if (error instanceof Deferred) throw error;
    if (error.headers) quota.observeHeaders(entry, error.headers);
    // No connection at all (the server's internet is down, or the provider is
    // unreachable): not the sample's fault, so it is not counted against it.
    if (error.kind === 'network') throw new Deferred('network_down', Date.now() + NETWORK_PAUSE_MS);
    if (error.kind === 'rate_limit' || error.kind === 'quota_exhausted') {
      const resetAt = error.retryAfterMs ? Date.now() + error.retryAfterMs : Date.now() + POLICY.retryMs;
      quota.markExhausted(entry, error.learned?.metric || 'rpm', resetAt);
      throw new Deferred('waiting_quota', resetAt);
    }
    // Provider errors may echo request text. Only a closed error code survives.
    throw new Deferred('provider_failure');
  } finally {
    stopListening(); release?.(); quota.flush({ prune: false });
    // Which model, how much room and time, and what came of it: never content.
    if (release) logger.info('Benchmark call', { model: entry.canonicalId, account: entry.accountId, room: output, timeoutMs, ms: Date.now() - began, outcome });
  }
}
module.exports = { accountFull, outputRoom, replayTimeout, call, candidates, assertActive, automaticallyFree, Deferred };
