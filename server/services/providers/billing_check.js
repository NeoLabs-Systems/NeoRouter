'use strict';

// Proves, per account, that a provider's free tier really cannot charge
// money. Providers whose free tier ends silently once a card or billing
// account is attached (Groq, Google) declare `billing.verify`; until a check
// here returns "free", their free-tier models are not treated as free.
//
// Checks:
//   ratelimit_headers  A one-token request; the daily request limit in the
//                      response headers separates free from paid plans.
//                      Also applied passively to every routed response.
//   paid_model_probe   A one-token request to a model without a free tier.
//                      Free projects get "free_tier … limit: 0" (429, not
//                      billed); a billed project answers. Checked on add and
//                      every few hours.
const { getDatabase } = require('../../db/database');
const registry = require('../../providers/registry');
const accounts = require('./account_service');
const catalog = require('../catalog/catalog_service');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('billing-check');
const TIMEOUT_MS = 30_000;
const FREE_TIER_ERROR = /free[_ ]tier/i;

function setState(accountId, state, message) {
  const db = getDatabase();
  const row = db.prepare('SELECT billing_state FROM provider_accounts WHERE id = ?').get(accountId);
  if (!row) return;
  db.prepare(`UPDATE provider_accounts SET billing_state = ?, billing_message = ?,
    billing_checked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`).run(state, message, accountId);
  if (row.billing_state !== state) {
    const log = state === 'paid' ? logger.warn : logger.info;
    log('Billing state changed', { accountId, from: row.billing_state, to: state, message });
    catalog.reclassifyAccount(accountId);
  }
}

function probeModel(accountId, patterns) {
  const rows = getDatabase().prepare(`SELECT model_id FROM models WHERE account_id = ? AND available = 1 AND capability = 'chat'
    ORDER BY model_id`).all(accountId);
  if (!patterns?.length) return rows[0]?.model_id || null;
  for (const pattern of patterns) {
    const match = rows.find((row) => new RegExp(pattern, 'i').test(row.model_id));
    if (match) return match.model_id;
  }
  return null;
}

async function post(url, headers, body) {
  const response = await fetch(url, {
    method: 'POST',
    headers: { 'content-type': 'application/json', ...headers },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(TIMEOUT_MS),
  });
  const text = await response.text().catch(() => '');
  return { status: response.status, headers: response.headers, text };
}

function headerValue(headers, name) {
  return typeof headers.get === 'function' ? headers.get(name) : headers[name];
}

function judgeHeaders(verify, headers) {
  const limit = Number(headerValue(headers, 'x-ratelimit-limit-requests'));
  if (!Number.isFinite(limit) || limit <= 0) return null;
  return limit > verify.maxFreeRequestsPerDay
    ? { state: 'paid', message: `Request limit ${limit}/day is above the free plan (${verify.maxFreeRequestsPerDay}).` }
    : { state: 'free', message: `Request limit ${limit}/day matches the free plan.` };
}

const CHECKS = {
  async ratelimit_headers(runtime, definition, verify) {
    const model = probeModel(runtime.id, verify.probeModels);
    if (!model) return { state: 'unknown', message: 'No model to check the plan with.' };
    const baseUrl = runtime.baseUrl || definition.baseUrl;
    const result = await post(`${baseUrl}/chat/completions`, { Authorization: `Bearer ${runtime.secret}` },
      { model, messages: [{ role: 'user', content: '.' }], max_tokens: 1 });
    return judgeHeaders(verify, result.headers)
      || { state: 'unknown', message: `No plan limits in the response (HTTP ${result.status}).` };
  },

  async paid_model_probe(runtime, definition, verify) {
    const model = probeModel(runtime.id, verify.paidOnlyModels);
    if (!model) return { state: 'unknown', message: 'No model without a free tier to check with.' };
    const baseUrl = runtime.baseUrl || definition.baseUrl;
    const result = await post(`${baseUrl}/models/${encodeURIComponent(model)}:generateContent`, { 'x-goog-api-key': runtime.secret },
      { contents: [{ parts: [{ text: '.' }] }], generationConfig: { maxOutputTokens: 1 } });
    if (result.status === 429 && FREE_TIER_ERROR.test(result.text)) {
      return { state: 'free', message: `${model} is refused with a free-tier limit: no billing account.` };
    }
    if (result.status >= 200 && result.status < 300) {
      return { state: 'paid', message: `${model} answered although it has no free tier: billing is enabled.` };
    }
    return { state: 'unknown', message: `Inconclusive answer from ${model} (HTTP ${result.status}).` };
  },
};

async function verifyAccount(accountId) {
  const runtime = accounts.getRuntime(accountId);
  if (!runtime || runtime.billingMode !== 'free_tier') return null;
  const definition = registry.get(runtime.definitionId);
  const verify = definition?.billing.verify;
  if (!verify) return null;
  try {
    const result = await CHECKS[verify.type](runtime, definition, verify);
    // An inconclusive check keeps a known state instead of flapping to unknown.
    const current = getDatabase().prepare('SELECT billing_state FROM provider_accounts WHERE id = ?').get(accountId)?.billing_state;
    if (result.state === 'unknown' && current && current !== 'unknown') {
      getDatabase().prepare('UPDATE provider_accounts SET billing_message = ? WHERE id = ?').run(result.message, accountId);
      return { state: current, message: result.message };
    }
    setState(accountId, result.state, result.message);
    return result;
  } catch (error) {
    logger.warn('Billing check failed', { accountId, error: error.message });
    return { state: 'unknown', message: error.message };
  }
}

// Passive check on every routed response of providers verified by headers.
function observeHeaders(entry, headers) {
  if (!headers || entry.billingMode !== 'free_tier') return;
  const verify = registry.get(entry.definitionId)?.billing.verify;
  if (verify?.type !== 'ratelimit_headers') return;
  const result = judgeHeaders(verify, headers);
  if (result?.state === 'paid') setState(entry.accountId, 'paid', result.message);
}

// The provider reported a charge for a model routed as free.
function observeCost(entry, cost) {
  if (!entry.isFree || !(cost > 0)) return;
  if (catalog.flagModel(entry.ref, { billedAt: new Date().toISOString() })) {
    logger.warn('Provider charged a model classified as free; it is now treated as paid', { model: entry.id, cost });
  }
}

// "free_tier … limit: 0": this model has no free quota on this account.
function observeFailure(entry, error) {
  if (entry.billingMode !== 'free_tier' || error.learned?.value !== 0 || !FREE_TIER_ERROR.test(error.message || '')) return;
  if (catalog.flagModel(entry.ref, { noFreeTier: true })) {
    logger.info('Model has no free tier on this account', { model: entry.id });
  }
}

module.exports = { verifyAccount, observeHeaders, observeCost, observeFailure, judgeHeaders };
