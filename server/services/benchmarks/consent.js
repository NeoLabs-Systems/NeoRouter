'use strict';

const { randomUUID } = require('node:crypto');
const { EventEmitter } = require('node:events');
const { getDatabase } = require('../../db/database');
const settings = require('../settings/settings_service');
const { HttpError } = require('../../middleware/error_handler');

const SETTING = 'local_benchmarks';
const OFF = Object.freeze({ enabled: false, revision: '' });
const events = new EventEmitter();
const globalSettings = () => settings.getGlobal(SETTING, OFF);
const userSettings = (userId) => settings.getUserSetting(userId, SETTING, OFF);

function personalKey(key) {
  return key && !key.peer_link_id && (!key.created_by_user_id || key.created_by_user_id === key.user_id) &&
    !key.revoked_at && (!key.expires_at || Date.parse(key.expires_at) > Date.now());
}

function eligibleKey(key) {
  return personalKey(key) && key.pool_access !== 'pool';
}

function ticket(userId, keyId) {
  const admin = globalSettings();
  if (!admin.enabled) return null;
  const owner = userSettings(userId);
  if (!owner.enabled) return null;
  const key = getDatabase().prepare(`SELECT k.*, u.disabled_at FROM router_keys k JOIN users u ON u.id = k.user_id
    WHERE k.id = ? AND k.user_id = ?`).get(keyId, userId);
  if (!eligibleKey(key) || key.disabled_at || !key.benchmark_collection) return null;
  return JSON.stringify([admin.revision, owner.revision, key.benchmark_revision]);
}

function revoke(userId, keyId) {
  events.emit('revoke', { userId, keyId });
}

function saveGlobal(enabled) {
  if (globalSettings().enabled === enabled) return;
  settings.setGlobal(SETTING, { enabled, revision: randomUUID() });
  revoke();
}

function saveUser(userId, enabled) {
  if (enabled && !globalSettings().enabled) throw new HttpError(409, 'BENCHMARK_DISABLED', 'Local benchmarking is disabled by the administrator.');
  if (userSettings(userId).enabled === enabled) return;
  settings.setUserSetting(userId, SETTING, { enabled, revision: randomUUID() });
  revoke(userId);
}

function saveKey(userId, keyId, enabled) {
  const db = getDatabase();
  const key = db.prepare('SELECT * FROM router_keys WHERE id = ? AND user_id = ?').get(keyId, userId);
  if (enabled && (!eligibleKey(key) || !globalSettings().enabled || !userSettings(userId).enabled)) {
    throw new HttpError(409, 'BENCHMARK_CONSENT_REQUIRED', 'Enable benchmarking in your account and use an active personal key without pool access.');
  }
  if (!key || Boolean(key.benchmark_collection) === enabled) return;
  db.prepare('UPDATE router_keys SET benchmark_collection = ?, benchmark_revision = ? WHERE id = ? AND user_id = ?')
    .run(Number(enabled), randomUUID(), keyId, userId);
  revoke(userId, keyId);
}

module.exports = { events, personalKey, eligibleKey, ticket, revoke, globalSettings, userSettings, saveGlobal, saveUser, saveKey };
