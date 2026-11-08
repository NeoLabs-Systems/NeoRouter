'use strict';

// Adds connections from a NeoRouter key file (Settings → export saved API
// keys). Each key is checked with its provider like one added by hand; a key
// that is already connected is skipped, and one that fails does not stop the
// rest. Prompts and keys are never logged.
const { getDatabase } = require('../../db/database');
const { decryptString } = require('../../utils/crypto');
const registry = require('../../providers/registry');
const accounts = require('./account_service');
const { finishSetup } = require('./account_setup');
const localAccess = require('../local_models/access');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('key-import');
const KIND = 'neorouter-keys';
const MAX_KEYS = 200;

// Same provider, same key (and address, where it can be changed): already there.
function alreadyConnected(ownerId, key) {
  const rows = getDatabase().prepare('SELECT base_url, secret_encrypted FROM provider_accounts WHERE user_id = ? AND definition_id = ? AND secret_encrypted IS NOT NULL')
    .all(ownerId, key.definitionId);
  return rows.some((row) => {
    try {
      return decryptString(row.secret_encrypted) === key.secret && (!key.baseUrl || !row.base_url || row.base_url === key.baseUrl);
    } catch {
      return false;
    }
  });
}

// Why a key cannot be imported here at all, or null.
function refusal(ownerId, key) {
  const definition = registry.get(key.definitionId);
  if (!definition || definition.category === 'pool') return { code: 'UNKNOWN_PROVIDER', message: 'This provider is not available on this server.' };
  if (definition.id === localAccess.DEFINITION_ID && !localAccess.allowed(ownerId)) return { code: 'LOCAL_MODELS_NOT_SHARED', message: 'Local models are not shared with you.' };
  return null;
}

/**
 * { imported: [{ label, accountId }], skipped: [{ label }], failed: [{ label, code, message }] }
 * ownerId: the space the connections go into; actorId: who imports them.
 */
async function importKeys(ownerId, actorId, actor, file) {
  const result = { imported: [], skipped: [], failed: [] };
  for (const key of file.keys.slice(0, MAX_KEYS)) {
    const label = key.label || registry.get(key.definitionId)?.label || key.definitionId;
    const refused = refusal(ownerId, key);
    if (refused) {
      result.failed.push({ label, ...refused });
      continue;
    }
    if (alreadyConnected(ownerId, key)) {
      result.skipped.push({ label });
      continue;
    }
    try {
      const { account } = await accounts.create(ownerId, {
        definitionId: key.definitionId,
        label: key.label,
        baseUrl: key.baseUrl || undefined,
        secret: key.secret,
        billingMode: key.billingMode,
        classification: key.classification,
        options: key.options,
        headers: key.headers,
      }, actorId, actor);
      await finishSetup(account.id);
      result.imported.push({ label: account.label, accountId: account.id });
    } catch (error) {
      result.failed.push({ label, code: error.code || 'IMPORT_FAILED', message: error.message });
    }
  }
  logger.info('Keys imported', { ownerId, imported: result.imported.length, skipped: result.skipped.length, failed: result.failed.length });
  return result;
}

module.exports = { importKeys, KIND, MAX_KEYS };
