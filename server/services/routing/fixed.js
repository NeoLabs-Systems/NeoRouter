'use strict';

// "This connection works again": a person fixed what was wrong (paid, signed
// up, waited out a ban). Everything the router held against the connection is
// dropped, and its next few failures are forgiven, so it is really tried again
// even though it hardly answered in the last days.
const { getDatabase } = require('../../db/database');
const stats = require('./stats_store');
const accountHealth = require('./account_health');
const concurrency = require('./concurrency');
const quota = require('../quota/quota_service');

function markAccountFixed(accountId) {
  const refs = getDatabase().prepare('SELECT id FROM models WHERE account_id = ?').all(accountId).map((row) => row.id);
  for (const ref of refs) stats.forgive(ref);
  accountHealth.markFixed(accountId);
  concurrency.forget(accountId);
  quota.clearObservedExhaustion(accountId, refs);
  return { models: refs.length, chances: accountHealth.chancesLeft(accountId) };
}

module.exports = { markAccountFixed };
