'use strict';

function integer(name, fallback, { min = Number.MIN_SAFE_INTEGER, max = Number.MAX_SAFE_INTEGER } = {}) {
  const raw = process.env[name];
  const value = raw === undefined || raw === '' ? fallback : Number(raw);
  if (!Number.isInteger(value) || value < min || value > max) {
    throw new Error(`${name} must be an integer between ${min} and ${max}.`);
  }
  return value;
}

function boolean(name, fallback) {
  const raw = process.env[name];
  if (raw === undefined || raw === '') return fallback;
  if (['1', 'true', 'yes', 'on'].includes(raw.toLowerCase())) return true;
  if (['0', 'false', 'no', 'off'].includes(raw.toLowerCase())) return false;
  throw new Error(`${name} must be true or false.`);
}

function getConfig() {
  return Object.freeze({
    host: process.env.NEOROUTER_HOST || '127.0.0.1',
    port: integer('NEOROUTER_PORT', 4600, { min: 1, max: 65535 }),
    publicUrl: process.env.NEOROUTER_PUBLIC_URL || null,
    webDevUrl: process.env.NEOROUTER_WEB_DEV_URL || null,
    trustProxy: boolean('NEOROUTER_TRUST_PROXY', false),
    logLevel: process.env.NEOROUTER_LOG_LEVEL || 'info',
    sessionTtlMs: integer('NEOROUTER_SESSION_TTL_MS', 30 * 24 * 60 * 60 * 1000, { min: 60_000 }),
    workersEnabled: boolean('NEOROUTER_WORKERS_ENABLED', process.env.NODE_ENV !== 'test'),
    maxBodyBytes: integer('NEOROUTER_MAX_BODY_BYTES', 32 * 1024 * 1024, { min: 1024 }),
    upstreamTimeoutMs: integer('NEOROUTER_UPSTREAM_TIMEOUT_MS', 120_000, { min: 1000 }),
    routing: Object.freeze({
      maxAttempts: integer('NEOROUTER_ROUTING_MAX_ATTEMPTS', 8, { min: 1, max: 30 }),
      totalBudgetMs: integer('NEOROUTER_ROUTING_TOTAL_BUDGET_MS', 300_000, { min: 1000 }),
      topK: integer('NEOROUTER_ROUTING_TOP_K', 5, { min: 1, max: 50 }),
      hedging: boolean('NEOROUTER_ROUTING_HEDGING', true),
      maxParallel: integer('NEOROUTER_ROUTING_MAX_PARALLEL', 3, { min: 1, max: 8 }),
      queueWaitMs: integer('NEOROUTER_ROUTING_QUEUE_WAIT_MS', 45_000, { min: 0 }),
      hedgeUnmeasuredMs: integer('NEOROUTER_ROUTING_HEDGE_UNMEASURED_MS', 10_000, { min: 100 }),
    }),
    sync: Object.freeze({
      validateIntervalMs: integer('NEOROUTER_VALIDATE_INTERVAL_MS', 5 * 60_000, { min: 30_000 }),
      modelSyncIntervalMs: integer('NEOROUTER_MODEL_SYNC_INTERVAL_MS', 6 * 60 * 60_000, { min: 60_000 }),
      metadataIntervalMs: integer('NEOROUTER_METADATA_INTERVAL_MS', 60 * 60_000, { min: 60_000 }),
      quotaProbeIntervalMs: integer('NEOROUTER_QUOTA_PROBE_INTERVAL_MS', 15 * 60_000, { min: 60_000 }),
    }),
    metadataSourcesEnabled: boolean('NEOROUTER_METADATA_SOURCES_ENABLED', process.env.NODE_ENV !== 'test'),
  });
}

module.exports = { getConfig, integer, boolean };
