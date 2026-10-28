'use strict';

const { getDatabase } = require('../../db/database');
const { encryptString, decryptString } = require('../../utils/crypto');

function getGlobal(key, fallback = null) {
  const row = getDatabase().prepare('SELECT value FROM settings WHERE key = ?').get(key);
  return row ? JSON.parse(row.value) : fallback;
}

function setGlobal(key, value) {
  getDatabase().prepare(`INSERT INTO settings (key, value) VALUES (?, ?)
    ON CONFLICT(key) DO UPDATE SET value = excluded.value`).run(key, JSON.stringify(value));
}

function getUserSetting(userId, key, fallback = null) {
  const row = getDatabase().prepare('SELECT value FROM user_settings WHERE user_id = ? AND key = ?').get(userId, key);
  return row ? JSON.parse(row.value) : fallback;
}

function setUserSetting(userId, key, value) {
  getDatabase().prepare(`INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)
    ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`).run(userId, key, JSON.stringify(value));
}

function deleteUserSetting(userId, key) {
  getDatabase().prepare('DELETE FROM user_settings WHERE user_id = ? AND key = ?').run(userId, key);
}

function getUserSecret(userId, key) {
  const value = getUserSetting(userId, key);
  return value ? decryptString(value) : null;
}

function setUserSecret(userId, key, secret) {
  if (!secret) return deleteUserSetting(userId, key);
  return setUserSetting(userId, key, encryptString(secret));
}

function registrationOpen() {
  return getGlobal('registration_open', false) === true;
}

module.exports = {
  getGlobal,
  setGlobal,
  getUserSetting,
  setUserSetting,
  deleteUserSetting,
  getUserSecret,
  setUserSecret,
  registrationOpen,
};
