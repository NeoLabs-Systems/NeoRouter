'use strict';

const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const { randomToken, sha256 } = require('../../utils/crypto');
const budget = require('../budget/budget_service');
const profiles = require('../routing/profiles');

const PREFIX = 'nrt';
const rpmWindows = new Map();

function publicKey(row) {
  const spent = budget.spent(row);
  return {
    id: row.id,
    name: row.name,
    prefix: `${PREFIX}_${row.prefix}`,
    mode: row.mode,
    budgetUsd: row.budget_usd,
    budgetPeriod: row.budget_period,
    spentUsd: Number(spent.toFixed(6)),
    remainingUsd: row.budget_usd === null ? null : Number(Math.max(0, row.budget_usd - spent).toFixed(6)),
    periodResetsAt: budget.periodEnd(row.budget_period) ? new Date(budget.periodEnd(row.budget_period)).toISOString() : null,
    rpmLimit: row.rpm_limit,
    allowedModels: JSON.parse(row.allowed_models_json || '[]'),
    profile: row.profile,
    expiresAt: row.expires_at,
    revoked: Boolean(row.revoked_at),
    lastUsedAt: row.last_used_at,
    createdAt: row.created_at,
  };
}

function assertProfile(userId, profile) {
  if (profile && !profiles.resolve(userId, profile)) throw new HttpError(400, 'UNKNOWN_PROFILE', 'This routing profile does not exist.');
}

function create(userId, input) {
  assertProfile(userId, input.profile);
  const id = crypto.randomUUID();
  const prefix = crypto.randomBytes(4).toString('hex');
  const secret = `${PREFIX}_${prefix}_${randomToken(32)}`;
  getDatabase().prepare(`INSERT INTO router_keys
    (id, user_id, name, prefix, key_hash, mode, budget_usd, budget_period, rpm_limit, allowed_models_json, profile, expires_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
    .run(id, userId, input.name, prefix, sha256(secret), input.mode || 'both', input.budgetUsd ?? null,
      input.budgetPeriod || 'total', input.rpmLimit ?? null, JSON.stringify(input.allowedModels || []),
      input.profile || 'balanced', input.expiresAt || null);
  return { key: get(userId, id), secret };
}

function getRow(userId, id) {
  const row = getDatabase().prepare('SELECT * FROM router_keys WHERE id = ? AND user_id = ?').get(id, userId);
  if (!row) throw new HttpError(404, 'KEY_NOT_FOUND', 'This router key does not exist.');
  return row;
}

function get(userId, id) {
  return publicKey(getRow(userId, id));
}

function list(userId) {
  return getDatabase().prepare('SELECT * FROM router_keys WHERE user_id = ? ORDER BY created_at DESC').all(userId).map(publicKey);
}

function update(userId, id, input) {
  const row = getRow(userId, id);
  assertProfile(userId, input.profile);
  getDatabase().prepare(`UPDATE router_keys SET name = ?, mode = ?, budget_usd = ?, budget_period = ?, rpm_limit = ?,
    allowed_models_json = ?, profile = ?, expires_at = ? WHERE id = ?`)
    .run(
      input.name ?? row.name,
      input.mode ?? row.mode,
      input.budgetUsd !== undefined ? input.budgetUsd : row.budget_usd,
      input.budgetPeriod ?? row.budget_period,
      input.rpmLimit !== undefined ? input.rpmLimit : row.rpm_limit,
      input.allowedModels !== undefined ? JSON.stringify(input.allowedModels) : row.allowed_models_json,
      input.profile ?? row.profile,
      input.expiresAt !== undefined ? input.expiresAt : row.expires_at,
      id,
    );
  return get(userId, id);
}

function revoke(userId, id) {
  getRow(userId, id);
  getDatabase().prepare("UPDATE router_keys SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?").run(id);
  return get(userId, id);
}

function remove(userId, id) {
  getRow(userId, id);
  getDatabase().prepare('DELETE FROM router_keys WHERE id = ?').run(id);
}

function authenticate(token) {
  if (!token || !token.startsWith(`${PREFIX}_`)) return null;
  const row = getDatabase().prepare(`SELECT k.*, u.disabled_at user_disabled FROM router_keys k JOIN users u ON u.id = k.user_id
    WHERE k.key_hash = ?`).get(sha256(token));
  if (!row || row.revoked_at || row.user_disabled) return null;
  if (row.expires_at && Date.parse(row.expires_at) <= Date.now()) return null;
  getDatabase().prepare("UPDATE router_keys SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?").run(row.id);
  return row;
}

// Per-key request rate limit (sliding 60 s window).
function consumeRpm(key, now = Date.now()) {
  if (!key.rpm_limit) return { allowed: true };
  const recent = (rpmWindows.get(key.id) || []).filter((time) => now - time < 60_000);
  if (recent.length >= key.rpm_limit) {
    rpmWindows.set(key.id, recent);
    return { allowed: false, retryAfterSeconds: Math.max(1, Math.ceil((recent[0] + 60_000 - now) / 1000)) };
  }
  recent.push(now);
  rpmWindows.set(key.id, recent);
  return { allowed: true };
}

module.exports = { create, list, get, getRow, update, revoke, remove, authenticate, consumeRpm, publicKey, PREFIX };
