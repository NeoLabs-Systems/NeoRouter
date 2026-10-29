'use strict';

// One request of a non-chat kind (embeddings, images, speech, transcription,
// System One, video job creation) through the same machinery as chat: key
// policy and budget, routing over the models of that kind, fallback across
// providers, learning from every attempt, quotas, usage log. Streaming and
// hedging are chat-only; these calls are single-shot.
const crypto = require('node:crypto');
const { getConfig } = require('../../config/env');
const { HttpError } = require('../../middleware/error_handler');
const { ProviderError, KINDS, fromNetworkError } = require('../../providers/provider_error');
const accounts = require('../../services/providers/account_service');
const { RoutingPlan } = require('../../services/routing/router');
const learning = require('../../services/routing/learning');
const loadTracker = require('../../services/routing/load_tracker');
const quota = require('../../services/quota/quota_service');
const budget = require('../../services/budget/budget_service');
const usage = require('../../services/usage/usage_service');
const routerKeys = require('../../services/router_keys/router_key_service');

const KEY_POLICY_REJECTIONS = new Set(['not_free', 'not_allowed_for_key', 'price_unknown']);
const MAX_CONSECUTIVE_BAD_REQUESTS = 2;

function trailItem(entry, outcome, extra = {}) {
  return { model: entry.id, outcome, ...extra };
}

/**
 * @param {object} options
 * @param {string} options.capability   catalog kind, e.g. 'embedding'
 * @param {string} options.method       adapter method that serves it, e.g. 'embed'
 * @param {(ctx) => Promise<object>} options.invoke   calls the adapter: ({ adapter, entry, signal, timeoutMs }) => result
 * @param {(entry, result) => number} options.costOf  actual cost in USD
 * @param {(entry) => number|null} options.estimateCost  worst-case cost before the call (null = unknown)
 * @param {(result, entry) => void} [options.respond]  writes the HTTP response; omitted → returns { result, entry }
 * @param {number} [options.inputTokens]  size estimate for quota checks and the log
 * @param {number} [options.contextTokens]  text that must fit the model's context (defaults to inputTokens)
 * @param {number} [options.units]        billed units (images, seconds, …) for the log
 * @param {(entry, adapter) => boolean} [options.filter]  extra candidate filter (embedding pins, timestamps)
 * @param {() => HttpError|null} [options.onNoCandidate]  explains an empty candidate list
 */
async function runCapability({ req, res = null, key, capability, method, requestedModel = 'auto', invoke, costOf, estimateCost, respond, inputTokens = 0, units = 0, format = capability, freeOnly = false, filter = null, onNoCandidate = null, contextTokens = inputTokens }) {
  const requestId = req.id || crypto.randomUUID();
  const startedAt = Date.now();
  const trail = [];

  const rpm = routerKeys.consumeRpm(key);
  if (!rpm.allowed) {
    res?.set('Retry-After', String(rpm.retryAfterSeconds));
    throw new HttpError(429, 'KEY_RATE_LIMITED', 'This router key exceeded its requests-per-minute limit.', { retryAfterSeconds: rpm.retryAfterSeconds });
  }
  const remaining = budget.remaining(key);
  const budgetExhausted = remaining !== null && remaining <= 0;
  if (budgetExhausted && key.mode === 'paid') {
    throw new HttpError(402, 'BUDGET_EXHAUSTED', `This router key has used its ${key.budget_period} budget of ${key.budget_usd} USD.`);
  }

  const plan = new RoutingPlan({
    userId: key.user_id,
    key,
    request: { messages: [] },
    requestedModel,
    needs: {},
    inputTokens: contextTokens,
    freeOnly: freeOnly || budgetExhausted,
    capability,
    estimateCost,
  });

  const finish = (fields) => {
    try {
      usage.record({
        id: requestId, user_id: key.user_id, router_key_id: key.id, created_at: startedAt, api_format: format,
        requested_model: requestedModel, attempts: Math.max(1, trail.length), stream: 0, trail_json: JSON.stringify(trail),
        latency_ms: Date.now() - startedAt, input_tokens: inputTokens, output_tokens: 0, cost_usd: 0, is_free: 0,
        capability, units, ...fields,
      });
    } catch {
      // Logging must never break a request.
    }
  };

  // Only providers whose adapter speaks this kind are candidates.
  const supported = (entry) => {
    const runtime = accounts.getRuntime(entry.accountId);
    if (!runtime) return false;
    const adapter = accounts.adapterFor(runtime);
    return typeof adapter[method] === 'function' && adapter.supports(method) && (!filter || filter(entry, adapter));
  };

  const config = getConfig();
  let lastError = null;
  let badRequests = 0;
  while (trail.length < config.routing.maxAttempts && Date.now() - startedAt < config.routing.totalBudgetMs) {
    const choice = plan.next({ filter: supported });
    if (!choice) break;
    const { entry } = choice;
    const adapter = accounts.adapterFor(accounts.getRuntime(entry.accountId));
    const reservation = budget.reserve(key, estimateCost(entry) || 0);
    const release = loadTracker.lease(entry);
    quota.recordRequest(entry, inputTokens);
    const attemptStarted = Date.now();
    try {
      const timeoutMs = Math.min(adapter.timeoutMs, config.routing.totalBudgetMs - (Date.now() - startedAt));
      const result = await invoke({ adapter, entry, signal: AbortSignal.timeout(Math.max(1000, timeoutMs)), timeoutMs });
      const latencyMs = Date.now() - attemptStarted;
      const cost = costOf(entry, result);
      reservation.settle(cost, requestId);
      learning.onSuccess(entry, { latencyMs, ttftMs: null, outputTokens: 0, generationMs: latencyMs }, result.headers || null);
      trail.push(trailItem(entry, 'success', { ms: latencyMs }));
      if (res) {
        res.set('X-NeoRouter-Model', entry.id);
        res.set('X-NeoRouter-Trail', trail.map((item) => item.model).join(','));
      }
      finish({
        status: 'success', http_status: 200, model_ref: entry.ref, model_label: entry.id, account_id: entry.accountId,
        cost_usd: cost, is_free: entry.isFree ? 1 : 0, units: result.units ?? units, input_tokens: result.inputTokens ?? inputTokens,
      });
      // Without respond (internal callers such as chat web search) the result is returned.
      if (!respond) return { result, entry };
      await respond(result, entry);
      return { result, entry };
    } catch (caught) {
      reservation.release();
      const error = caught instanceof ProviderError ? caught : caught instanceof HttpError ? caught : fromNetworkError(caught);
      if (error instanceof HttpError) throw error;
      lastError = error;
      trail.push(trailItem(entry, error.kind, { status: error.status, ms: Date.now() - attemptStarted, message: String(error.message).slice(0, 200) }));
      const policy = learning.onFailure(entry, error, { estimatedTokens: inputTokens });
      if (error.kind === KINDS.AUTH) accounts.validateAccount(entry.accountId).catch(() => {});
      badRequests = error.kind === KINDS.BAD_REQUEST ? badRequests + 1 : 0;
      if (!policy.fallback || badRequests >= MAX_CONSECUTIVE_BAD_REQUESTS) break;
    } finally {
      release();
    }
  }

  if (trail.length === 0 && onNoCandidate) {
    const explained = onNoCandidate();
    if (explained) {
      finish({ status: 'error', http_status: explained.status, error_code: explained.code, error_message: explained.message });
      throw explained;
    }
  }
  const rejected = plan.rejectionSummary();
  const reasons = Object.keys(rejected);
  if (!plan.parsed.auto && trail.length === 0 && reasons.length && reasons.every((reason) => KEY_POLICY_REJECTIONS.has(reason))) {
    const why = reasons.includes('not_free') ? 'is not free and this key only allows free models' : 'is not allowed for this key';
    const message = `The model "${requestedModel}" ${why}.`;
    finish({ status: 'error', http_status: 403, error_code: 'MODEL_NOT_ALLOWED', error_message: message });
    throw new HttpError(403, 'MODEL_NOT_ALLOWED', message, { rejected });
  }
  const status = lastError?.kind === KINDS.BAD_REQUEST && badRequests >= MAX_CONSECUTIVE_BAD_REQUESTS ? 400
    : trail.length === 0 && budgetExhausted ? 402
      : trail.length === 0 ? 503 : 502;
  const code = trail.length === 0 ? 'NO_MODEL_AVAILABLE' : 'ALL_ATTEMPTS_FAILED';
  const message = trail.length === 0
    ? `No ${capability} model in your catalog can serve this request right now (filters, quotas, cooldowns or budget).`
    : `All ${trail.length} attempt(s) failed. Last error: ${lastError?.message || 'unknown'}`;
  finish({ status: 'error', http_status: status, error_code: code, error_message: message });
  throw new HttpError(status, code, message, { trail, rejected });
}

module.exports = { runCapability };
