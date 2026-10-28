'use strict';

const crypto = require('node:crypto');
const bcrypt = require('bcrypt');
const { generateSecret, generateURI, verifySync } = require('otplib');
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const { encryptString, decryptString, verifyPassword } = require('../../utils/crypto');

// TOTP two-factor authentication, following NeoAgent: an authenticator app
// code (6 digits, 30 s, one step of clock drift) or a one-time recovery code.
const TOTP_OPTIONS = Object.freeze({ strategy: 'totp', digits: 6, period: 30, epochTolerance: 30 });
const RECOVERY_ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
const RECOVERY_CODE_COUNT = 10;
const MAX_FAILED_ATTEMPTS = 5;
const LOCKOUT_MS = 5 * 60 * 1000;
const CHALLENGE_TTL_MS = 10 * 60 * 1000;
const ISSUER = 'NeoRouter';

// Login challenges between the password step and the code step.
const challenges = new Map();

const normalizeCode = (value) => String(value || '').trim().replace(/\s+/g, '').toUpperCase();
const normalizeRecoveryCode = (value) => normalizeCode(value).replace(/-/g, '');

function generateRecoveryCode() {
  let raw = '';
  while (raw.length < 10) raw += RECOVERY_ALPHABET[crypto.randomInt(RECOVERY_ALPHABET.length)];
  return `${raw.slice(0, 5)}-${raw.slice(5)}`;
}

function row(userId) {
  return getDatabase().prepare('SELECT * FROM user_two_factor WHERE user_id = ?').get(userId) || null;
}

function status(userId) {
  const factor = row(userId);
  const remaining = getDatabase().prepare('SELECT COUNT(*) count FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL').get(userId).count;
  return { enabled: factor?.enabled === 1, pending: Boolean(factor?.pending_secret_encrypted), enabledAt: factor?.enabled_at || null, recoveryCodesRemaining: remaining };
}

function isEnabled(userId) {
  return row(userId)?.enabled === 1;
}

async function assertPassword(userId, password) {
  const user = getDatabase().prepare('SELECT password_hash FROM users WHERE id = ?').get(userId);
  if (!user || !(await verifyPassword(String(password || ''), user.password_hash))) {
    throw new HttpError(401, 'INVALID_PASSWORD', 'The current password is incorrect.');
  }
}

function verifyTotp(secret, code) {
  const token = normalizeCode(code);
  if (!/^\d{6}$/.test(token)) return false;
  return verifySync({ ...TOTP_OPTIONS, secret, token })?.valid === true;
}

async function storeRecoveryCodes(userId) {
  const codes = Array.from({ length: RECOVERY_CODE_COUNT }, generateRecoveryCode);
  const hashes = await Promise.all(codes.map((code) => bcrypt.hash(normalizeRecoveryCode(code), 10)));
  const db = getDatabase();
  db.transaction(() => {
    db.prepare('DELETE FROM user_recovery_codes WHERE user_id = ?').run(userId);
    const insert = db.prepare('INSERT INTO user_recovery_codes (user_id, code_hash) VALUES (?, ?)');
    for (const hash of hashes) insert.run(userId, hash);
  })();
  return codes;
}

async function consumeRecoveryCode(userId, code) {
  const normalized = normalizeRecoveryCode(code);
  if (normalized.length < 6) return false;
  const db = getDatabase();
  for (const entry of db.prepare('SELECT id, code_hash FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL ORDER BY id').all(userId)) {
    if (await bcrypt.compare(normalized, entry.code_hash)) {
      db.prepare("UPDATE user_recovery_codes SET used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?").run(entry.id);
      return true;
    }
  }
  return false;
}

// Checks an authenticator or recovery code, with a short lockout after
// repeated failures so codes cannot be brute-forced.
async function verifyCode(userId, code, now = Date.now()) {
  const factor = row(userId);
  if (!factor || factor.enabled !== 1) return true;
  if (factor.locked_until > now) throw new HttpError(429, 'TWO_FACTOR_LOCKED', 'Too many wrong codes. Try again in a few minutes.');
  const db = getDatabase();
  const valid = verifyTotp(decryptString(factor.secret_encrypted), code) || await consumeRecoveryCode(userId, code);
  if (valid) {
    db.prepare('UPDATE user_two_factor SET failed_attempts = 0, locked_until = 0 WHERE user_id = ?').run(userId);
    return true;
  }
  const attempts = factor.failed_attempts + 1;
  db.prepare('UPDATE user_two_factor SET failed_attempts = ?, locked_until = ? WHERE user_id = ?')
    .run(attempts >= MAX_FAILED_ATTEMPTS ? 0 : attempts, attempts >= MAX_FAILED_ATTEMPTS ? now + LOCKOUT_MS : 0, userId);
  return false;
}

async function beginSetup(userId, currentPassword) {
  await assertPassword(userId, currentPassword);
  const user = getDatabase().prepare('SELECT username, email FROM users WHERE id = ?').get(userId);
  const secret = generateSecret();
  const otpauthUrl = generateURI({ ...TOTP_OPTIONS, issuer: ISSUER, label: user.email || user.username, secret });
  getDatabase().prepare(`INSERT INTO user_two_factor (user_id, pending_secret_encrypted) VALUES (?, ?)
    ON CONFLICT(user_id) DO UPDATE SET pending_secret_encrypted = excluded.pending_secret_encrypted,
      updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`).run(userId, encryptString(secret));
  return { otpauthUrl, manualKey: secret, status: status(userId) };
}

async function enable(userId, code) {
  const factor = row(userId);
  if (!factor?.pending_secret_encrypted) throw new HttpError(400, 'NO_PENDING_SETUP', 'Start the two-factor setup first.');
  if (!verifyTotp(decryptString(factor.pending_secret_encrypted), code)) throw new HttpError(401, 'INVALID_TWO_FACTOR', 'The code from your authenticator app is not valid.');
  const recoveryCodes = await storeRecoveryCodes(userId);
  getDatabase().prepare(`UPDATE user_two_factor SET secret_encrypted = pending_secret_encrypted, pending_secret_encrypted = NULL, enabled = 1,
    enabled_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), failed_attempts = 0, locked_until = 0,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ?`).run(userId);
  return { recoveryCodes, status: status(userId) };
}

async function disable(userId, { currentPassword, code }) {
  await assertPassword(userId, currentPassword);
  if (!isEnabled(userId)) return { status: status(userId) };
  if (!(await verifyCode(userId, code))) throw new HttpError(401, 'INVALID_TWO_FACTOR', 'The code is not valid.');
  const db = getDatabase();
  db.transaction(() => {
    db.prepare('DELETE FROM user_two_factor WHERE user_id = ?').run(userId);
    db.prepare('DELETE FROM user_recovery_codes WHERE user_id = ?').run(userId);
  })();
  return { status: status(userId) };
}

async function regenerateRecoveryCodes(userId, { currentPassword, code }) {
  await assertPassword(userId, currentPassword);
  if (!isEnabled(userId)) throw new HttpError(400, 'TWO_FACTOR_DISABLED', 'Two-factor authentication is not enabled.');
  if (!(await verifyCode(userId, code))) throw new HttpError(401, 'INVALID_TWO_FACTOR', 'The code is not valid.');
  return { recoveryCodes: await storeRecoveryCodes(userId), status: status(userId) };
}

function createChallenge(userId, now = Date.now()) {
  for (const [token, entry] of challenges) if (entry.expiresAt <= now) challenges.delete(token);
  const token = crypto.randomBytes(24).toString('base64url');
  challenges.set(token, { userId, expiresAt: now + CHALLENGE_TTL_MS });
  return token;
}

function consumeChallenge(token, now = Date.now()) {
  const entry = challenges.get(token);
  if (!entry || entry.expiresAt <= now) {
    challenges.delete(token);
    throw new HttpError(401, 'TWO_FACTOR_EXPIRED', 'The sign-in took too long. Enter your password again.');
  }
  return entry.userId;
}

function finishChallenge(token) {
  challenges.delete(token);
}

module.exports = {
  status,
  isEnabled,
  verifyCode,
  beginSetup,
  enable,
  disable,
  regenerateRecoveryCodes,
  createChallenge,
  consumeChallenge,
  finishChallenge,
  TOTP_OPTIONS,
};
