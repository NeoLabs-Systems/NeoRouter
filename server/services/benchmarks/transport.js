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
  return { id: sample.keyId, user_id: sample.userId, allow_free: 1, allow_plan: 0, mode: 'free_only', pool_access: 'own', priority: 'low' };
}

function streamingRequest(request) {
  return { ...request, stream: true, max_tokens: request.max_tokens || policy.maxOutputTokens };
}

function candidates(sample, request, requestedModel, filter = () => true) {
  const canonical = streamingRequest(request);
  const plan = new RoutingPlan({
    userId: sample.userId, key: routingKey(sample), request: canonical, requestedModel,
    needs: requirements(canonical), inputTokens: estimateInputTokens(canonical), freeOnly: true,
    entryFilter: (entry) => eligible(entry) && filter(entry), internalEvidence: false,
  });
  return plan.rank({ sample: false }).map(({ entry }) => entry);
}

async function call(sample, request, { signal, model = 'auto:smart', ref, filter = () => true } = {}) {
  if (!active(sample, signal)) throw new Deferred('consent_revoked');
  const started = Date.now();
  let result;
  try {
    const { executeBenchmark } = require('../../capabilities/chat/executor');
    result = await executeBenchmark({ key: routingKey(sample),
      canonical: streamingRequest({ ...request, model }),
      benchmark: {
        signal, pinned: ref != null, active: () => active(sample, signal),
        eligible(entry, refresh) {
          const current = refresh ? catalog.getModel(sample.userId, entry.ref) : entry;
          return eligible(current) && (!ref || entry.ref === ref) && filter(entry);
        },
      },
    });
  } catch (error) {
    if (!active(sample, signal)) throw new Deferred('consent_revoked');
    const trail = error.details?.trail?.map(({ outcome, ms }) => ({ outcome, ms })) || [];
    const kind = error.kind || trail.at(-1)?.outcome;
    logger.info('Benchmark attempt', { model, code: kind || error.code || 'failed', ms: Date.now() - started, trail });
    if (kind === KINDS.NETWORK) throw new Deferred('network_unavailable');
    if (error.status === 503 || error.status === 429 || [KINDS.RATE_LIMIT, KINDS.QUOTA_EXHAUSTED].includes(kind)) {
      throw new Deferred('waiting_capacity', { retryAt: started + (error.details?.retryAfterSeconds || 0) * 1000 });
    }
    throw new Deferred('provider_failure', { failed: true });
  }
  if (!active(sample, signal)) throw new Deferred('consent_revoked');
  if (result?.charged) throw new Deferred('billed_model', { failed: true });
  if (!result || !['stop', 'tool_calls'].includes(result.finishReason) || invalidToolCalls(result.message.tool_calls)) {
    throw new Deferred('incomplete_output', { failed: true });
  }
  const { reasoning_content, ...message } = result.message;
  if (Buffer.byteLength(JSON.stringify(message)) > policy.maxPayloadBytes) throw new Deferred('oversized_output', { failed: true });
  logger.info('Benchmark attempt', { model: result.entry.canonicalId, maxTokens: result.maxTokens, code: 'completed', ms: Date.now() - started });
  return { entry: result.entry, message };
}

module.exports = { candidates, call, active, eligible, Deferred };
