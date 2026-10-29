'use strict';

const crypto = require('node:crypto');
const { getConfig } = require('../../config/env');
const webSearch = require('./web_search');
const { HttpError } = require('../../middleware/error_handler');
const { ProviderError, KINDS, fromNetworkError } = require('../../providers/provider_error');
const accounts = require('../../services/providers/account_service');
const { RoutingPlan, maxOutputFor } = require('../../services/routing/router');
const learning = require('../../services/routing/learning');
const stats = require('../../services/routing/stats_store');
const loadTracker = require('../../services/routing/load_tracker');
const quota = require('../../services/quota/quota_service');
const budget = require('../../services/budget/budget_service');
const usage = require('../../services/usage/usage_service');
const billing = require('../../services/providers/billing_check');
const requestProcessing = require('../../services/processing/processing_service');
const { restoreResponse, StreamRestorer } = require('../../services/processing/redaction');
const routerKeys = require('../../services/router_keys/router_key_service');
const { createLogger } = require('../../utils/logger');
const {
  requirements, estimateInputTokens, normalizeUsage, reportedCost, StreamAccumulator, invalidToolCalls, textOf,
} = require('./canonical');

const translators = {
  openai_chat: require('./translators/openai_chat'),
  anthropic_messages: require('./translators/anthropic_messages'),
  openai_responses: require('./translators/openai_responses'),
};

const logger = createLogger('gateway');
const STREAM_IDLE_TIMEOUT_MS = 120_000;
const HEDGE_FACTOR = 3;
const HEDGE_MIN_MS = { stream: 2500, plain: 4000 };
const HEDGE_MAX_MS = 30_000;
const HEDGE_MIN_SAMPLES = 3;
const HEDGE_RETRY_MS = 2000;
const QUEUE_POLL_MS = 1000;
// Rejections that clear by themselves within seconds to minutes. When every
// candidate is blocked only by these, the request waits instead of failing.
const TEMPORARY_REJECTIONS = new Set(['rate_limited', 'quota_exhausted', 'circuit_open', 'cooldown']);

const KEY_POLICY_REJECTIONS = new Set(['not_free', 'not_allowed_for_key', 'price_unknown']);
// Rejections that do not change within the lifetime of one request.
const PERMANENT_REJECTIONS = new Set(['model_disabled', 'model_unavailable', 'account_unavailable', 'no_tools', 'no_vision', 'not_available', 'no_free_quota', 'incompatible', 'payment_required', 'auth_failed', 'model_excluded', 'provider_excluded', 'provider_not_included', 'below_min_intelligence', 'too_expensive', 'budget_exhausted', 'needs_more_tokens']);

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const MAX_CONSECUTIVE_BAD_REQUESTS = 3;

// No answer at all: a reasoning model that spent max_tokens on thinking is
// "truncated" (learned, mild), anything else is empty (a real failure).
function noAnswer(finishReason) {
  if (finishReason === 'length') return new ProviderError(KINDS.TRUNCATED, 'The model used the whole max_tokens budget before answering.');
  return new ProviderError(KINDS.EMPTY, 'The provider returned no answer.');
}

// Only an answer or a tool call is output. Reasoning alone (e.g. a stop
// sequence that fired while the model was still thinking) gives the client
// nothing, so it counts as empty unless the answer was cut by max_tokens.
function hasAnswer(message) {
  return Boolean(textOf(message?.content).trim() || message?.tool_calls?.length);
}

function estimateOutputTokens(accumulator) {
  return Math.ceil((accumulator.content.length + accumulator.reasoning.length + JSON.stringify(accumulator.toolCalls).length * (accumulator.toolCalls.length ? 1 : 0)) / 4);
}

function writeSse(res, event) {
  if (event.event) res.write(`event: ${event.event}\n`);
  res.write(`data: ${typeof event.data === 'string' ? event.data : JSON.stringify(event.data)}\n\n`);
}

function streamErrorEvent(format, error) {
  const message = error.message || 'Upstream stream failed.';
  if (format === 'anthropic_messages') return { event: 'error', data: { type: 'error', error: { type: 'api_error', message } } };
  if (format === 'openai_responses') return { event: 'error', data: { type: 'error', code: error.kind || 'server_error', message } };
  return { data: { error: { message, type: 'upstream_error', code: error.kind || 'server_error' } } };
}

// How long to wait for an attempt before hedging with another free model:
// a multiple of what this model usually needs, or a default when unmeasured.
function hedgeDelay(entry, stream) {
  const snapshot = stats.snapshot(entry.ref);
  const usual = stream ? snapshot.ttft_ms : snapshot.latency_ms;
  const kind = stream ? 'stream' : 'plain';
  if (snapshot.samples < HEDGE_MIN_SAMPLES || !usual) return getConfig().routing.hedgeUnmeasuredMs;
  return Math.min(HEDGE_MAX_MS, Math.max(HEDGE_MIN_MS[kind], usual * HEDGE_FACTOR));
}

function trailItem(entry, outcome, extra = {}) {
  return { model: entry.id, ref: entry.ref, outcome, ...extra };
}

// Wraps the remaining upstream iterator so idle gaps abort the attempt.
async function* withIdleTimeout(iterator, controller, idleMs) {
  let timer = setTimeout(() => controller.abort(new ProviderError(KINDS.TIMEOUT, 'The stream stalled.')), idleMs);
  try {
    for (;;) {
      const step = await iterator.next();
      clearTimeout(timer);
      if (step.done) return;
      yield step.value;
      timer = setTimeout(() => controller.abort(new ProviderError(KINDS.TIMEOUT, 'The stream stalled.')), idleMs);
    }
  } finally {
    clearTimeout(timer);
  }
}

// tool_choice "required" or a named function obliges the model to call a tool.
function toolCallRequired(canonical) {
  return Boolean(canonical.tools?.length) && (canonical.tool_choice === 'required' || typeof canonical.tool_choice === 'object');
}

const missingToolCall = () => new ProviderError(KINDS.INVALID_OUTPUT, 'The model ignored tool_choice and answered without a tool call.');

async function runAttempt({ entry, adapter, canonical, stream, signal, timeoutMs }) {
  const startedAt = Date.now();
  const mustCallTool = toolCallRequired(canonical);
  if (!stream) {
    const { response, headers } = await adapter.chat(canonical, { modelId: entry.modelId, signal, timeoutMs, model: entry });
    const choice = response?.choices?.[0];
    const message = choice?.message;
    // A reasoning model can spend a small max_tokens entirely on thinking;
    // that is a truncated answer, not a provider failure.
    if (!hasAnswer(message)) throw noAnswer(choice?.finish_reason);
    if (invalidToolCalls(message.tool_calls)) throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned malformed tool-call arguments.');
    if (mustCallTool && !message?.tool_calls?.length) throw missingToolCall();
    return { response, headers, latencyMs: Date.now() - startedAt };
  }
  // Streams are committed to the client only once real output arrived, so a
  // provider that fails or stays silent can still be replaced transparently.
  const controller = new AbortController();
  const combined = AbortSignal.any([signal, controller.signal]);
  let headers = null;
  const iterator = adapter.stream(canonical, { modelId: entry.modelId, signal: combined, onHeaders: (value) => { headers = value; }, model: entry })[Symbol.asyncIterator]();
  const firstTimer = setTimeout(() => controller.abort(new ProviderError(KINDS.TIMEOUT, 'No output before the first-token timeout.')), timeoutMs);
  const accumulator = new StreamAccumulator();
  const buffered = [];
  let ttftMs = null;
  try {
    for (;;) {
      const step = await iterator.next();
      if (step.done) break;
      accumulator.push(step.value);
      buffered.push(step.value);
      // Commit on the first real answer token (reasoning is buffered), and
      // with a required tool call only once the call starts.
      if (accumulator.hasAnswer() && ttftMs === null) ttftMs = Date.now() - startedAt;
      if (accumulator.hasAnswer() && (!mustCallTool || accumulator.toolCalls.length)) break;
    }
  } catch (error) {
    throw controller.signal.aborted && controller.signal.reason instanceof ProviderError ? controller.signal.reason : fromNetworkError(error);
  } finally {
    clearTimeout(firstTimer);
  }
  if (!accumulator.hasAnswer()) throw noAnswer(accumulator.finishReason);
  if (mustCallTool && !accumulator.toolCalls.length) throw missingToolCall();
  const rest = withIdleTimeout(iterator, controller, STREAM_IDLE_TIMEOUT_MS);
  return { buffered, rest, accumulator, headers, ttftMs, startedAt, controller };
}

/**
 * canonical: an already translated request (web search turns); internal:
 * a turn of a request that already passed the key's rate limit.
 */
async function execute({ req, res, key, format, body, freeOnly = false, canonical: preset = null, internal = false }) {
  const translator = translators[format];
  // Built-in web search runs the request as several turns (web_search.js).
  if (!preset) {
    const search = webSearch.requested(format, body || {});
    if (search && webSearch.available(key.user_id)) {
      const rpm = routerKeys.consumeRpm(key);
      if (!rpm.allowed) {
        res.set('Retry-After', String(rpm.retryAfterSeconds));
        throw new HttpError(429, 'KEY_RATE_LIMITED', 'This router key exceeded its requests-per-minute limit.', { retryAfterSeconds: rpm.retryAfterSeconds });
      }
      return webSearch.executeWithSearch({ execute, req, res, key, format, spec: search, freeOnly, translator: safeTranslator(translator) });
    }
    // Without a search engine the marker is dropped, as before.
    if (search) body = search.body;
  }
  const requestId = (internal ? null : req.id) || crypto.randomUUID();
  const startedAt = Date.now();
  const requestedModel = String(body?.model || preset?.model || 'auto');
  let canonical;
  try {
    canonical = preset ? structuredClone(preset) : translator.toCanonical(body || {});
  } catch (error) {
    if (error instanceof HttpError) throw error;
    throw new HttpError(400, 'INVALID_REQUEST', `The request body could not be read: ${error.message}`);
  }
  if (!canonical.messages.length) throw new HttpError(400, 'INVALID_REQUEST', 'The request contains no messages.');

  const rpm = internal ? { allowed: true } : routerKeys.consumeRpm(key);
  if (!rpm.allowed) {
    res.set('Retry-After', String(rpm.retryAfterSeconds));
    throw new HttpError(429, 'KEY_RATE_LIMITED', 'This router key exceeded its requests-per-minute limit.', { retryAfterSeconds: rpm.retryAfterSeconds });
  }
  const remaining = budget.remaining(key);
  const budgetExhausted = remaining !== null && remaining <= 0;
  if (budgetExhausted && key.mode === 'paid') {
    throw new HttpError(402, 'BUDGET_EXHAUSTED', `This router key has used its ${key.budget_period} budget of ${key.budget_usd} USD.`);
  }

  // Compression and the privacy shield run before routing, so the router sees
  // the real request size and providers never see hidden values.
  const processing = await requestProcessing.apply(key.user_id, canonical, { compressionLevel: req.get?.('x-neorouter-compression') || null });
  canonical = processing.canonical;
  const { vault } = processing;
  const processingStats = processing.stats;

  const stream = canonical.stream;
  const needs = requirements(canonical);
  const inputTokens = estimateInputTokens(canonical);
  const plan = new RoutingPlan({
    userId: key.user_id, key, request: canonical, requestedModel, needs, inputTokens, freeOnly: freeOnly || budgetExhausted,
  });

  const config = getConfig();
  const clientAbort = new AbortController();
  res.on('close', () => { if (!res.writableFinished) clientAbort.abort(new ProviderError(KINDS.ABORTED, 'The client disconnected.')); });
  const trail = [];
  let lastError = null;
  let badRequests = 0;

  const finish = (fields) => {
    try {
      usage.record({
        id: requestId,
        user_id: key.user_id,
        router_key_id: key.id,
        created_at: startedAt,
        api_format: format,
        requested_model: requestedModel,
        attempts: Math.max(1, trail.length),
        stream: stream ? 1 : 0,
        trail_json: JSON.stringify(trail),
        redacted_count: processingStats.redacted,
        compression_saved_tokens: processingStats.compression.savedTokens,
        latency_ms: Date.now() - startedAt,
        input_tokens: 0,
        output_tokens: 0,
        cost_usd: 0,
        is_free: 0,
        ...fields,
      });
    } catch (error) {
      logger.error('Failed to record request', { requestId, error });
    }
  };

  // Commits a successful attempt to the client (JSON or SSE) and records it.
  const commit = async (attempt, result) => {
    const { entry, reservation } = attempt;
    res.set('X-NeoRouter-Model', entry.id);
    res.set('X-NeoRouter-Trail', [...trail.map((item) => item.model), entry.id].join(','));
    if (processingStats.compression.level !== 'off') res.set('X-NeoRouter-Compression', `${processingStats.compression.level}; saved~=${processingStats.compression.savedTokens}`);
    if (processingStats.redacted) res.set('X-NeoRouter-Redacted', String(processingStats.redacted));
    if (!stream) {
      const tokens = normalizeUsage(result.response.usage) || {
        prompt_tokens: inputTokens,
        completion_tokens: Math.ceil(textOf(result.response.choices[0].message.content).length / 4),
      };
      const cost = budget.costOf(entry, tokens);
      reservation.settle(cost, requestId);
      billing.observeCost(entry, reportedCost(result.response.usage));
      quota.recordTokens(entry, tokens.prompt_tokens + tokens.completion_tokens - inputTokens);
      learning.onSuccess(entry, { latencyMs: result.latencyMs, ttftMs: null, outputTokens: tokens.completion_tokens, generationMs: result.latencyMs }, result.headers);
      trail.push(trailItem(entry, 'success', { ms: result.latencyMs }));
      finish({
        status: 'success', http_status: 200, model_ref: entry.ref, model_label: entry.id, account_id: entry.accountId,
        input_tokens: tokens.prompt_tokens, output_tokens: tokens.completion_tokens, cost_usd: cost, is_free: entry.isFree ? 1 : 0,
      });
      const response = vault ? restoreResponse(result.response, vault) : result.response;
      res.json(translator.responseFromCanonical({ ...response, usage: tokens }, entry.id));
      return;
    }
    res.status(200);
    res.set({ 'Content-Type': 'text/event-stream; charset=utf-8', 'Cache-Control': 'no-cache, no-transform', Connection: 'keep-alive', 'X-Accel-Buffering': 'no' });
    res.flushHeaders();
    const { accumulator } = result;
    // Placeholders are put back on the way out; usage accounting keeps the
    // chunks as the provider sent them.
    const restorer = vault ? new StreamRestorer(vault) : null;
    let template = null;
    const outgoing = (chunk) => {
      if (chunk?.id) template = { id: chunk.id, object: chunk.object, created: chunk.created, model: chunk.model };
      return restorer ? restorer.process(chunk) : chunk;
    };
    async function* replay() {
      for (const chunk of result.buffered) yield outgoing(chunk);
      for await (const chunk of result.rest) {
        accumulator.push(chunk);
        yield outgoing(chunk);
      }
      const tail = restorer?.flush(template || {});
      if (tail) yield tail;
    }
    let streamError = null;
    try {
      for await (const event of translator.streamFromCanonical(replay(), entry.id)) writeSse(res, event);
    } catch (error) {
      streamError = result.controller.signal.reason instanceof ProviderError ? result.controller.signal.reason : fromNetworkError(error);
      if (streamError.kind !== KINDS.ABORTED) writeSse(res, streamErrorEvent(format, streamError));
    }
    res.end();
    const durationMs = Date.now() - attempt.started;
    const tokens = accumulator.usage || { prompt_tokens: inputTokens, completion_tokens: estimateOutputTokens(accumulator) };
    const cost = budget.costOf(entry, tokens);
    reservation.settle(cost, requestId);
    billing.observeCost(entry, accumulator.reportedCost);
    quota.recordTokens(entry, tokens.prompt_tokens + tokens.completion_tokens - inputTokens);
    if (streamError) {
      if (streamError.kind !== KINDS.ABORTED) learning.onFailure(entry, streamError, { estimatedTokens: inputTokens });
    } else if (invalidToolCalls(accumulator.toolCalls)) {
      learning.onFailure(entry, new ProviderError(KINDS.INVALID_OUTPUT, 'Streamed tool-call arguments were not valid JSON.'));
    } else {
      learning.onSuccess(entry, {
        latencyMs: durationMs, ttftMs: result.ttftMs, outputTokens: tokens.completion_tokens, generationMs: durationMs - result.ttftMs,
      }, result.headers);
    }
    trail.push(trailItem(entry, streamError ? `stream_${streamError.kind}` : 'success', { ms: durationMs }));
    finish({
      status: streamError && streamError.kind !== KINDS.ABORTED ? 'error' : 'success',
      http_status: 200,
      error_code: streamError ? (streamError.kind === KINDS.ABORTED ? 'client_closed' : streamError.kind) : null,
      error_message: streamError?.message || null,
      model_ref: entry.ref, model_label: entry.id, account_id: entry.accountId,
      input_tokens: tokens.prompt_tokens, output_tokens: tokens.completion_tokens, cost_usd: cost, is_free: entry.isFree ? 1 : 0,
      ttft_ms: result.ttftMs,
    });
  };

  // Attempts run as a small race: a fresh attempt starts when the current
  // ones fail, or (hedging) when a free attempt is slower than that model
  // usually is. The first success wins and the others are cancelled.
  const running = new Set();
  let attemptsStarted = 0;
  let nextHedgeAt = 0;

  const launch = (entry) => {
    const runtime = accounts.getRuntime(entry.accountId);
    if (!runtime) return false;
    const adapter = accounts.adapterFor(runtime);
    const controller = new AbortController();
    const attempt = {
      entry,
      controller,
      started: Date.now(),
      reservation: budget.reserve(key, budget.estimateMaxCost(entry, inputTokens, maxOutputFor(canonical, entry)) || 0),
      release: loadTracker.lease(entry),
    };
    quota.recordRequest(entry, inputTokens);
    const timeoutMs = Math.min(adapter.timeoutMs, config.routing.totalBudgetMs - (Date.now() - startedAt));
    attempt.promise = runAttempt({ entry, adapter, canonical, stream, signal: AbortSignal.any([clientAbort.signal, controller.signal]), timeoutMs })
      .then((result) => ({ attempt, result }), (error) => ({ attempt, error }));
    running.add(attempt);
    attemptsStarted += 1;
    return true;
  };

  const startNext = (filter = null) => {
    while (attemptsStarted < config.routing.maxAttempts && Date.now() - startedAt < config.routing.totalBudgetMs && !clientAbort.signal.aborted) {
      const choice = plan.next({ filter });
      if (!choice) return false;
      if (launch(choice.entry)) return true;
    }
    return false;
  };

  // Starts an attempt, or waits for temporarily blocked candidates to free up
  // (bursts beyond every provider's rate limit queue instead of failing).
  const ensureRunning = async () => {
    const waitUntil = Date.now() + config.routing.queueWaitMs;
    while (!running.size) {
      if (startNext()) return true;
      const remaining = Math.min(waitUntil, startedAt + config.routing.totalBudgetMs) - Date.now();
      if (remaining <= 0 || clientAbort.signal.aborted || attemptsStarted >= config.routing.maxAttempts) return false;
      const reasons = Object.keys(plan.rejectionSummary());
      if (!reasons.some((reason) => TEMPORARY_REJECTIONS.has(reason) || reason.startsWith('quota_'))) return false;
      await sleep(Math.min(remaining, QUEUE_POLL_MS * (0.5 + Math.random())));
    }
    return true;
  };

  const settle = (attempt) => {
    running.delete(attempt);
    attempt.release();
  };

  await ensureRunning();
  while (running.size) {
    const newest = [...running].reduce((a, b) => (b.started > a.started ? b : a));
    const canHedge = config.routing.hedging && running.size < config.routing.maxParallel
      && attemptsStarted < config.routing.maxAttempts && [...running].every((item) => item.entry.isFree);
    let timer = null;
    const contenders = [...running].map((item) => item.promise);
    if (canHedge) {
      const wait = Math.max(0, hedgeDelay(newest.entry, stream) - (Date.now() - newest.started), nextHedgeAt - Date.now());
      contenders.push(new Promise((resolve) => { timer = setTimeout(() => resolve({ hedge: true }), wait); }));
    }
    const outcome = await Promise.race(contenders);
    clearTimeout(timer);
    if (outcome.hedge) {
      // Hedges only go to free models, so a slow attempt never costs money twice.
      if (!startNext((entry) => entry.isFree)) nextHedgeAt = Date.now() + HEDGE_RETRY_MS;
      continue;
    }
    const { attempt } = outcome;
    if (outcome.error) {
      settle(attempt);
      attempt.reservation.release();
      const error = outcome.error instanceof ProviderError ? outcome.error : fromNetworkError(outcome.error);
      if (error.kind === KINDS.ABORTED && clientAbort.signal.aborted) break;
      lastError = error;
      trail.push(trailItem(attempt.entry, error.kind, { status: error.status, ms: Date.now() - attempt.started, message: String(error.message).slice(0, 200) }));
      const policy = learning.onFailure(attempt.entry, error, { estimatedTokens: inputTokens, maxTokens: canonical.max_tokens });
      if (error.kind === KINDS.AUTH) accounts.validateAccount(attempt.entry.accountId).catch(() => {});
      badRequests = error.kind === KINDS.BAD_REQUEST ? badRequests + 1 : 0;
      if (!policy.fallback || badRequests >= MAX_CONSECUTIVE_BAD_REQUESTS) {
        for (const other of [...running]) {
          other.controller.abort(new ProviderError(KINDS.ABORTED, 'Request stopped.'));
          settle(other);
          other.reservation.release();
        }
        break;
      }
      if (!running.size) await ensureRunning();
      continue;
    }
    for (const other of [...running]) {
      if (other === attempt) continue;
      other.controller.abort(new ProviderError(KINDS.ABORTED, 'Another attempt answered first.'));
      settle(other);
      other.reservation.release();
      trail.push(trailItem(other.entry, 'hedge_lost', { ms: Date.now() - other.started }));
      learning.onSlow(other.entry, Date.now() - other.started);
    }
    running.delete(attempt);
    try {
      await commit(attempt, outcome.result);
    } finally {
      attempt.release();
    }
    return undefined;
  }

  if (clientAbort.signal.aborted) {
    finish({ status: 'error', http_status: 499, error_code: 'client_closed', error_message: 'The client disconnected.' });
    return undefined;
  }
  const rejected = plan.rejectionSummary();
  // An explicitly named model this key may never use is a permission error,
  // not a temporary outage.
  const reasons = Object.keys(rejected);
  if (!plan.parsed.auto && trail.length === 0 && reasons.length && reasons.every((reason) => KEY_POLICY_REJECTIONS.has(reason))) {
    const why = reasons.includes('not_free') ? 'is not free and this key only allows free models' : 'is not allowed for this key';
    const message = `The model "${requestedModel}" ${why}.`;
    finish({ status: 'error', http_status: 403, error_code: 'MODEL_NOT_ALLOWED', error_message: message });
    throw new HttpError(403, 'MODEL_NOT_ALLOWED', message, { rejected });
  }
  if (trail.length === 0 && reasons.length && reasons.every((reason) => reason.startsWith('context_') || KEY_POLICY_REJECTIONS.has(reason) || PERMANENT_REJECTIONS.has(reason)) && reasons.some((reason) => reason.startsWith('context_'))) {
    const message = `The request (~${inputTokens} tokens) is larger than the context window of every model available to this key.`;
    finish({ status: 'error', http_status: 400, error_code: 'CONTEXT_TOO_LARGE', error_message: message });
    throw new HttpError(400, 'CONTEXT_TOO_LARGE', message, { rejected });
  }
  const status = lastError?.kind === KINDS.BAD_REQUEST && badRequests >= MAX_CONSECUTIVE_BAD_REQUESTS ? 400
    : lastError?.kind === KINDS.CONTEXT_LENGTH ? 400
      : trail.length === 0 && budgetExhausted ? 402
        : trail.length === 0 ? 503 : 502;
  const code = trail.length === 0 ? 'NO_MODEL_AVAILABLE' : 'ALL_ATTEMPTS_FAILED';
  const message = trail.length === 0
    ? 'No model in your catalog can serve this request right now (filters, quotas, cooldowns or budget).'
    : `All ${trail.length} attempt(s) failed. Last error: ${lastError?.message || 'unknown'}`;
  finish({ status: 'error', http_status: status, error_code: code, error_message: message });
  throw new HttpError(status, code, message, { trail, rejected });
}

// Translation errors become 400s, as in execute().
function safeTranslator(translator) {
  return {
    ...translator,
    toCanonical(body) {
      try {
        return translator.toCanonical(body);
      } catch (error) {
        if (error instanceof HttpError) throw error;
        throw new HttpError(400, 'INVALID_REQUEST', `The request body could not be read: ${error.message}`);
      }
    },
  };
}

module.exports = { execute, translators };
