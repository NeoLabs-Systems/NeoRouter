'use strict';

// What a user can do with their own account: take everything with them
// (account data, and separately the provider keys they saved) or delete it.
// Every step asks for the password again and, when 2FA is on, for a code;
// exporting the keys needs 2FA in any case, since they leave the server in
// plain text.
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const { decryptString } = require('../../utils/crypto');
const twoFactor = require('../auth/two_factor_service');
const registry = require('../../providers/registry');
const audit = require('../teams/audit');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('account-data');
const FORMAT = 1;
// Connections NeoRouter makes itself (pool partners); their keys are not the user's.
const INTERNAL_DEFINITIONS = ['neorouter_peer'];

const json = (text, fallback = null) => {
  try { return text ? JSON.parse(text) : fallback; } catch { return fallback; }
};

const rows = (sql, ...params) => getDatabase().prepare(sql).all(...params);

function userRow(userId) {
  const user = getDatabase().prepare('SELECT * FROM users WHERE id = ?').get(userId);
  if (!user) throw new HttpError(404, 'USER_NOT_FOUND', 'This account does not exist.');
  return user;
}

function ownAccounts(userId) {
  return rows(`SELECT * FROM provider_accounts WHERE user_id = ? AND definition_id NOT IN (${INTERNAL_DEFINITIONS.map(() => '?').join(',')})
    ORDER BY created_at`, userId, ...INTERNAL_DEFINITIONS);
}

const providerName = (definitionId) => registry.get(definitionId)?.label || definitionId;

/** Everything stored for this user, without passwords, keys or other secrets. */
async function exportData(userId, credentials) {
  await twoFactor.confirm(userId, credentials);
  const user = userRow(userId);
  const data = {
    format: FORMAT,
    kind: 'neorouter-account',
    exportedAt: new Date().toISOString(),
    profile: {
      id: user.id, username: user.username, email: user.email, displayName: user.display_name, role: user.role,
      createdAt: user.created_at, lastLoginAt: user.last_login_at,
    },
    twoFactor: { enabled: twoFactor.isEnabled(userId) },
    settings: Object.fromEntries(rows('SELECT key, value FROM user_settings WHERE user_id = ?', userId).map((row) => [row.key, json(row.value, row.value)])),
    connections: ownAccounts(userId).map((row) => ({
      id: row.id, provider: providerName(row.definition_id), definitionId: row.definition_id, label: row.label, slug: row.slug,
      baseUrl: row.base_url, options: json(row.options_json, {}), billing: row.billing_mode, prepaid: Boolean(row.prepaid),
      enabled: Boolean(row.enabled), status: row.status, createdAt: row.created_at,
      models: rows('SELECT model_id, display_name, capability, enabled, is_free FROM models WHERE account_id = ? ORDER BY model_id', row.id)
        .map((model) => ({ id: model.model_id, name: model.display_name, capability: model.capability, enabled: Boolean(model.enabled), free: Boolean(model.is_free) })),
    })),
    routingProfiles: rows('SELECT id, name, weights_json, constraints_json, created_at FROM routing_profiles WHERE user_id = ?', userId)
      .map((row) => ({ id: row.id, name: row.name, weights: json(row.weights_json, {}), constraints: json(row.constraints_json, {}), createdAt: row.created_at })),
    // NeoRouter keys are stored only as a hash: listed, but they cannot be exported.
    routerKeys: rows(`SELECT id, name, prefix, mode, budget_usd, budget_period, rpm_limit, allowed_models_json, profile, expires_at, revoked_at,
      last_used_at, created_at, app_name, token_limit, token_period, priority FROM router_keys WHERE user_id = ? ORDER BY created_at`, userId)
      .map(({ allowed_models_json: allowed, ...row }) => ({ ...row, allowedModels: json(allowed, null) })),
    team: {
      memberOf: getDatabase().prepare('SELECT owner_user_id, created_at FROM team_members WHERE member_user_id = ?').get(userId) || null,
      members: rows('SELECT member_user_id, created_at FROM team_members WHERE owner_user_id = ?', userId),
    },
    pools: rows('SELECT pool_id, name, is_host, self_name, share_json, paused, joined_at FROM pools WHERE user_id = ?', userId)
      .map((row) => ({ id: row.pool_id, name: row.name, host: Boolean(row.is_host), myName: row.self_name, share: json(row.share_json, {}), paused: Boolean(row.paused), joinedAt: row.joined_at })),
    // Usage history: which model answered, tokens, cost. Prompts are never stored.
    requests: rows(`SELECT id, created_at, capability, task, requested_model, model_label, status, http_status, error_code, input_tokens, output_tokens,
      units, cost_usd, saved_usd, is_free, latency_ms, stream FROM requests WHERE user_id = ? ORDER BY created_at`, userId),
    mediaJobs: rows('SELECT id, capability, model_label, status, request_json, result_json, created_at FROM media_jobs WHERE user_id = ? ORDER BY created_at', userId)
      .map(({ request_json: request, result_json: result, ...row }) => ({ ...row, request: json(request), result: json(result) })),
  };
  logger.info('Account data exported', { userId });
  return data;
}

/** The provider keys this user saved, in plain text. Needs 2FA. */
async function exportKeys(userId, credentials) {
  await twoFactor.confirm(userId, credentials, { requireFactor: true });
  userRow(userId);
  // Everything needed to add the connection again (Connections → import).
  const keys = ownAccounts(userId).filter((row) => row.secret_encrypted).map((row) => {
    const { headers, ...options } = json(row.options_json, {});
    return {
      provider: providerName(row.definition_id),
      definitionId: row.definition_id,
      label: row.label,
      baseUrl: row.base_url,
      secret: decryptString(row.secret_encrypted),
      billingMode: row.prepaid === 2 ? 'plan_paid' : row.prepaid ? 'plan' : row.billing_mode,
      classification: row.classification || 'auto',
      ...(Object.keys(options).length ? { options } : {}),
      ...(headers ? { headers } : {}),
    };
  });
  audit.record('account.keys_exported', { actorUserId: userId, subjectUserId: userId, detail: { count: keys.length } });
  logger.info('Saved keys exported', { userId, count: keys.length });
  return { format: FORMAT, kind: 'neorouter-keys', exportedAt: new Date().toISOString(), keys };
}

/**
 * Deletes the account and everything that belongs to it. Pools are left
 * first, so partners are told; the last admin cannot go (the server would
 * have nobody to run it).
 */
async function deleteAccount(userId, credentials) {
  await twoFactor.confirm(userId, credentials);
  const user = userRow(userId);
  if (user.role === 'admin' && getDatabase().prepare("SELECT COUNT(*) count FROM users WHERE role = 'admin' AND disabled_at IS NULL").get().count <= 1) {
    throw new HttpError(409, 'LAST_ADMIN', 'You are the only admin. Make someone else an admin first.');
  }
  const pools = require('../pools/pool_service');
  for (const { pool_id: poolId } of rows('SELECT pool_id FROM pools WHERE user_id = ?', userId)) {
    await pools.leave(userId, poolId).catch((error) => logger.warn('Could not tell a pool that the account is gone', { userId, error: error.message }));
  }
  audit.record('account.deleted', { actorUserId: null, subjectUserId: null, detail: { username: user.username } });
  getDatabase().prepare('DELETE FROM users WHERE id = ?').run(userId);
  logger.info('Account deleted by its owner', { userId });
}

module.exports = { exportData, exportKeys, deleteAccount };
