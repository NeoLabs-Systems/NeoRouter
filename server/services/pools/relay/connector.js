'use strict';

// A member's side of the relay: one WebSocket per pool to the pool's host,
// kept open (heartbeats, reconnecting with growing pauses), over which
// partners reach this member and this member reaches partners that have no
// public address either. It goes out from here, so it works behind NAT.
const { EventEmitter } = require('node:events');
const WebSocket = require('ws');
const identity = require('../identity');
const store = require('../store');
const { RelayEndpoint } = require('./endpoint');
const { LIMITS, helloText, parse, encode } = require('./frames');
const { createLogger } = require('../../../utils/logger');

const logger = createLogger('pool-relay');
const RETRY_MIN_MS = 1_000;
const RETRY_MAX_MS = 60_000;

const events = new EventEmitter();
const connectors = new Map(); // `${userId}|${poolId}` → Connector

function relayUrl(hostUrl) {
  const url = new URL(hostUrl);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.pathname = '/federation/v1/relay';
  url.search = '';
  return url.toString();
}

class Connector {
  constructor(userId, pool, { checkHost, onUnknownSender }) {
    this.userId = userId;
    this.poolId = pool.poolId;
    this.hostUrl = pool.hostUrl;
    this.hostMemberId = pool.hostMemberId;
    this.checkHost = checkHost;
    this.ws = null;
    this.ready = false;
    this.stopped = false;
    this.failures = 0;
    this.online = new Set();
    this.waiters = [];
    this.endpoint = new RelayEndpoint({ userId, poolId: pool.poolId, send: (frame) => this.send(frame), onUnknownSender });
  }

  send(frame) {
    return new Promise((resolve, reject) => {
      if (!this.ready || this.ws?.readyState !== WebSocket.OPEN) return reject(new Error('not connected to the pool host'));
      this.ws.send(encode(frame), (error) => (error ? reject(error) : resolve()));
    });
  }

  async connect() {
    if (this.stopped) return;
    try {
      await this.checkHost(this.userId, this.hostUrl);
    } catch (error) {
      logger.warn('Pool host is not an allowed address for the relay', { poolId: this.poolId, error: error.message });
      return this.retry();
    }
    // Left the pool while the address was checked.
    if (this.stopped) return undefined;
    const ws = new WebSocket(relayUrl(this.hostUrl), { handshakeTimeout: 10_000, maxPayload: LIMITS.frameBytes, headers: { 'User-Agent': 'NeoRouter-Pool' } });
    this.ws = ws;
    let heartbeat = null;
    let alive = true;
    ws.on('open', () => {
      heartbeat = setInterval(() => {
        if (!alive) return ws.terminate();
        alive = false;
        return ws.ping();
      }, LIMITS.heartbeatMs);
      heartbeat.unref();
    });
    ws.on('pong', () => { alive = true; });
    ws.on('ping', () => { alive = true; });
    ws.on('message', (data, isBinary) => this.onFrame(isBinary ? null : parse(data.toString('utf8'))));
    ws.on('error', () => {});
    ws.on('close', (code) => {
      clearInterval(heartbeat);
      if (this.ws !== ws) return;
      const wasReady = this.ready;
      this.ready = false;
      this.ws = null;
      this.online.clear();
      this.endpoint.closeAll();
      if (wasReady) events.emit('change', { userId: this.userId, poolId: this.poolId });
      // Not (or no longer) welcome there: no point in knocking again soon.
      if (code === 4403 || code === 4410) this.failures = Math.max(this.failures, 5);
      this.retry();
    });
    return undefined;
  }

  onFrame(frame) {
    if (!frame) return;
    if (frame.t === 'challenge' && !this.ready) {
      const me = identity.ensure(this.userId);
      const text = helloText({ nonce: String(frame.nonce || ''), pool: this.poolId, member: me.memberId, host: this.hostMemberId });
      this.ws.send(encode({ t: 'hello', pool: this.poolId, member: me.memberId, sig: identity.sign(this.userId, text) }));
      return;
    }
    if (frame.t === 'ready') {
      this.ready = true;
      this.failures = 0;
      this.online = new Set((Array.isArray(frame.online) ? frame.online : []).map(String));
      for (const resolve of this.waiters.splice(0)) resolve(true);
      events.emit('change', { userId: this.userId, poolId: this.poolId });
      return;
    }
    if (frame.t === 'presence') {
      const member = String(frame.member || '');
      if (frame.online) this.online.add(member);
      else this.online.delete(member);
      events.emit('presence', { userId: this.userId, poolId: this.poolId, memberId: member, online: Boolean(frame.online) });
      return;
    }
    if (this.ready) this.endpoint.handleFrame(frame);
  }

  retry() {
    if (this.stopped) return;
    this.failures += 1;
    const delay = Math.min(RETRY_MAX_MS, RETRY_MIN_MS * 2 ** Math.min(this.failures - 1, 6));
    const timer = setTimeout(() => this.connect(), delay * (0.75 + Math.random() * 0.5));
    timer.unref();
  }

  waitReady(timeoutMs) {
    if (this.ready) return Promise.resolve(true);
    return new Promise((resolve) => {
      const timer = setTimeout(() => resolve(false), timeoutMs);
      timer.unref();
      this.waiters.push((value) => { clearTimeout(timer); resolve(value); });
    });
  }

  stop() {
    this.stopped = true;
    this.ready = false;
    this.endpoint.closeAll('Left the pool.');
    this.ws?.close(1000, 'Left the pool');
    for (const resolve of this.waiters.splice(0)) resolve(false);
  }
}

const keyOf = (userId, poolId) => `${userId}|${poolId}`;

/**
 * Keeps a connection to this pool's host while we are a member of it and the
 * host relays; ends it otherwise. options: { checkHost, onUnknownSender }.
 */
function ensure(userId, poolId, options) {
  const key = keyOf(userId, poolId);
  const pool = store.pool(userId, poolId);
  const wanted = pool && !pool.isHost && pool.hostRelay;
  const existing = connectors.get(key);
  if (existing && (!wanted || existing.hostUrl !== pool.hostUrl)) {
    existing.stop();
    connectors.delete(key);
  }
  if (!wanted || connectors.has(key)) return connectors.get(key) || null;
  const connector = new Connector(userId, pool, options);
  connectors.set(key, connector);
  connector.connect();
  return connector;
}

function stop(userId, poolId) {
  connectors.get(keyOf(userId, poolId))?.stop();
  connectors.delete(keyOf(userId, poolId));
}

/** The endpoint to ask partners through, while connected. */
function endpointFor(userId, poolId) {
  const connector = connectors.get(keyOf(userId, poolId));
  return connector?.ready ? connector.endpoint : null;
}

function waitReady(userId, poolId, timeoutMs = 5_000) {
  return connectors.get(keyOf(userId, poolId))?.waitReady(timeoutMs) || Promise.resolve(false);
}

/** true/false when the host told us, null when we do not know (not connected). */
function isOnline(userId, poolId, memberId) {
  const connector = connectors.get(keyOf(userId, poolId));
  return connector?.ready ? connector.online.has(memberId) : null;
}

function isConnected(userId, poolId) {
  return Boolean(connectors.get(keyOf(userId, poolId))?.ready);
}

function stopAllForTests() {
  for (const connector of connectors.values()) connector.stop();
  connectors.clear();
}

module.exports = { events, ensure, stop, endpointFor, waitReady, isOnline, isConnected, relayUrl, stopAllForTests };
