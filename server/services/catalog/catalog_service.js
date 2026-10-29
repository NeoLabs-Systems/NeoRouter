'use strict';

const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const registry = require('../../providers/registry');
const accounts = require('../providers/account_service');
const metadata = require('./metadata_service');
const { classify } = require('./free_classifier');
const { canonicalId } = require('./model_identity');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('catalog');
const flag = (value) => (value === null || value === undefined ? null : value ? 1 : 0);

// Pulls the live model list from the provider and reconciles the catalog.
// Models that disappear are kept (history, stats) but marked unavailable.
async function syncAccount(accountId) {
  const runtime = accounts.getRuntime(accountId);
  if (!runtime) return null;
  const definition = registry.get(runtime.definitionId);
  const adapter = accounts.adapterFor(runtime);
  const listed = await adapter.listModels();
  const db = getDatabase();
  const seen = new Set();
  const upsert = db.prepare(`INSERT INTO models
    (account_id, model_id, display_name, canonical_id, capability, context_window, max_output, supports_tools, supports_vision,
     supports_json, supports_reasoning, price_in, price_out, price_source, is_free, free_reason, available, raw_json, price_unit, price_per_unit, last_seen_at)
    VALUES (@account_id, @model_id, @display_name, @canonical_id, @capability, @context_window, @max_output, @supports_tools,
     @supports_vision, @supports_json, @supports_reasoning, @price_in, @price_out, @price_source, @is_free, @free_reason, 1, @raw_json,
     @price_unit, @price_per_unit,
     strftime('%Y-%m-%dT%H:%M:%fZ','now'))
    ON CONFLICT(account_id, model_id) DO UPDATE SET display_name = excluded.display_name, canonical_id = excluded.canonical_id,
      capability = excluded.capability, context_window = excluded.context_window, max_output = excluded.max_output,
      supports_tools = excluded.supports_tools, supports_vision = excluded.supports_vision, supports_json = excluded.supports_json,
      supports_reasoning = excluded.supports_reasoning, price_in = excluded.price_in, price_out = excluded.price_out,
      price_source = excluded.price_source, raw_json = excluded.raw_json, price_unit = excluded.price_unit,
      price_per_unit = excluded.price_per_unit, available = 1, last_seen_at = excluded.last_seen_at`);
  db.transaction(() => {
    for (const model of listed) {
      if (!model.id || seen.has(model.id)) continue;
      seen.add(model.id);
      const facts = metadata.providerFacts(runtime.definitionId, model.id) || {};
      const priceFromFacts = !model.priceKnown && Number.isFinite(facts.priceIn) && Number.isFinite(facts.priceOut) && facts.priceIn >= 0 && facts.priceOut >= 0;
      upsert.run({
        account_id: accountId,
        model_id: model.id,
        display_name: model.name || facts.name || model.id,
        canonical_id: canonicalId(model.id),
        capability: model.kind === 'chat' && facts.textOutput !== false ? 'chat' : model.kind,
        context_window: model.contextWindow || facts.contextWindow || null,
        max_output: model.maxOutput || facts.maxOutput || null,
        supports_tools: definition.quirks.noTools ? 0 : flag(model.tools ?? facts.tools),
        supports_vision: flag(model.vision ?? facts.vision),
        supports_json: flag(model.json),
        supports_reasoning: flag(model.reasoning ?? facts.reasoning),
        price_in: model.priceKnown ? model.priceIn : priceFromFacts ? facts.priceIn : null,
        price_out: model.priceKnown ? model.priceOut : priceFromFacts ? facts.priceOut : null,
        price_source: model.priceKnown ? 'listing' : priceFromFacts ? 'models.dev' : null,
        is_free: 0,
        free_reason: null,
        raw_json: JSON.stringify({ freeHint: model.freeHint, priceKnown: model.priceKnown, ...(model.decision ? { decision: true } : {}) }),
        price_unit: model.priceUnit || null,
        price_per_unit: model.unitPrice ?? null,
      });
    }
    const existing = db.prepare('SELECT id, model_id FROM models WHERE account_id = ?').all(accountId);
    const markGone = db.prepare('UPDATE models SET available = 0 WHERE id = ?');
    for (const row of existing) if (!seen.has(row.model_id)) markGone.run(row.id);
  })();
  reclassifyAccount(accountId);
  for (const canonical of new Set([...seen].map(canonicalId))) metadata.recomputeCanonical(canonical);
  db.prepare("UPDATE provider_accounts SET last_synced_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?").run(accountId);
  logger.info('Synced provider models', { accountId, models: seen.size });
  return { models: seen.size };
}

// Free status depends on the account billing mode and overrides, so it is
// recomputed whenever either changes, not only on sync.
function reclassifyAccount(accountId) {
  const db = getDatabase();
  const account = db.prepare('SELECT definition_id, billing_mode, billing_state, base_url, secret_encrypted FROM provider_accounts WHERE id = ?').get(accountId);
  if (!account) return;
  const definition = registry.get(account.definition_id);
  const rows = db.prepare('SELECT id, model_id, raw_json, overrides_json, price_in, price_out, price_source FROM models WHERE account_id = ?').all(accountId);
  const update = db.prepare('UPDATE models SET is_free = ?, free_reason = ? WHERE id = ?');
  db.transaction(() => {
    for (const row of rows) {
      const listing = JSON.parse(row.raw_json || '{}');
      const overrides = JSON.parse(row.overrides_json || '{}');
      const result = classify({
        definition,
        billingMode: account.billing_mode,
        billingState: account.billing_state,
        baseUrl: account.base_url,
        hasSecret: Boolean(account.secret_encrypted),
        modelId: row.model_id,
        listing: { ...listing, priceKnown: listing.priceKnown || row.price_source !== null || overrides.priceIn !== undefined },
        overrides,
      });
      update.run(result.isFree ? 1 : 0, result.reason, row.id);
    }
  })();
}

// Records evidence from real traffic on a model (e.g. { billedAt } or
// { noFreeTier }) and reclassifies its account. Returns false when the flag
// was already set, so callers log each finding once.
function flagModel(modelRef, flags) {
  const db = getDatabase();
  const row = db.prepare('SELECT account_id, overrides_json FROM models WHERE id = ?').get(modelRef);
  if (!row) return false;
  const overrides = JSON.parse(row.overrides_json || '{}');
  if (Object.keys(flags).every((key) => overrides[key])) return false;
  db.prepare('UPDATE models SET overrides_json = ? WHERE id = ?').run(JSON.stringify({ ...overrides, ...flags }), modelRef);
  reclassifyAccount(row.account_id);
  return true;
}

// Models per capability in the user's catalog (chat, embedding, image, …),
// so the UI can show kinds that are not routable yet.
function capabilitySummary(userId) {
  return getDatabase().prepare(`SELECT m.capability, COUNT(*) total, SUM(m.is_free) free FROM models m
    JOIN provider_accounts a ON a.id = m.account_id WHERE a.user_id = ? AND m.available = 1 AND a.enabled = 1
    GROUP BY m.capability ORDER BY total DESC`).all(userId).map((row) => ({ id: row.capability, total: row.total, free: row.free || 0 }));
}

const OVERRIDE_KEYS = ['free', 'billedAt', 'noFreeTier', 'priceUnit', 'unitPrice', 'priceIn', 'priceOut', 'contextWindow', 'tools', 'vision', 'intelligence'];

function updateModel(userId, modelRef, { enabled, overrides }) {
  const db = getDatabase();
  const row = db.prepare(`SELECT m.* FROM models m JOIN provider_accounts a ON a.id = m.account_id
    WHERE m.id = ? AND a.user_id = ?`).get(modelRef, userId);
  if (!row) throw new HttpError(404, 'MODEL_NOT_FOUND', 'This model does not exist.');
  let nextOverrides = JSON.parse(row.overrides_json || '{}');
  if (overrides) {
    for (const key of OVERRIDE_KEYS) {
      if (!(key in overrides)) continue;
      if (overrides[key] === null) delete nextOverrides[key];
      else nextOverrides[key] = overrides[key];
    }
  }
  db.prepare('UPDATE models SET enabled = ?, overrides_json = ? WHERE id = ?')
    .run(enabled === undefined ? row.enabled : enabled ? 1 : 0, JSON.stringify(nextOverrides), modelRef);
  reclassifyAccount(row.account_id);
  return getModel(userId, modelRef);
}

function bulkSetEnabled(userId, { accountId, enabled, onlyFree }) {
  const db = getDatabase();
  const result = db.prepare(`UPDATE models SET enabled = ? WHERE account_id IN
    (SELECT id FROM provider_accounts WHERE user_id = ? AND (? IS NULL OR id = ?)) AND (? = 0 OR is_free = 1)`)
    .run(enabled ? 1 : 0, userId, accountId || null, accountId || null, onlyFree ? 1 : 0);
  return { updated: result.changes };
}

// One row per model with everything routing needs. Overrides win over
// provider data; provider data wins over canonical metadata.
const SELECT_ENTRY = `SELECT m.*, a.slug, a.user_id, a.definition_id, a.billing_mode, a.enabled account_enabled, a.status account_status, a.prepaid account_prepaid,
    md.intelligence, md.intelligence_source, md.intelligence_confidence, md.speed_tps, md.context_window md_context,
    md.supports_tools md_tools, md.supports_vision md_vision, md.supports_reasoning md_reasoning, md.release_date, md.params_b, md.skills_json,
    s.successes, s.failures, s.decayed_at, s.latency_ms, s.ttft_ms, s.tps, s.samples, s.penalty, s.penalty_at,
    s.cooldown_until, s.cooldown_reason, s.breaker_open_until, s.last_error, s.last_success_at, s.last_failure_at
  FROM models m
  JOIN provider_accounts a ON a.id = m.account_id
  LEFT JOIN model_metadata md ON md.canonical_id = m.canonical_id
  LEFT JOIN model_stats s ON s.model_ref = m.id`;

function toEntry(row) {
  const overrides = JSON.parse(row.overrides_json || '{}');
  const pick = (override, own, fallback) => (override !== undefined ? override : own !== null && own !== undefined ? own : fallback);
  const bool = (value) => (value === null || value === undefined ? null : Boolean(value));
  return {
    ref: row.id,
    id: `${row.slug}/${row.model_id}`,
    modelId: row.model_id,
    name: row.display_name || row.model_id,
    canonicalId: row.canonical_id,
    capability: row.capability,
    accountId: row.account_id,
    userId: row.user_id,
    providerSlug: row.slug,
    definitionId: row.definition_id,
    billingMode: row.billing_mode,
    enabled: Boolean(row.enabled),
    available: Boolean(row.available),
    accountEnabled: Boolean(row.account_enabled),
    accountStatus: row.account_status,
    contextWindow: pick(overrides.contextWindow, row.context_window, row.md_context),
    maxOutput: row.max_output,
    tools: bool(pick(overrides.tools, row.supports_tools, row.md_tools)),
    vision: bool(pick(overrides.vision, row.supports_vision, row.md_vision)),
    json: bool(row.supports_json),
    reasoning: bool(pick(undefined, row.supports_reasoning, row.md_reasoning)),
    priceIn: pick(overrides.priceIn, row.price_in, null),
    priceOut: pick(overrides.priceOut, row.price_out, null),
    priceSource: overrides.priceIn !== undefined ? 'manual' : row.price_source,
    // Media models: USD per image, second (video), minute (audio in) or
    // million characters (speech).
    priceUnit: pick(overrides.priceUnit, row.price_unit, null),
    unitPrice: pick(overrides.unitPrice, row.price_per_unit, null),
    isFree: Boolean(row.is_free),
    // Answers typed questions (decision model) rather than classifying text.
    decision: row.capability === 'moderation' && JSON.parse(row.raw_json || '{}').decision === true,
    // Included in a prepaid plan: no extra cost per request.
    isPlan: !row.is_free && Boolean(row.account_prepaid),
    // After the plan is used up: billed per use (true) or unavailable.
    planOverflow: row.account_prepaid === 2,
    freeReason: row.free_reason,
    intelligence: pick(overrides.intelligence, row.intelligence, null),
    // Score per skill (coding, math, …) from benchmarks; null when unknown.
    skills: row.skills_json ? JSON.parse(row.skills_json) : null,
    intelligenceSource: overrides.intelligence !== undefined ? 'manual' : row.intelligence_source,
    intelligenceConfidence: row.intelligence_confidence,
    speedPriorTps: row.speed_tps,
    releaseDate: row.release_date,
    paramsB: row.params_b,
    overrides,
    stats: {
      successes: row.successes || 0,
      failures: row.failures || 0,
      decayedAt: row.decayed_at || 0,
      latencyMs: row.latency_ms,
      ttftMs: row.ttft_ms,
      tps: row.tps,
      samples: row.samples || 0,
      penalty: row.penalty || 0,
      penaltyAt: row.penalty_at || 0,
      cooldownUntil: row.cooldown_until || 0,
      cooldownReason: row.cooldown_reason,
      breakerOpenUntil: row.breaker_open_until || 0,
      lastError: row.last_error,
      lastSuccessAt: row.last_success_at,
      lastFailureAt: row.last_failure_at,
    },
  };
}

function listForUser(userId, { capability = 'chat', includeUnavailable = false } = {}) {
  return getDatabase().prepare(`${SELECT_ENTRY} WHERE a.user_id = ? AND m.capability = ? ${includeUnavailable ? '' : 'AND m.available = 1'}
    ORDER BY a.slug, m.model_id`).all(userId, capability).map(toEntry);
}

function getModel(userId, modelRef) {
  const row = getDatabase().prepare(`${SELECT_ENTRY} WHERE m.id = ? AND a.user_id = ?`).get(modelRef, userId);
  if (!row) throw new HttpError(404, 'MODEL_NOT_FOUND', 'This model does not exist.');
  return toEntry(row);
}

module.exports = { syncAccount, reclassifyAccount, flagModel, capabilitySummary, updateModel, bulkSetEnabled, listForUser, getModel, toEntry };
