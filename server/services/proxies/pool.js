'use strict';
const crypto = require('node:crypto');
const { parse } = require('./import');
const { createDispatcher } = require('./transport');
const { POLICY } = require('./policy');
const { withLease } = require('./stream');
const { getDatabase } = require('../../db/database');
const { encryptString } = require('../../utils/crypto');
const settings = require('../settings/settings_service');
const { HttpError } = require('../../middleware/error_handler');
const health = new Map();
const agents = new Map();
function state(id) { if (!health.has(id)) health.set(id, { failures: 0, until: 0, latencyMs: null, active: 0, served: 0 }); return health.get(id); }
function list() {
  return getDatabase().prepare('SELECT id, endpoint, enabled FROM outbound_proxies ORDER BY endpoint').all().map((row) => ({ ...row, enabled: !!row.enabled, ...state(row.id) }));
}
function config() { return settings.getGlobal('outbound_proxy_policy', { enabled: false, fallbackDirect: true }); }
function importLines(text, protocol) {
  const lines = text.trimEnd().split(/\r?\n/);
  if (lines.length > POLICY.maxImport) throw new HttpError(400, 'TOO_MANY_PROXIES', `Import at most ${POLICY.maxImport} lines at once.`);
  const result = { added: 0, duplicates: 0, invalidLines: [] };
  const insert = getDatabase().prepare('INSERT OR IGNORE INTO outbound_proxies (id, fingerprint, endpoint, secret_encrypted) VALUES (?, ?, ?, ?)');
  getDatabase().transaction(() => {
    for (const [index, line] of lines.entries()) {
      if (!line.trim() || line.trim().startsWith('#')) continue;
      let entry;
      try { entry = parse(line, protocol); } catch { result.invalidLines.push(index + 1); continue; }
      const changes = insert.run(crypto.randomUUID(), entry.fingerprint, entry.endpoint, encryptString(entry.url)).changes;
      if (changes) result.added++; else result.duplicates++;
    }
  })();
  return result;
}
function dispose(id, graceful = false) {
  const agent = agents.get(id)?.dispatcher;
  agents.delete(id);
  if (agent) (graceful ? agent.close() : agent.destroy()).catch(() => {});
}
function failed(id, h) {
  h.failures++;
  h.until = Date.now() + Math.min(POLICY.maxRestMs, POLICY.restMs * 2 ** Math.min(h.failures - 1, 8));
  dispose(id);
}
function change(id, enabled) {
  const result = getDatabase().prepare('UPDATE outbound_proxies SET enabled = ? WHERE id = ?').run(enabled ? 1 : 0, id);
  if (!result.changes) throw new HttpError(404, 'PROXY_NOT_FOUND', 'Proxy not found.');
  dispose(id, true); health.delete(id);
}
function remove(id) { getDatabase().prepare('DELETE FROM outbound_proxies WHERE id = ?').run(id); dispose(id, true); health.delete(id); }
function agentFor(row) {
  const now = Date.now();
  // Bound retained dispatchers even when an admin imports thousands of proxies.
  // Graceful eviction allows any already-running streams to finish.
  for (const [id, cached] of agents) {
    if (id !== row.id && (now - cached.usedAt > POLICY.idleMs || (!agents.has(row.id) && agents.size >= POLICY.maxDispatchers))) dispose(id, true);
  }
  if (!agents.has(row.id)) agents.set(row.id, { usedAt: now, dispatcher: createDispatcher(row, (elapsed) => {
    const h = health.get(row.id);
    if (!h) return;
    h.latencyMs = h.latencyMs == null ? elapsed : h.latencyMs * POLICY.smoothing + elapsed * (1 - POLICY.smoothing);
  }) });
  const cached = agents.get(row.id);
  cached.usedAt = now;
  // Move to the end so the oldest dispatcher is evicted first.
  agents.delete(row.id); agents.set(row.id, cached);
  return cached.dispatcher;
}
async function fetchWithProxies(url, init = {}) {
  init.signal?.throwIfAborted();
  const policy = config();
  if (!policy.enabled) return fetch(url, init);
  const rows = getDatabase().prepare('SELECT * FROM outbound_proxies WHERE enabled = 1').all();
  const tried = new Set();
  for (let attempt = 0; attempt < POLICY.maxAttempts; attempt++) {
    const candidates = rows.filter((row) => !tried.has(row.id) && state(row.id).until <= Date.now());
    const weights = candidates.map((row) => { const h = state(row.id); return 1 / ((h.active + 1) * Math.max(1, h.latencyMs ?? POLICY.unmeasuredMs) * (h.failures + 1)); });
    let draw = Math.random() * weights.reduce((sum, value) => sum + value, 0);
    const row = candidates.find((_, index) => (draw -= weights[index]) <= 0) || candidates.at(-1);
    if (!row) break;
    tried.add(row.id); const h = state(row.id); h.active++; h.served++;
    let leased = false;
    try {
      const response = await fetch(url, { ...init, dispatcher: agentFor(row) });
      h.failures = 0; h.until = 0;
      const wrapped = withLease(response, () => { h.active--; }, () => {
        if (!init.signal?.aborted) failed(row.id, h);
      });
      leased = true;
      return wrapped;
    } catch (error) {
      if (init.signal?.aborted) throw init.signal.reason;
      failed(row.id, h);
      const code = error.cause?.code || error.code;
      // Only retry mutations when connection failed before any request was sent.
      if (!['GET', 'HEAD'].includes((init.method || 'GET').toUpperCase()) && !['PROXY_CONNECT_FAILED', 'ECONNREFUSED', 'ENETUNREACH', 'EHOSTUNREACH', 'UND_ERR_CONNECT_TIMEOUT'].includes(code)) throw new Error('Outbound proxy transport failed.');
    } finally { if (!leased) h.active--; }
  }
  if (policy.fallbackDirect) return fetch(url, init);
  throw new Error('No healthy outbound proxy is available.');
}
async function probe(id, url) {
  const row = getDatabase().prepare('SELECT * FROM outbound_proxies WHERE id = ?').get(id);
  if (!row) throw new HttpError(404, 'PROXY_NOT_FOUND', 'Proxy not found.');
  const h = state(id); const began = Date.now();
  try {
    const response = await fetch(url, { dispatcher: agentFor(row), redirect: 'manual', signal: AbortSignal.timeout(POLICY.connectMs) });
    await response.body?.cancel();
    if (response.status === 407) throw new Error('Proxy authentication failed');
    h.latencyMs = Date.now() - began; h.failures = 0; h.until = 0;
    return { ok: true, status: response.status, latencyMs: h.latencyMs };
  } catch { failed(id, h); return { ok: false }; }
}
module.exports = { probe, parse, list, config, importLines, change, remove, fetch: fetchWithProxies, POLICY };
