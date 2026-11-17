'use strict';
const crypto = require('node:crypto');
const { masterKey } = require('../../utils/crypto');
const { getDatabase } = require('../../db/database');
const consent = require('./consent');
const POLICY = require('./policy');

// Separate cryptographic domains per user; selecting a ciphertext with the
// wrong owner cannot decrypt it even if a future query forgets its WHERE clause.
function userKey(userId) { return crypto.createHmac('sha256', masterKey()).update(`local-benchmarks:v1:${userId}`).digest(); }
function fingerprint(userId, value) { return crypto.createHmac('sha256', userKey(userId)).update(JSON.stringify(value)).digest('hex'); }
function encrypt(userId, value) {
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv('aes-256-gcm', userKey(userId), iv);
  cipher.setAAD(Buffer.from(userId));
  const bytes = Buffer.concat([cipher.update(JSON.stringify(value)), cipher.final()]);
  return Buffer.concat([iv, cipher.getAuthTag(), bytes]).toString('base64url');
}
function decrypt(userId, value) {
  const bytes = Buffer.from(value, 'base64url');
  const cipher = crypto.createDecipheriv('aes-256-gcm', userKey(userId), bytes.subarray(0, 12));
  cipher.setAAD(Buffer.from(userId)); cipher.setAuthTag(bytes.subarray(12, 28));
  return JSON.parse(Buffer.concat([cipher.update(bytes.subarray(28)), cipher.final()]).toString('utf8'));
}
function taskFingerprint(userId, canonical) {
  const ids = new Map();
  const messages = canonical.messages.map((message) => {
    const copy = { ...message };
    if (message.tool_calls) copy.tool_calls = message.tool_calls.map((call) => {
      if (!ids.has(call.id)) ids.set(call.id, `call_${ids.size + 1}`);
      return { ...call, id: ids.get(call.id) };
    });
    if (message.tool_call_id) copy.tool_call_id = ids.get(message.tool_call_id) || message.tool_call_id;
    return copy;
  });
  return fingerprint(userId, { ...canonical, messages });
}
function put(userId, keyId, sample, ticket, now = Date.now()) {
  if (!consent.allowed(userId, keyId, ticket)) return false;
  const db = getDatabase();
  const counts = db.prepare(`SELECT COUNT(*) total, SUM(CASE WHEN user_id = ? THEN 1 ELSE 0 END) owned
    FROM benchmark_samples WHERE payload IS NOT NULL`).get(userId);
  if (counts.total >= POLICY.maxSamples || counts.owned >= POLICY.maxSamplesPerUser) return false;
  const hash = taskFingerprint(userId, sample.canonical);
  const expires = now + consent.userSettings(userId).retentionDays * 86400000;
  return !!db.prepare(`INSERT OR IGNORE INTO benchmark_samples
    (id, user_id, key_id, fingerprint, consent_json, payload, next_run_at, created_at, expires_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`).run(crypto.randomUUID(), userId, keyId, hash, JSON.stringify(ticket), encrypt(userId, { sample, progress: {} }), now, now, expires).changes;
}
function checkpoint(row, payload) {
  if (Buffer.byteLength(JSON.stringify(payload)) > POLICY.maxPayloadBytes * 3) { finish(row, 'oversized_checkpoint'); return false; }
  if (!consent.allowed(row.user_id, row.key_id, JSON.parse(row.consent_json))) return false;
  return !!getDatabase().prepare(`UPDATE benchmark_samples SET payload = ?, status = 'pending', attempts = 0, lease_until = 0, next_run_at = ?, code = NULL
    WHERE id = ? AND user_id = ?`).run(encrypt(row.user_id, payload), Date.now(), row.id, row.user_id).changes;
}
function finish(row, code) {
  getDatabase().prepare(`UPDATE benchmark_samples SET payload = NULL, status = ?, code = ?, lease_until = 0 WHERE id = ? AND user_id = ?`)
    .run(code === 'validated' ? 'completed' : 'rejected', code, row.id, row.user_id);
}
function counts(userId = null) {
  const rows = getDatabase().prepare(`SELECT status, COUNT(*) count FROM benchmark_samples ${userId ? 'WHERE user_id = ?' : ''} GROUP BY status`).all(...(userId ? [userId] : []));
  return Object.fromEntries(rows.map((row) => [row.status, row.count]));
}
function purge(userId) {
  consent.cancelUser(userId);
  const db = getDatabase();
  const affected = db.prepare('SELECT DISTINCT canonical_id canonical, task FROM benchmark_contributions WHERE user_id = ?').all(userId);
  db.transaction(() => {
    db.prepare('DELETE FROM benchmark_samples WHERE user_id = ?').run(userId);
    db.prepare('DELETE FROM benchmark_contributions WHERE user_id = ?').run(userId);
  })();
  require('./aggregates').rebuild(Date.now(), affected);
}
function prune(now = Date.now()) {
  const db = getDatabase();
  db.prepare('DELETE FROM benchmark_samples WHERE expires_at <= ?').run(now);
  const cutoff = now - POLICY.evidenceRetentionMs;
  const affected = db.prepare('SELECT DISTINCT canonical_id canonical, task FROM benchmark_contributions WHERE evaluated_at < ?').all(cutoff);
  db.prepare('DELETE FROM benchmark_contributions WHERE evaluated_at < ?').run(cutoff);
  if (affected.length) require('./aggregates').rebuild(now, affected);
}
module.exports = { put, checkpoint, finish, counts, purge, prune, encrypt, decrypt, fingerprint, taskFingerprint };
