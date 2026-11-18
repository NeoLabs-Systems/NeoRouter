'use strict';
// Shared bounds for transport, import and adaptive selection.
const POLICY = Object.freeze({
  connectMs: 8000,
  restMs: 30000,
  maxRestMs: 900000,
  maxAttempts: 3,
  maxImport: 5000,
  connections: 8,
  smoothing: 0.8,
  unmeasuredMs: 100,
  maxDispatchers: 64,
  idleMs: 60000,
  // Automatic check: a tiny public page every proxy should reach.
  checkUrl: 'https://www.cloudflare.com/cdn-cgi/trace',
  checkEveryMs: 60 * 60_000,
  checkConcurrency: 16,
});
module.exports = { POLICY };
