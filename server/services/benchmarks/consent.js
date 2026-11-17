'use strict';
const crypto = require('node:crypto');
const { EventEmitter } = require('node:events');
const { getDatabase } = require('../../db/database');
const settings = require('../settings/settings_service');
const { HttpError } = require('../../middleware/error_handler');
const POLICY = require('./policy');
const events = new EventEmitter();
const KEY = 'local_benchmarks';
const globalSettings = () => settings.getGlobal(KEY, { enabled: false, generation: '' });
const userSettings = (id) => settings.getUserSetting(id, KEY, { enabled: false, allowReplay: false, retentionDays: POLICY.retentionDays, generation: '' });

function ticket(userId, keyId) {
  const global = globalSettings();
  if (!global.enabled) return null;
  const user = userSettings(userId);
  if (!user.enabled || !user.allowReplay) return null;
  const row = getDatabase().prepare(`SELECT k.*, u.disabled_at FROM router_keys k JOIN users u ON u.id = k.user_id
    WHERE k.id = ? AND k.user_id = ?`).get(keyId, userId);
  if (!row || row.disabled_at || row.revoked_at || row.peer_link_id || !row.benchmark_collection || (row.expires_at && (!Number.isFinite(Date.parse(row.expires_at)) || Date.parse(row.expires_at) <= Date.now()))) return null;
  return { global: global.generation, user: user.generation, key: row.benchmark_generation };
}
function allowed(userId, keyId, expected = null) {
  const current = ticket(userId, keyId);
  return !!current && (!expected || ['global', 'user', 'key'].every((key) => current[key] === expected[key]));
}
function assertParent(userId) {
  if (!globalSettings().enabled || !userSettings(userId).enabled || !userSettings(userId).allowReplay) {
    throw new HttpError(409, 'BENCHMARK_CONSENT_REQUIRED', 'Enable local benchmarking and replay consent in your account settings first.');
  }
}
function saveGlobal(enabled) {
  if (globalSettings().enabled === enabled) return;
  settings.setGlobal(KEY, { enabled, generation: crypto.randomUUID() });
  // A running evaluator also listens, so revocation aborts its current call.
  events.emit('revoke', {});
  getDatabase().prepare('DELETE FROM benchmark_samples').run();
}
function saveUser(userId, input) {
  if (input.enabled && (!globalSettings().enabled || input.allowReplay !== true)) {
    throw new HttpError(409, 'BENCHMARK_REPLAY_CONSENT_REQUIRED', 'The admin must enable benchmarking, and you must consent to replay to your configured free providers.');
  }
  const saved = userSettings(userId);
  if (input.enabled === saved.enabled && input.allowReplay === saved.allowReplay) {
    const retentionDays = input.retentionDays ?? saved.retentionDays;
    settings.setUserSetting(userId, KEY, { ...saved, retentionDays });
    getDatabase().prepare('UPDATE benchmark_samples SET expires_at = MIN(expires_at, created_at + ?) WHERE user_id = ?').run(retentionDays * 86400000, userId);
    return;
  }
  settings.setUserSetting(userId, KEY, { enabled: input.enabled, allowReplay: input.enabled && input.allowReplay === true,
    retentionDays: input.retentionDays ?? saved.retentionDays, generation: crypto.randomUUID() });
  events.emit('revoke', { userId });
  getDatabase().prepare('DELETE FROM benchmark_samples WHERE user_id = ?').run(userId);
}
function saveKey(userId, keyId, enabled) {
  if (enabled) assertParent(userId);
  const row = getDatabase().prepare('SELECT id FROM router_keys WHERE id = ? AND user_id = ? AND peer_link_id IS NULL').get(keyId, userId);
  if (!row) throw new HttpError(404, 'KEY_NOT_FOUND', 'This router key does not exist.');
  getDatabase().prepare('UPDATE router_keys SET benchmark_collection = ?, benchmark_generation = ? WHERE id = ?').run(enabled ? 1 : 0, crypto.randomUUID(), keyId);
  events.emit('revoke', { userId, keyId });
  getDatabase().prepare('DELETE FROM benchmark_samples WHERE user_id = ? AND key_id = ?').run(userId, keyId);
}
function cancelUser(userId) {
  if (getDatabase().prepare('SELECT 1 FROM users WHERE id = ?').get(userId)) {
    settings.setUserSetting(userId, KEY, { ...userSettings(userId), generation: crypto.randomUUID() });
  }
  events.emit('revoke', { userId });
}
module.exports = { globalSettings, userSettings, ticket, allowed, assertParent, saveGlobal, saveUser, saveKey, cancelUser, events };
