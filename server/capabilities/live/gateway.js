'use strict';

// Live voice sessions: a WebSocket from the client relayed to a provider's
// live model, with the same key policy, routing, learning and usage log as
// every other request. The model is chosen when the session opens; if the
// provider refuses the connection, the next candidate is tried. Once the
// provider has answered, the session stays with it (a conversation cannot
// move mid-way). Usage is logged when the session ends: the tokens the
// provider reported, and the session length as units (seconds).
const crypto = require('node:crypto');
const WebSocket = require('ws');
const { dialectFor, usageOf } = require('./dialects');
const { ProviderError, KINDS, classifyHttpError, fromNetworkError } = require('../../providers/provider_error');
const accounts = require('../../services/providers/account_service');
const { RoutingPlan } = require('../../services/routing/router');
const learning = require('../../services/routing/learning');
const loadTracker = require('../../services/routing/load_tracker');
const quota = require('../../services/quota/quota_service');
const plans = require('../../services/routing/plans');
const budget = require('../../services/budget/budget_service');
const usage = require('../../services/usage/usage_service');
const routerKeys = require('../../services/router_keys/router_key_service');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('live');
const MAX_ATTEMPTS = 3;
const OPEN_TIMEOUT_MS = 15_000;
// The opening message must arrive soon after the client connects.
const FIRST_MESSAGE_TIMEOUT_MS = 30_000;
const MAX_PAYLOAD = 16 * 1024 * 1024;

// Browsers cannot set headers on a WebSocket, so the key may also come as
// ?key= (Gemini style) or as the "openai-insecure-api-key.<key>" subprotocol.
function tokenOf(request, url) {
  const authorization = request.headers.authorization || '';
  if (authorization.startsWith('Bearer ')) return authorization.slice(7).trim();
  const header = request.headers['x-api-key'] || request.headers['x-goog-api-key'];
  if (header) return String(header).trim();
  if (url.searchParams.get('key')) return url.searchParams.get('key');
  const protocols = String(request.headers['sec-websocket-protocol'] || '').split(',').map((value) => value.trim());
  const carried = protocols.find((value) => value.startsWith('openai-insecure-api-key.'));
  return carried ? carried.slice('openai-insecure-api-key.'.length) : null;
}

function reject(socket, status, message) {
  socket.write(`HTTP/1.1 ${status} ${status === 401 ? 'Unauthorized' : status === 429 ? 'Too Many Requests' : 'Bad Request'}\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n${JSON.stringify({ error: { message } })}`);
  socket.destroy();
}

// Opens the provider's socket; resolves once it is open, rejects with a
// ProviderError (HTTP status from a refused upgrade) otherwise.
function openUpstream({ url, headers }) {
  return new Promise((resolve, rejectOpen) => {
    const upstream = new WebSocket(url, { headers, maxPayload: MAX_PAYLOAD, handshakeTimeout: OPEN_TIMEOUT_MS });
    // The provider may speak right after the handshake, before the session
    // is wired up; those messages wait here.
    upstream.early = [];
    const hold = (data, binary) => upstream.early.push([data, binary]);
    upstream.on('message', hold);
    upstream.once('open', () => resolve(upstream));
    upstream.release = () => upstream.off('message', hold);
    upstream.once('unexpected-response', (_request, response) => {
      let body = '';
      response.on('data', (chunk) => { body += chunk; });
      response.on('end', () => {
        let parsed = body;
        try { parsed = JSON.parse(body); } catch { /* plain text */ }
        rejectOpen(classifyHttpError(response.statusCode, parsed, response.headers));
      });
      upstream.terminate();
    });
    upstream.once('error', (error) => rejectOpen(error instanceof ProviderError ? error : fromNetworkError(error)));
  });
}

class LiveSession {
  constructor({ client, request, url, dialect, key }) {
    this.client = client;
    this.request = request;
    this.url = url;
    this.dialect = dialect;
    this.key = key;
    this.id = crypto.randomUUID();
    this.startedAt = Date.now();
    this.trail = [];
    // Client messages kept until the provider answers, so a provider that
    // hangs up right away can be replaced by the next one.
    this.queue = [];
    this.answered = false;
    this.upstream = null;
    this.entry = null;
    this.tokens = { input: 0, output: 0 };
    this.closed = false;
  }

  start() {
    const fromUrl = this.dialect.modelFromUrl(this.url);
    this.client.on('close', () => this.end('client_closed'));
    this.client.on('error', () => this.end('client_error'));
    if (fromUrl !== null) {
      this.client.on('message', (data, binary) => this.forward(data, binary));
      this.connect(fromUrl, null);
      return;
    }
    // The model is named in the opening message.
    const timer = setTimeout(() => this.fail('The session did not start in time.', 'session_timeout'), FIRST_MESSAGE_TIMEOUT_MS);
    timer.unref?.();
    this.client.once('message', (data) => {
      clearTimeout(timer);
      let first;
      try { first = JSON.parse(String(data)); } catch { first = null; }
      const model = this.dialect.modelFromFirst(first);
      if (model === null) return this.fail('The first message must open the session and name the model.', 'invalid_request');
      this.client.on('message', (next, binary) => this.forward(next, binary));
      return this.connect(model, first);
    });
  }

  forward(data, binary) {
    if (this.upstream?.readyState === WebSocket.OPEN) this.upstream.send(data, { binary });
    if (!this.answered) this.queue.push([data, binary]);
  }

  plan(requestedModel) {
    const remaining = budget.remaining(this.key);
    const exhausted = remaining !== null && remaining <= 0;
    return new RoutingPlan({
      userId: this.key.user_id,
      key: this.key,
      request: { messages: [] },
      requestedModel,
      needs: {},
      inputTokens: 0,
      freeOnly: exhausted,
      capability: 'live',
      estimateCost: () => 0,
    });
  }

  // Only accounts whose provider speaks this protocol are candidates.
  supported(entry) {
    const runtime = accounts.getRuntime(entry.accountId);
    return Boolean(runtime && accounts.adapterFor(runtime).definition.live.includes(this.dialect.id));
  }

  async connect(requestedModel, first) {
    this.requestedModel = requestedModel;
    const rpm = routerKeys.consumeRpm(this.key);
    if (!rpm.allowed) return this.fail('This router key exceeded its requests-per-minute limit.', 'rate_limited');
    this.first = first;
    this.routing = this.plan(requestedModel);
    this.lastError = null;
    return this.tryNext();
  }

  async tryNext() {
    while (this.trail.length < MAX_ATTEMPTS && !this.closed) {
      const choice = this.routing.next({ filter: (entry) => this.supported(entry) });
      if (!choice) break;
      const { entry } = choice;
      const adapter = accounts.adapterFor(accounts.getRuntime(entry.accountId));
      const attemptStarted = Date.now();
      quota.recordRequest(entry, 0);
      plans.onSent(entry);
      try {
        const upstream = await openUpstream(this.dialect.upstream(adapter, entry.modelId, { request: this.request, path: this.url.pathname }));
        if (this.closed) {
          upstream.close();
          return null;
        }
        this.trail.push({ model: entry.id, outcome: 'success', ms: Date.now() - attemptStarted });
        learning.onSuccess(entry, { latencyMs: Date.now() - attemptStarted, ttftMs: null, outputTokens: 0, generationMs: 0 }, null);
        return this.attach(upstream, entry);
      } catch (caught) {
        const error = caught instanceof ProviderError ? caught : fromNetworkError(caught);
        if (!this.failed(entry, error, attemptStarted)) break;
      }
    }
    const message = this.trail.length
      ? `All ${this.trail.length} attempt(s) failed. Last error: ${this.lastError?.message || 'unknown'}`
      : `No live model in your catalog can serve "${this.requestedModel}" over this protocol right now (filters, quotas, cooldowns or budget).`;
    return this.fail(message, this.trail.length ? 'all_attempts_failed' : 'no_model_available');
  }

  // Records a failed attempt; returns whether to try the next candidate.
  failed(entry, error, attemptStarted) {
    this.lastError = error;
    this.trail.push({ model: entry.id, outcome: error.kind, status: error.status, ms: Date.now() - attemptStarted, message: String(error.message).slice(0, 200) });
    const policy = learning.onFailure(entry, error, { estimatedTokens: 0 });
    if (error.kind === KINDS.AUTH) accounts.validateAccount(entry.accountId).catch(() => {});
    return policy.fallback;
  }

  attach(upstream, entry) {
    const attachedAt = Date.now();
    this.upstream = upstream;
    this.entry = entry;
    this.release = loadTracker.lease(entry);
    if (this.first) upstream.send(JSON.stringify(this.dialect.rewriteFirst(this.first, entry.modelId)));
    for (const [data, binary] of this.queue) upstream.send(data, { binary });
    const relay = (data, binary) => {
      if (!this.answered) {
        this.answered = true;
        this.queue = [];
      }
      if (!binary) this.count(data);
      if (this.client.readyState === WebSocket.OPEN) this.client.send(data, { binary });
    };
    upstream.release();
    for (const [data, binary] of upstream.early.splice(0)) relay(data, binary);
    upstream.on('message', relay);
    upstream.on('close', (code, reason) => {
      if (this.upstream !== upstream) return;
      if (!this.answered && !this.closed) return this.hungUp(entry, code, String(reason), attachedAt);
      if (this.client.readyState === WebSocket.OPEN) this.client.close(code === 1005 || code === 1006 ? 1011 : code, reason);
      return this.end('provider_closed');
    });
    upstream.on('error', (error) => logger.warn('Provider socket error', { model: entry.id, error: error.message }));
  }

  // The provider closed before saying anything (Gemini does this for a model
  // it cannot serve): the session moves on to the next candidate.
  hungUp(entry, code, reason, attachedAt) {
    this.release();
    this.release = null;
    this.upstream = null;
    this.entry = null;
    this.trail.pop();
    const error = new ProviderError(code === 1008 ? KINDS.NOT_FOUND : KINDS.SERVER, reason || `The provider closed the session (code ${code}).`);
    if (this.failed(entry, error, attachedAt)) return this.tryNext();
    return this.fail(`The provider closed the session: ${error.message}`, 'provider_closed');
  }

  count(data) {
    let event;
    try { event = JSON.parse(String(data)); } catch { return; }
    const reported = usageOf(event);
    if (!reported) return;
    this.tokens.input += reported.input;
    this.tokens.output += reported.output;
  }

  fail(message, code) {
    if (this.client.readyState === WebSocket.OPEN) {
      if (this.dialect.error) this.client.send(JSON.stringify(this.dialect.error(message, code)));
      // Close reasons are limited to 123 bytes.
      this.client.close(1011, Buffer.from(message).subarray(0, 120).toString());
    }
    this.end(code, message);
    return null;
  }

  end(reason, errorMessage = null) {
    if (this.closed) return;
    this.closed = true;
    this.release?.();
    if (this.upstream && this.upstream.readyState !== WebSocket.CLOSED) this.upstream.close();
    const entry = this.entry;
    const seconds = Math.round((Date.now() - this.startedAt) / 1000);
    const cost = entry ? budget.costOf(entry, { prompt_tokens: this.tokens.input, completion_tokens: this.tokens.output }) : 0;
    if (entry) budget.reserve(this.key, 0).settle(cost, this.id);
    try {
      usage.record({
        id: this.id, user_id: this.key.user_id, router_key_id: this.key.id, created_at: this.startedAt, api_format: this.dialect.id,
        requested_model: this.requestedModel || 'auto', attempts: Math.max(1, this.trail.length), stream: 1, trail_json: JSON.stringify(this.trail),
        latency_ms: Date.now() - this.startedAt, input_tokens: this.tokens.input, output_tokens: this.tokens.output, capability: 'live', units: seconds,
        ...(entry
          ? { status: 'success', http_status: 101, model_ref: entry.ref, model_label: entry.id, account_id: entry.accountId, cost_usd: cost, is_free: entry.isFree ? 1 : 0 }
          : { status: 'error', http_status: 502, error_code: reason, error_message: errorMessage, cost_usd: 0, is_free: 0 }),
      });
    } catch {
      // Logging must never break a session.
    }
  }
}

/** Serves live sessions on the HTTP server's WebSocket upgrades. */
function attach(server) {
  const wss = new WebSocket.Server({
    noServer: true,
    maxPayload: MAX_PAYLOAD,
    // Never echo a subprotocol that carries the key.
    handleProtocols: (protocols) => [...protocols].find((value) => !value.startsWith('openai-insecure-api-key.')) || false,
  });
  server.on('upgrade', (request, socket, head) => {
    const url = new URL(request.url, 'http://local');
    const dialect = dialectFor(url.pathname);
    if (!dialect) return socket.destroy();
    const key = routerKeys.authenticate(tokenOf(request, url));
    if (!key) return reject(socket, 401, 'A valid NeoRouter key (nrt_…) is required.');
    return wss.handleUpgrade(request, socket, head, (client) => new LiveSession({ client, request, url, dialect, key }).start());
  });
  return wss;
}

module.exports = { attach, tokenOf };
