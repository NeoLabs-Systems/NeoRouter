'use strict';

const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { getConfig } = require('../../config/env');
const { HttpError } = require('../../middleware/error_handler');
const { randomToken, sha256, hashPassword, verifyPassword } = require('../../utils/crypto');
const settings = require('../settings/settings_service');

const MIN_PASSWORD = 12;

function publicUser(user) {
  if (!user) return null;
  return {
    id: user.id,
    username: user.username,
    email: user.email,
    displayName: user.display_name || user.username,
    role: user.role,
    disabled: Boolean(user.disabled_at),
    createdAt: user.created_at,
    lastLoginAt: user.last_login_at || null,
  };
}

function hasUsers() {
  return Boolean(getDatabase().prepare('SELECT 1 FROM users LIMIT 1').get());
}

function registrationStatus() {
  const bootstrap = !hasUsers();
  return { open: bootstrap || settings.registrationOpen(), bootstrap };
}

function createSession(userId, context = {}) {
  const token = `nrs_${randomToken(32)}`;
  const id = crypto.randomUUID();
  const expiresAt = new Date(Date.now() + getConfig().sessionTtlMs).toISOString();
  getDatabase().prepare(`INSERT INTO user_sessions
    (id, user_id, token_hash, expires_at, ip_address, user_agent) VALUES (?, ?, ?, ?, ?, ?)`)
    .run(id, userId, sha256(token), expiresAt, context.ipAddress || null, context.userAgent || null);
  return { id, token, expiresAt };
}

function assertPassword(password) {
  if (String(password || '').length < MIN_PASSWORD) {
    throw new HttpError(400, 'WEAK_PASSWORD', `Password must contain at least ${MIN_PASSWORD} characters.`);
  }
}

async function register({ username, email, password, displayName }, context = {}) {
  const normalizedUsername = String(username || '').trim();
  const normalizedEmail = String(email || '').trim().toLowerCase();
  if (normalizedUsername.length < 3 || normalizedUsername.length > 64) {
    throw new HttpError(400, 'INVALID_USERNAME', 'Username must contain 3 to 64 characters.');
  }
  if (!normalizedEmail) throw new HttpError(400, 'EMAIL_REQUIRED', 'An email address is required.');
  assertPassword(password);
  const passwordHash = await hashPassword(password);
  const db = getDatabase();
  const id = crypto.randomUUID();
  db.transaction(() => {
    // The registration gate is evaluated inside the transaction so two
    // concurrent bootstrap requests cannot both become admin.
    const bootstrap = !hasUsers();
    if (!bootstrap && !settings.registrationOpen()) {
      throw new HttpError(403, 'REGISTRATION_DISABLED', 'Registration is disabled. Ask an administrator to open it.');
    }
    if (db.prepare('SELECT 1 FROM users WHERE username = ?').get(normalizedUsername)) {
      throw new HttpError(409, 'USERNAME_TAKEN', 'This username is already in use.');
    }
    if (db.prepare('SELECT 1 FROM users WHERE email = ?').get(normalizedEmail)) {
      throw new HttpError(409, 'EMAIL_TAKEN', 'This email is already in use.');
    }
    db.prepare(`INSERT INTO users (id, username, email, display_name, password_hash, role)
      VALUES (?, ?, ?, ?, ?, ?)`)
      .run(id, normalizedUsername, normalizedEmail, displayName || normalizedUsername, passwordHash, bootstrap ? 'admin' : 'user');
  })();
  const user = db.prepare('SELECT * FROM users WHERE id = ?').get(id);
  return { user: publicUser(user), session: createSession(id, context) };
}

async function login({ account, password }, context = {}) {
  const db = getDatabase();
  const lookup = String(account || '').trim();
  const user = db.prepare('SELECT * FROM users WHERE username = ? OR email = ?').get(lookup, lookup);
  const valid = user ? await verifyPassword(String(password || ''), user.password_hash) : false;
  if (!valid) throw new HttpError(401, 'INVALID_CREDENTIALS', 'Username or password is incorrect.');
  if (user.disabled_at) throw new HttpError(403, 'ACCOUNT_DISABLED', 'This account is disabled.');
  db.prepare("UPDATE users SET last_login_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?").run(user.id);
  return { user: publicUser(user), session: createSession(user.id, context) };
}

function authenticateToken(token) {
  if (!token || !token.startsWith('nrs_')) return null;
  const db = getDatabase();
  const row = db.prepare(`SELECT s.id session_id, u.*
    FROM user_sessions s JOIN users u ON u.id = s.user_id
    WHERE s.token_hash = ? AND s.revoked_at IS NULL AND s.expires_at > ?`)
    .get(sha256(token), new Date().toISOString());
  if (!row || row.disabled_at) return null;
  db.prepare("UPDATE user_sessions SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?").run(row.session_id);
  return { type: 'session', sessionId: row.session_id, userId: row.id, user: publicUser(row) };
}

function logout(sessionId) {
  getDatabase().prepare("UPDATE user_sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?").run(sessionId);
}

function logoutAll(userId) {
  getDatabase().prepare("UPDATE user_sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND revoked_at IS NULL").run(userId);
}

async function changePassword(userId, currentPassword, newPassword) {
  const db = getDatabase();
  const user = db.prepare('SELECT * FROM users WHERE id = ?').get(userId);
  if (!user || !(await verifyPassword(currentPassword, user.password_hash))) {
    throw new HttpError(401, 'INVALID_PASSWORD', 'The current password is incorrect.');
  }
  assertPassword(newPassword);
  db.prepare("UPDATE users SET password_hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?")
    .run(await hashPassword(newPassword), userId);
  logoutAll(userId);
}

function updateProfile(userId, { displayName, email }) {
  const db = getDatabase();
  const user = db.prepare('SELECT * FROM users WHERE id = ?').get(userId);
  if (!user) throw new HttpError(404, 'USER_NOT_FOUND', 'This account does not exist.');
  const nextEmail = email !== undefined ? String(email).trim().toLowerCase() : user.email;
  if (db.prepare('SELECT 1 FROM users WHERE email = ? AND id != ?').get(nextEmail, userId)) {
    throw new HttpError(409, 'EMAIL_TAKEN', 'This email is already in use.');
  }
  const nextName = displayName !== undefined ? String(displayName).trim().slice(0, 80) || user.username : user.display_name;
  db.prepare("UPDATE users SET display_name = ?, email = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?")
    .run(nextName, nextEmail, userId);
  return publicUser(db.prepare('SELECT * FROM users WHERE id = ?').get(userId));
}

function listUsers() {
  return getDatabase().prepare('SELECT * FROM users ORDER BY created_at').all().map(publicUser);
}

function setUserDisabled(actorId, userId, disabled) {
  if (actorId === userId) throw new HttpError(400, 'CANNOT_DISABLE_SELF', 'You cannot disable your own account.');
  const result = getDatabase().prepare(`UPDATE users SET disabled_at = CASE WHEN ? THEN strftime('%Y-%m-%dT%H:%M:%fZ','now') ELSE NULL END
    WHERE id = ?`).run(disabled ? 1 : 0, userId);
  if (!result.changes) throw new HttpError(404, 'USER_NOT_FOUND', 'This account does not exist.');
  if (disabled) logoutAll(userId);
}

function setUserRole(actorId, userId, role) {
  if (actorId === userId) throw new HttpError(400, 'CANNOT_CHANGE_OWN_ROLE', 'You cannot change your own role.');
  const result = getDatabase().prepare('UPDATE users SET role = ? WHERE id = ?').run(role, userId);
  if (!result.changes) throw new HttpError(404, 'USER_NOT_FOUND', 'This account does not exist.');
}

function deleteUser(actorId, userId) {
  if (actorId === userId) throw new HttpError(400, 'CANNOT_DELETE_SELF', 'You cannot delete your own account.');
  const result = getDatabase().prepare('DELETE FROM users WHERE id = ?').run(userId);
  if (!result.changes) throw new HttpError(404, 'USER_NOT_FOUND', 'This account does not exist.');
}

module.exports = {
  publicUser,
  registrationStatus,
  register,
  login,
  authenticateToken,
  logout,
  logoutAll,
  changePassword,
  updateProfile,
  listUsers,
  setUserDisabled,
  setUserRole,
  deleteUser,
};
