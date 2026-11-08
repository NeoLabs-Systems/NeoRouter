'use strict';

// What follows adding a connection: list its models, check how it is billed
// and, for a plan, how much of the allowance is left. A failed model list is
// reported, not thrown; the connection is kept and synced again later.
const accounts = require('./account_service');
const billing = require('./billing_check');
const catalog = require('../catalog/catalog_service');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('providers');

async function finishSetup(accountId) {
  let sync = null;
  try {
    sync = await catalog.syncAccount(accountId);
  } catch (error) {
    sync = { error: error.message };
    logger.warn('Initial model sync failed', { accountId, error: error.message });
  }
  await billing.verifyAccount(accountId);
  // A plan's allowance should be known before the first request.
  await accounts.probeQuota(accountId).catch(() => {});
  return sync;
}

module.exports = { finishSetup };
