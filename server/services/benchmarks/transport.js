'use strict';

const { getDatabase } = require('../../db/database');
const catalog = require('../catalog/catalog_service');
const { REASONS } = require('../catalog/free_classifier');
const { RoutingPlan } = require('../routing/router');
const { KINDS } = require('../../providers/provider_error');
const { isPeerEntry } = require('../pools/links');
const { requirements, estimateInputTokens, invalidToolCalls } = require('../../capabilities/chat/canonical');
const consent = require('./consent');
const policy = require('./policy');
const logger = require('../../utils/logger').createLogger('local-benchmarks');

class Deferred extends Error {
  constructor(code, { retryAt = 0, failed = false } = {}) {
    super(code);
    Object.assign(this, { code, retryAt, failed });
  }
}

const cooling = new Map();
const quotaKinds = new Set([KINDS.RATE_LIMIT, KINDS.QUOTA_EXHAUSTED]);

function recordFailure(userId, entry, { kind, retryAfterMs }, now = Date.now()) {
  if (kind === KINDS.ABORTED || kind === KINDS.NETWORK) return;
  const paused = cooling.get(userId) || { models: new Map(), refs: new Map() };
  if (kind === KINDS.TIMEOUT) {
    paused.models.set(entry.canonicalId, now + policy.modelTimeoutCooldownMs);
  } else {
    const delay = Math.min(policy.maxRetryMs, Math.max(policy.retryMs, retryAfterMs || 0));
    paused.refs.set(entry.ref, now + delay);
  }
  cooling.set(userId, paused);
}

function ready(userId, entry, now = Date.now()) {
  const paused = cooling.get(userId);
  return !(paused?.models.get(entry.canonicalId) > now || paused?.refs.get(entry.ref) > now);
}

function prune(now) {
  for (const [userId, paused] of cooling) {
    for (const map of [paused.models, paused.refs]) {
      for (const [key, until] of map) if (until <= now) map.delete(key);
    }
    if (!paused.models.size && !paused.refs.size) cooling.delete(userId);
  }
}

consent.events.on('revoke', ({ userId, keyId }) => {
  if (keyId) return;
  if (userId) cooling.delete(userId);
  else cooling.clear();
});

const FREE = new Set([REASONS.PROVIDER_FREE_MODEL, REASONS.LISTED_ZERO_PRICE, REASONS.FREE_TIER_ACCOUNT]);
function eligible(entry) {
  if (!entry?.isFree || !entry.enabled || !entry.available || ['disabled', 'invalid'].includes(entry.accountStatus) || entry.isPlan || entry.isFlatRate || isPeerEntry(entry) || !FREE.has(entry.freeReason)) return false;
  const account = getDatabase().prepare('SELECT prepaid, enabled FROM provider_accounts WHERE id = ?').get(entry.accountId);
  return Boolean(account?.enabled && !account.prepaid);
}

function active(sample, signal) {
  return !signal?.aborted && sample.expiresAt > Date.now() && consent.ticket(sample.userId, sample.keyId) === sample.ticket;
}

function routingKey(sample) {
  const source = getDatabase().prepare('SELECT profile FROM router_keys WHERE id = ? AND user_id = ?').get(sample.keyId, sample.userId);
  return {
    id: sample.keyId, user_id: sample.userId, profile: source?.profile,
    allow_free: 1, allow_plan: 0, mode: 'free_only', pool_access: 'own', priority: 'low',
  };
}

function streamingRequest(request) {
  return { ...request, stream: true, max_tokens: request.max_tokens || policy.maxOutputTokens };
}

function candidates(sample, request, requestedModel, filter = () => true) {
  const canonical = streamingRequest(request);
  const plan = new RoutingPlan({
    userId: sample.userId, key: routingKey(sample), request: canonical, requestedModel,
    needs: requirements(canonical), inputTokens: estimateInputTokens(canonical), freeOnly: true,
    entryFilter: (entry) => eligible(entry) && ready(sample.userId, entry) && filter(entry), internalEvidence: false,
  });
  return plan.rank({ sample: false }).map(({ entry }) => entry);
}

async function call(sample, request, { signal, model = 'auto:smart', ref, stage = 'replay', filter = () => true } = {}) {
  if (!active(sample, signal)) throw new Deferred('consent_revoked');
  const started = Date.now();
  let result;
  try {
    const { executeBenchmark } = require('../../capabilities/chat/executor');
    result = await executeBenchmark({ key: routingKey(sample),
      canonical: streamingRequest({ ...request, model }),
      benchmark: {
        signal, pinned: ref != null, active: () => active(sample, signal),
        onFailure: (entry, error) => recordFailure(sample.userId, entry, error),
        eligible(entry, refresh) {
          const current = refresh ? catalog.getModel(sample.userId, entry.ref) : entry;
          return eligible(current) && ready(sample.userId, current) && (!ref || entry.ref === ref) && filter(entry);
        },
      },
    });
  } catch (error) {
    if (!active(sample, signal)) throw new Deferred('consent_revoked');
    const trail = error.details?.trail?.map(({ outcome, ms }) => ({ outcome, ms })) || [];
    const kind = error.kind || trail.at(-1)?.outcome;
    logger.info('Benchmark attempt', { stage, model, code: kind || error.code || 'failed', ms: Date.now() - started, trail });
    if (kind === KINDS.NETWORK) throw new Deferred('network_unavailable');
    if (quotaKinds.has(kind)) throw new Deferred('waiting_capacity', { retryAt: Date.now() + policy.tickMs });
    if (error.status === 503 || error.status === 429) {
      const retryAfter = error.details?.retryAfterSeconds;
      throw new Deferred('waiting_capacity', { retryAt: retryAfter ? Date.now() + retryAfter * 1000 : 0 });
    }
    throw new Deferred(kind === KINDS.TIMEOUT ? 'provider_timeout' : 'provider_failure', { failed: true });
  }
  if (!active(sample, signal)) throw new Deferred('consent_revoked');
  if (result?.charged) throw new Deferred('billed_model', { failed: true });
  if (!result || !['stop', 'tool_calls'].includes(result.finishReason) || invalidToolCalls(result.message.tool_calls)) {
    if (result) recordFailure(sample.userId, result.entry, { kind: KINDS.INVALID_OUTPUT });
    throw new Deferred('incomplete_output', { failed: true });
  }
  const { reasoning_content, ...message } = result.message;
  if (Buffer.byteLength(JSON.stringify(message)) > policy.maxPayloadBytes) {
    recordFailure(sample.userId, result.entry, { kind: KINDS.INVALID_OUTPUT });
    throw new Deferred('oversized_output', { failed: true });
  }
  logger.info('Benchmark attempt', { stage, model: result.entry.canonicalId, outputLimit: result.maxTokens, code: 'completed', ms: Date.now() - started });
  return { entry: result.entry, message };
}

module.exports = { candidates, call, active, eligible, prune, Deferred };
