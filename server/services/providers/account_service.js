'use strict';

const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const { encryptString, decryptString } = require('../../utils/crypto');
const registry = require('../../providers/registry');
const { hasFreeTier } = require('../catalog/free_classifier');
const { createLogger } = require('../../utils/logger');
const teamMembers = require('../teams/members');

const logger = createLogger('provider-accounts');
const AUTH_FAILURES_BEFORE_DISABLE = 3;

function definitionOrThrow(definitionId) {
  const definition = registry.get(definitionId);
  if (!definition) throw new HttpError(400, 'UNKNOWN_PROVIDER', `Unknown provider "${definitionId}".`);
  return definition;
}

function slugify(value) {
  return String(value).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40) || 'provider';
}

function uniqueSlug(userId, base) {
  const db = getDatabase();
  let slug = slugify(base);
  for (let index = 2; db.prepare('SELECT 1 FROM provider_accounts WHERE user_id = ? AND slug = ?').get(userId, slug); index += 1) {
    slug = `${slugify(base)}-${index}`;
  }
  return slug;
}

function maskSecret(secret) {
  if (!secret) return null;
  return secret.length <= 8 ? '••••' : `${secret.slice(0, 4)}••••${secret.slice(-4)}`;
}

function rowToRuntime(row) {
  const secret = row.secret_encrypted ? decryptString(row.secret_encrypted) : null;
  return {
    id: row.id,
    userId: row.user_id,
    definitionId: row.definition_id,
    slug: row.slug,
    label: row.label,
    baseUrl: row.base_url,
    secret,
    options: JSON.parse(row.options_json || '{}'),
    billingMode: row.billing_mode,
    enabled: Boolean(row.enabled),
    status: row.status,
  };
}

function publicAccount(row) {
  const definition = registry.get(row.definition_id);
  const runtime = rowToRuntime(row);
  const counts = getDatabase().prepare(`SELECT COUNT(*) total, SUM(is_free) free, SUM(enabled) enabled
    FROM models WHERE account_id = ? AND available = 1 AND capability = 'chat'`).get(row.id);
  return {
    id: row.id,
    definitionId: row.definition_id,
    providerLabel: definition?.label || row.definition_id,
    category: definition?.category || null,
    slug: row.slug,
    label: row.label,
    baseUrl: row.base_url || definition?.baseUrl || null,
    keyPreview: maskSecret(runtime.secret),
    options: runtime.options,
    billingMode: row.billing_mode,
    billingState: row.billing_state,
    billingMessage: row.billing_message,
    billingCheckedAt: row.billing_checked_at,
    billingNote: definition?.billing.note || null,
    billingVerifiable: Boolean(definition?.billing.verify),
    enabled: Boolean(row.enabled),
    status: row.status,
    statusMessage: row.status_message,
    lastValidatedAt: row.last_validated_at,
    lastSyncedAt: row.last_synced_at,
    createdAt: row.created_at,
    createdBy: teamMembers.publicUser(row.created_by_user_id),
    models: { total: counts.total || 0, free: counts.free || 0, enabled: counts.enabled || 0 },
  };
}

function getRow(userId, accountId) {
  const row = getDatabase().prepare('SELECT * FROM provider_accounts WHERE id = ? AND user_id = ?').get(accountId, userId);
  if (!row) throw new HttpError(404, 'ACCOUNT_NOT_FOUND', 'This provider account does not exist.');
  return row;
}

function getRuntime(accountId) {
  const row = getDatabase().prepare('SELECT * FROM provider_accounts WHERE id = ?').get(accountId);
  return row ? rowToRuntime(row) : null;
}

function adapterFor(runtime) {
  return registry.createAdapter(definitionOrThrow(runtime.definitionId), runtime);
}

function validateInput(definition, { secret, baseUrl, options = {} }) {
  if (!definition.keyOptional && !secret) throw new HttpError(400, 'KEY_REQUIRED', `${definition.label} requires an API key.`);
  if (definition.editableBaseUrl && !baseUrl && !definition.baseUrl) {
    throw new HttpError(400, 'BASE_URL_REQUIRED', 'A base URL is required for this provider.');
  }
  if (baseUrl) {
    let parsed;
    try { parsed = new URL(baseUrl); } catch { throw new HttpError(400, 'INVALID_BASE_URL', 'The base URL is not a valid URL.'); }
    if (!/^https?:$/.test(parsed.protocol)) throw new HttpError(400, 'INVALID_BASE_URL', 'The base URL must use http or https.');
  }
  for (const field of definition.fields) {
    if (field.required && !String(options[field.key] || '').trim()) {
      throw new HttpError(400, 'FIELD_REQUIRED', `${field.label} is required.`);
    }
  }
}

function recordValidation(accountId, result) {
  const db = getDatabase();
  const row = db.prepare('SELECT auth_failures, enabled FROM provider_accounts WHERE id = ?').get(accountId);
  if (!row) return;
  const failures = result.status === 'invalid' ? row.auth_failures + 1 : 0;
  // Only repeated confirmed rejections disable an account; transient errors
  // never do, so a flaky provider is not silently dropped.
  const status = result.status === 'invalid' && failures >= AUTH_FAILURES_BEFORE_DISABLE ? 'disabled'
    : result.status === 'unknown' ? 'error' : result.status;
  db.prepare(`UPDATE provider_accounts SET status = ?, status_message = ?, auth_failures = ?,
    enabled = CASE WHEN ? = 'disabled' THEN 0 ELSE enabled END,
    last_validated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`)
    .run(status, result.message || null, failures, status, accountId);
  if (status === 'disabled') logger.warn('Provider account disabled after repeated auth failures', { accountId });
}

async function validateAccount(accountId) {
  const runtime = getRuntime(accountId);
  if (!runtime) return null;
  const result = await adapterFor(runtime).validate();
  recordValidation(accountId, result);
  return result;
}

async function create(userId, input, createdBy = userId) {
  const definition = definitionOrThrow(input.definitionId);
  const baseUrl = definition.editableBaseUrl ? (input.baseUrl || definition.baseUrl || null) : null;
  const options = { ...(input.options || {}) };
  if (input.headers) options.headers = input.headers;
  validateInput(definition, { secret: input.secret, baseUrl, options });
  const billingMode = input.billingMode && (input.billingMode === 'paid' || hasFreeTier(definition.billing))
    ? input.billingMode : definition.billing.defaultMode;

  const probe = { secret: input.secret || null, baseUrl, options };
  const result = await registry.createAdapter(definition, probe).validate();
  if (result.status === 'invalid') {
    throw new HttpError(422, 'KEY_REJECTED', `${definition.label} rejected this key: ${result.message}`);
  }
  const id = crypto.randomUUID();
  const slug = uniqueSlug(userId, input.slug || definition.id);
  getDatabase().prepare(`INSERT INTO provider_accounts
    (id, user_id, definition_id, slug, label, base_url, secret_encrypted, options_json, billing_mode, status, status_message, last_validated_at, created_by_user_id)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'), ?)`)
    .run(id, userId, definition.id, slug, input.label || definition.label, baseUrl,
      input.secret ? encryptString(input.secret) : null, JSON.stringify(options), billingMode,
      result.status === 'healthy' ? 'healthy' : 'error', result.message || null, createdBy);
  return { account: publicAccount(getRow(userId, id)), validation: result };
}

async function update(userId, accountId, input) {
  const row = getRow(userId, accountId);
  const definition = definitionOrThrow(row.definition_id);
  const db = getDatabase();
  const runtime = rowToRuntime(row);
  const next = {
    secret: input.secret !== undefined ? input.secret || null : runtime.secret,
    baseUrl: definition.editableBaseUrl && input.baseUrl !== undefined ? input.baseUrl : runtime.baseUrl,
    // Options are replaced as a whole so cleared fields disappear; headers
    // are kept unless the caller sends them too.
    options: input.options !== undefined ? { ...input.options, ...(runtime.options.headers ? { headers: runtime.options.headers } : {}) } : runtime.options,
  };
  if (input.headers !== undefined) next.options.headers = input.headers;
  const credentialsChanged = input.secret !== undefined || input.baseUrl !== undefined || input.options !== undefined;
  if (credentialsChanged) {
    validateInput(definition, next);
    const result = await registry.createAdapter(definition, next).validate();
    if (result.status === 'invalid') throw new HttpError(422, 'KEY_REJECTED', `${definition.label} rejected this key: ${result.message}`);
  }
  if (input.billingMode === 'free_tier' && !hasFreeTier(definition.billing)) {
    throw new HttpError(400, 'NO_FREE_TIER', `${definition.label} has no free tier.`);
  }
  if (input.slug !== undefined && slugify(input.slug) !== row.slug) {
    const slug = slugify(input.slug);
    if (db.prepare('SELECT 1 FROM provider_accounts WHERE user_id = ? AND slug = ? AND id != ?').get(userId, slug, accountId)) {
      throw new HttpError(409, 'SLUG_TAKEN', 'Another provider already uses this name in model ids.');
    }
    db.prepare('UPDATE provider_accounts SET slug = ? WHERE id = ?').run(slug, accountId);
  }
  db.prepare(`UPDATE provider_accounts SET label = ?, base_url = ?, secret_encrypted = ?, options_json = ?, billing_mode = ?,
    enabled = ?, auth_failures = CASE WHEN ? THEN 0 ELSE auth_failures END,
    billing_state = CASE WHEN ? THEN 'unknown' ELSE billing_state END,
    status = CASE WHEN ? THEN 'unknown' ELSE status END WHERE id = ?`)
    .run(
      input.label ?? row.label,
      next.baseUrl,
      next.secret ? encryptString(next.secret) : null,
      JSON.stringify(next.options),
      input.billingMode ?? row.billing_mode,
      input.enabled === undefined ? row.enabled : (input.enabled ? 1 : 0),
      credentialsChanged || input.enabled === true ? 1 : 0,
      credentialsChanged || (input.billingMode !== undefined && input.billingMode !== row.billing_mode) ? 1 : 0,
      credentialsChanged || (input.enabled === true && row.status === 'disabled') ? 1 : 0,
      accountId,
    );
  return publicAccount(getRow(userId, accountId));
}

// Checks credentials and lists models without saving anything, so the UI
// can preview what an endpoint offers before the account is created.
async function probe(input) {
  const definition = definitionOrThrow(input.definitionId);
  const baseUrl = definition.editableBaseUrl ? (input.baseUrl || definition.baseUrl || null) : null;
  const options = { ...(input.options || {}) };
  if (input.headers) options.headers = input.headers;
  validateInput(definition, { secret: input.secret, baseUrl, options });
  const adapter = registry.createAdapter(definition, { secret: input.secret || null, baseUrl, options });
  const validation = await adapter.validate();
  let models = [];
  let modelsError = null;
  try {
    models = (await adapter.listModels()).map((model) => ({ id: model.id, name: model.name, kind: model.kind, freeHint: model.freeHint, contextWindow: model.contextWindow }));
  } catch (error) {
    modelsError = error.message;
  }
  return { validation, models, modelsError };
}

function remove(userId, accountId) {
  getRow(userId, accountId);
  getDatabase().prepare('DELETE FROM provider_accounts WHERE id = ?').run(accountId);
}

function list(userId) {
  return getDatabase().prepare('SELECT * FROM provider_accounts WHERE user_id = ? ORDER BY created_at').all(userId).map(publicAccount);
}

function get(userId, accountId) {
  return publicAccount(getRow(userId, accountId));
}

function allEnabledIds() {
  return getDatabase().prepare('SELECT id FROM provider_accounts WHERE enabled = 1').all().map((row) => row.id);
}

module.exports = {
  create,
  probe,
  update,
  remove,
  list,
  get,
  getRow,
  getRuntime,
  adapterFor,
  validateAccount,
  recordValidation,
  allEnabledIds,
  AUTH_FAILURES_BEFORE_DISABLE,
};
