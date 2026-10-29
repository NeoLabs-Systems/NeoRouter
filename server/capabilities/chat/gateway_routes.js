'use strict';

const express = require('express');
const { requireRouterKey } = require('../../middleware/router_key_auth');
const { execute } = require('./executor');
const catalog = require('../../services/catalog/catalog_service');
const accounts = require('../../services/providers/account_service');
const { allowedByKey, kindAllowed, modelKind, MODEL_KINDS } = require('../../services/routing/router');
const profiles = require('../../services/routing/profiles');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('gateway');
const router = express.Router();
const asyncRoute = (handler) => (req, res, next) => Promise.resolve(handler(req, res)).catch(next);

const ANTHROPIC_ERROR_TYPES = { 400: 'invalid_request_error', 401: 'authentication_error', 402: 'billing_error', 403: 'permission_error', 404: 'not_found_error', 429: 'rate_limit_error', 529: 'overloaded_error' };

// Errors use the dialect of the endpoint so SDKs surface them properly.
function gatewayErrorHandler(format) {
  return (error, req, res, _next) => {
    const status = error.status || 500;
    if (status >= 500 && !error.code) logger.error('Gateway request failed', { requestId: req.id, error });
    const message = status >= 500 && !error.code ? 'An internal error occurred.' : error.message;
    if (res.headersSent) return res.end();
    if (format === 'anthropic_messages') {
      return res.status(status).json({ type: 'error', error: { type: ANTHROPIC_ERROR_TYPES[status] || 'api_error', message }, request_id: req.id });
    }
    return res.status(status).json({
      error: {
        message,
        type: status === 401 ? 'invalid_request_error' : status === 429 ? 'rate_limit_error' : status >= 500 ? 'server_error' : 'invalid_request_error',
        code: error.code || null,
        ...(error.details ? { details: error.details } : {}),
      },
    });
  };
}

const KINDS = new Set(require('../../core/capability_registry').list().map((item) => item.id));

function modelsVisibleTo(key, capability = 'chat') {
  return catalog.listForUser(key.user_id, { capability })
    .filter((entry) => entry.enabled && entry.accountEnabled && allowedByKey(entry, key) && !kindAllowed(entry, key));
}

// Models of this kind the key's free/plan/paid choice leaves out, so an app
// can tell "nothing there" from "not allowed for this key".
// System One lists count decision models apart from classifiers.
function modelsHiddenFrom(key, capability) {
  const hidden = { free: 0, plan: 0, paid: 0 };
  const decisions = { free: 0, plan: 0, paid: 0 };
  for (const entry of catalog.listForUser(key.user_id, { capability })) {
    if (!entry.enabled || !entry.accountEnabled || !allowedByKey(entry, key) || !kindAllowed(entry, key)) continue;
    const kind = entry.isFree ? 'free' : entry.isPlan ? 'plan' : 'paid';
    hidden[kind] += 1;
    if (entry.decision) decisions[kind] += 1;
  }
  return capability === 'moderation' ? { ...hidden, decision_models: decisions } : hidden;
}

// openai_live, openai_realtime or gemini_live (capabilities/live/dialects.js).
function liveProtocols(entry) {
  const runtime = accounts.getRuntime(entry.accountId);
  return runtime ? accounts.adapterFor(runtime).definition.live : [];
}

// "auto" and one entry per routing profile, over every kind of model the key
// may use. With more than one kind in reach, the same again limited to each
// kind ("auto-free:fast", "auto-plan", "auto-paid:best"), so an app can ask
// for, say, only what a subscription already covers. Kinds the key may not
// use, or that have no models of this type, get no entries.
const KIND_LABELS = { free: 'free models', plan: 'subscriptions', paid: 'paid models' };

// Live autos list the protocols their models speak (each live endpoint
// routes only over its own protocol); System One autos say whether they can
// answer /v1/systemone, which needs decision models.
function autoModels(key, entries, type) {
  const kinds = MODEL_KINDS.filter((item) => entries.some((entry) => modelKind(entry) === item));
  const scopes = kinds.length > 1 ? [null, ...kinds] : [null];
  const list = [];
  for (const scope of scopes) {
    const inScope = scope ? entries.filter((entry) => modelKind(entry) === scope) : entries;
    const extra = type === 'live'
      ? { protocols: [...new Set(inScope.flatMap(liveProtocols))] }
      : type === 'moderation' ? { decision: inScope.some((entry) => entry.decision) } : {};
    const base = scope ? `auto-${scope}` : 'auto';
    const only = scope ? `, ${KIND_LABELS[scope]} only` : '';
    // Plain auto follows the key's own profile; every profile is also listed by name.
    const keyProfile = profiles.resolve(key.user_id, key.profile) || profiles.resolve(key.user_id, 'balanced');
    list.push({ id: base, name: `Auto (key default: ${keyProfile.name}${only})`, neorouter: { auto: true, profile: keyProfile.id, keyDefault: true, kinds: scope ? [scope] : kinds, ...extra } });
    for (const profile of profiles.list(key.user_id)) {
      list.push({
        id: `${base}:${profile.id}`,
        name: `Auto (${profile.name}${only})`,
        neorouter: { auto: true, profile: profile.id, kinds: scope ? [scope] : kinds, ...extra },
      });
    }
  }
  return list;
}

router.get('/models', requireRouterKey, (req, res) => {
  const key = req.routerKey;
  // ?type=embedding (or image, speech, transcription, moderation, video) lists another kind.
  const kind = KINDS.has(req.query.type) ? req.query.type : 'chat';
  const entries = modelsVisibleTo(key, kind);
  const created = Math.floor(Date.now() / 1000);
  const autos = autoModels(key, entries, kind);
  if (req.get('anthropic-version')) {
    const data = [...autos.map((auto) => ({ type: 'model', id: auto.id, display_name: auto.name, created_at: new Date().toISOString() })),
      ...entries.map((entry) => ({ type: 'model', id: entry.id, display_name: entry.name, created_at: new Date().toISOString() }))];
    return res.json({ data, has_more: false, first_id: data[0]?.id || null, last_id: data[data.length - 1]?.id || null });
  }
  return res.json({
    object: 'list',
    data: [
      ...autos.map((auto) => ({ id: auto.id, object: 'model', created, owned_by: 'neorouter', name: auto.name, neorouter: auto.neorouter })),
      ...entries.map((entry) => ({
        id: entry.id,
        object: 'model',
        created,
        owned_by: entry.providerSlug,
        name: entry.name,
        context_length: entry.contextWindow,
        pricing: entry.isFree || entry.isPlan ? { prompt: '0', completion: '0' } : { prompt: entry.priceIn === null ? null : String(entry.priceIn / 1e6), completion: entry.priceOut === null ? null : String(entry.priceOut / 1e6) },
        neorouter: {
          kind, free: entry.isFree, plan: entry.isPlan, intelligence: entry.intelligence, tools: entry.tools, vision: entry.vision,
          ...(entry.priceUnit ? { unit_price: entry.unitPrice, price_unit: entry.priceUnit } : {}),
          ...(kind === 'live' ? { protocols: liveProtocols(entry) } : {}),
          ...(kind === 'moderation' ? { decision: entry.decision } : {}),
        },
      })),
    ],
    neorouter: { not_allowed_for_key: modelsHiddenFrom(key, kind) },
  });
});

router.get('/models/:model(*)', requireRouterKey, (req, res, next) => {
  const entry = modelsVisibleTo(req.routerKey).find((item) => item.id === req.params.model);
  if (!entry) return gatewayErrorHandler('openai_chat')(Object.assign(new Error(`Model ${req.params.model} not found.`), { status: 404, code: 'MODEL_NOT_FOUND' }), req, res, next);
  return res.json({ id: entry.id, object: 'model', created: Math.floor(Date.now() / 1000), owned_by: entry.providerSlug });
});

router.post('/chat/completions', requireRouterKey, asyncRoute((req, res) => execute({ req, res, key: req.routerKey, format: 'openai_chat', body: req.body })), gatewayErrorHandler('openai_chat'));
router.post('/messages', requireRouterKey, asyncRoute((req, res) => execute({ req, res, key: req.routerKey, format: 'anthropic_messages', body: req.body })), gatewayErrorHandler('anthropic_messages'));
router.post('/responses', requireRouterKey, asyncRoute((req, res) => execute({ req, res, key: req.routerKey, format: 'openai_responses', body: req.body })), gatewayErrorHandler('openai_responses'));

// Anthropic SDKs call count_tokens before large requests; an estimate is
// better than failing the call.
router.post('/messages/count_tokens', requireRouterKey, (req, res) => {
  const { toCanonical } = require('./translators/anthropic_messages');
  const { estimateInputTokens } = require('./canonical');
  res.json({ input_tokens: estimateInputTokens(toCanonical(req.body || {})) });
});

router.use(gatewayErrorHandler('openai_chat'));

module.exports = router;
module.exports.gatewayErrorHandler = gatewayErrorHandler;
