'use strict';

// Who may use the models running on this server, and their connections to
// them. Admins always may; everyone else only when an admin allows it. Each
// person reaches the models through a "NeoRouter Local" connection, which the
// installing admin gets automatically and others can add like any provider.
const { getDatabase } = require('../../db/database');
const settings = require('../settings/settings_service');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('local-models');

const DEFINITION_ID = 'neorouter_local';
const SETTING = 'local_models_access';
const ACCESS = Object.freeze(['admins', 'everyone']);

function level() {
  const value = settings.getGlobal(SETTING, 'admins');
  return ACCESS.includes(value) ? value : 'admins';
}

function setLevel(value) {
  if (!ACCESS.includes(value)) throw new Error(`Unknown access level ${value}.`);
  settings.setGlobal(SETTING, value);
  scheduleSync();
}

function isAdmin(userId) {
  return getDatabase().prepare('SELECT role FROM users WHERE id = ?').get(userId)?.role === 'admin';
}

function allowed(userId) {
  return level() === 'everyone' || isAdmin(userId);
}

// Whether "NeoRouter Local" appears under Add connection for this person.
function offered(userId) {
  return allowed(userId) && Boolean(getDatabase().prepare("SELECT 1 FROM local_models WHERE status = 'installed'").get());
}

function accountIds() {
  return getDatabase().prepare('SELECT id FROM provider_accounts WHERE definition_id = ?').all(DEFINITION_ID).map((row) => row.id);
}

function hasAccount(userId) {
  return Boolean(getDatabase().prepare('SELECT 1 FROM provider_accounts WHERE definition_id = ? AND user_id = ?').get(DEFINITION_ID, userId));
}

/** Gives the person a connection to the local models, unless they have one. */
async function connect(userId) {
  if (hasAccount(userId)) return;
  const accounts = require('../providers/account_service');
  try {
    await accounts.create(userId, { definitionId: DEFINITION_ID, slug: 'local', billingMode: 'free_tier' }, userId, null);
  } catch (error) {
    logger.warn('Could not add the local models connection', { error: error.message });
  }
}

// Every connection relists its models after a model starts, stops or goes away.
let pending = null;
function scheduleSync() {
  clearTimeout(pending);
  pending = setTimeout(syncAll, 1000);
  pending.unref();
}

async function syncAll() {
  const catalog = require('../catalog/catalog_service');
  for (const id of accountIds()) {
    try {
      await catalog.syncAccount(id);
    } catch (error) {
      logger.warn('Local models connection could not be synced', { accountId: id, error: error.message });
    }
  }
}

module.exports = { DEFINITION_ID, ACCESS, level, setLevel, allowed, offered, connect, scheduleSync, syncAll };
