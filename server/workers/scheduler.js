'use strict';

const { getConfig } = require('../config/env');
const accounts = require('../services/providers/account_service');
const catalog = require('../services/catalog/catalog_service');
const billing = require('../services/providers/billing_check');
const metadata = require('../services/catalog/metadata_service');
const quota = require('../services/quota/quota_service');
const usage = require('../services/usage/usage_service');
const { createLogger } = require('../utils/logger');

const logger = createLogger('scheduler');
const QUOTA_FLUSH_MS = 10_000;
const BILLING_CHECK_MS = 6 * 60 * 60_000;
const LOG_RETENTION_DAYS = 90;
const timers = [];

function jitter(ms) {
  return Math.round(ms * (0.8 + Math.random() * 0.4));
}

function every(name, intervalMs, task, { runAtStart = false } = {}) {
  let running = false;
  const run = async () => {
    if (running) return;
    running = true;
    try {
      await task();
    } catch (error) {
      logger.warn('Scheduled task failed', { task: name, error: error.message });
    } finally {
      running = false;
    }
  };
  const schedule = () => {
    const timer = setTimeout(async () => {
      await run();
      schedule();
    }, jitter(intervalMs));
    timer.unref();
    timers.push(timer);
  };
  if (runAtStart) setImmediate(run);
  schedule();
}

async function forEachAccount(task) {
  for (const accountId of accounts.allEnabledIds()) {
    try {
      await task(accountId);
    } catch (error) {
      logger.warn('Account task failed', { accountId, error: error.message });
    }
  }
}

function start() {
  const config = getConfig();
  setInterval(() => quota.flush(), QUOTA_FLUSH_MS).unref();
  if (!config.workersEnabled) return;
  every('validate-keys', config.sync.validateIntervalMs, () => forEachAccount((id) => accounts.validateAccount(id)));
  every('verify-billing', BILLING_CHECK_MS, () => forEachAccount((id) => billing.verifyAccount(id)), { runAtStart: true });
  every('sync-models', config.sync.modelSyncIntervalMs, () => forEachAccount((id) => catalog.syncAccount(id)));
  if (config.metadataSourcesEnabled) {
    every('refresh-metadata', config.sync.metadataIntervalMs, () => metadata.refreshSources(), { runAtStart: true });
  }
  every('probe-quotas', config.sync.quotaProbeIntervalMs, () => forEachAccount(async (id) => {
    const results = await accounts.adapterFor(accounts.getRuntime(id)).fetchQuota();
    for (const item of results || []) quota.setState(`a:${id}`, item.metric, { limit: item.limit, remaining: item.remaining, resetAt: item.resetAt || null }, 'probe');
  }));
  every('prune-logs', 24 * 60 * 60_000, () => usage.prune(LOG_RETENTION_DAYS));
}

function stop() {
  for (const timer of timers.splice(0)) clearTimeout(timer);
  quota.flush();
}

module.exports = { start, stop };
