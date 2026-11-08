'use strict';

// The host's side of the relay: members without a public address keep a
// WebSocket open to /federation/v1/relay, and the hub passes stream frames
// between the two members of a stream. It reads only the routing fields;
// the requests and answers themselves are sealed between the members
// (seal.js), so the host can neither read nor change them.
//
// Logging in: the hub sends a random challenge; the member signs it with its
// pool key (the one in the host's member list). Only current members of a
// pool this instance hosts, with relaying on, get in; a newer connection of
// the same member replaces the older one.
const crypto = require('node:crypto');
const { EventEmitter } = require('node:events');
const { WebSocketServer } = require('ws');
const identity = require('../identity');
const store = require('../store');
const { RelayEndpoint } = require('./endpoint');
const { LIMITS, helloText, parse, validStreamFrame, encode } = require('./frames');
const { getConfig } = require('../../../config/env');

const PATH = '/federation/v1/relay';
// Failed logins per client address and minute before it has to wait.
const FAILED_LOGINS_PER_MINUTE = 20;

const events = new EventEmitter();
const connections = new Map(); // `${poolId}|${memberId}` → connection
const streams = new Map(); // stream id → { asker, answerer, at }
const localEndpoints = new Map(); // `${userId}|${poolId}` → { endpoint, connection }
const failedLogins = new Map(); // client address → [times]
let sweeper = null;

const keyOf = (poolId, memberId) => `${poolId}|${memberId}`;

function online(poolId) {
  return [...connections.values()].filter((conn) => conn.poolId === poolId).map((conn) => conn.memberId);
}

function isOnline(poolId, memberId) {
  return connections.has(keyOf(poolId, memberId));
}

// ------------------------------------------------------------------ passing frames along

function endStream(id, notify) {
  const stream = streams.get(id);
  if (!stream) return;
  streams.delete(id);
  stream.asker.streams.delete(id);
  stream.answerer.streams.delete(id);
  for (const [conn, frame] of notify) conn.deliver(frame);
}

function route(from, frame) {
  if (!validStreamFrame(frame)) return;
  if (frame.t === 'open') {
    const answerer = connections.get(keyOf(from.poolId, String(frame.to || '')));
    if (streams.has(frame.s) || from.streams.size >= LIMITS.streamsPerConnection) {
      from.deliver({ t: 'fail', s: frame.s, code: 'busy' });
      return;
    }
    if (!answerer || answerer === from) {
      from.deliver({ t: 'fail', s: frame.s, code: 'unreachable' });
      return;
    }
    streams.set(frame.s, { asker: from, answerer, at: Date.now() });
    from.streams.add(frame.s);
    answerer.streams.add(frame.s);
    answerer.deliver({ t: 'open', s: frame.s, from: from.memberId, seq: frame.seq, d: frame.d });
    return;
  }
  const stream = streams.get(frame.s);
  if (!stream || (stream.asker !== from && stream.answerer !== from)) return;
  stream.at = Date.now();
  const other = stream.asker === from ? stream.answerer : stream.asker;
  const forwarded = frame.t === 'cancel' || frame.t === 'fail'
    ? { t: frame.t, s: frame.s, ...(frame.code ? { code: frame.code } : {}) }
    : { t: frame.t, s: frame.s, seq: frame.seq, d: frame.d, ...(frame.r ? { r: frame.r } : {}) };
  if (['end', 'cancel', 'fail'].includes(frame.t)) endStream(frame.s, [[other, forwarded]]);
  // A member that does not read its answers would make the host buffer without end.
  else if (other.ws && other.ws.bufferedAmount > LIMITS.bufferedBytes) endStream(frame.s, [[other, { t: 'fail', s: frame.s, code: 'slow' }], [from, { t: 'cancel', s: frame.s }]]);
  else other.deliver(forwarded);
}

// A connection went away: the other side of each of its streams is told.
function dropStreams(conn) {
  for (const id of [...conn.streams]) {
    const stream = streams.get(id);
    if (!stream) continue;
    endStream(id, [stream.asker === conn ? [stream.answerer, { t: 'cancel', s: id }] : [stream.asker, { t: 'fail', s: id, code: 'unreachable' }]]);
  }
}

function broadcastPresence(poolId, memberId, isUp) {
  for (const conn of connections.values()) {
    if (conn.poolId === poolId && conn.memberId !== memberId && conn.ws) conn.deliver({ t: 'presence', member: memberId, online: isUp });
  }
  events.emit('presence', { poolId, memberId, online: isUp });
}

// ------------------------------------------------------------------ members' connections

function register(conn) {
  const key = keyOf(conn.poolId, conn.memberId);
  const previous = connections.get(key);
  if (previous) {
    connections.delete(key);
    dropStreams(previous);
    previous.ws.close(4000, 'Replaced by a newer connection');
  }
  connections.set(key, conn);
  conn.deliver({ t: 'ready', online: online(conn.poolId).filter((id) => id !== conn.memberId) });
  broadcastPresence(conn.poolId, conn.memberId, true);
}

function unregister(conn) {
  const key = keyOf(conn.poolId, conn.memberId);
  if (connections.get(key) !== conn) return;
  connections.delete(key);
  dropStreams(conn);
  broadcastPresence(conn.poolId, conn.memberId, false);
}

function login(ws, nonce, frame) {
  if (frame?.t !== 'hello') return null;
  const pool = store.hostedPool(String(frame.pool || ''));
  if (!pool || !pool.relayEnabled) return null;
  const member = store.member(pool.userId, pool.poolId, String(frame.member || ''));
  if (!member || member.memberId === pool.hostMemberId) return null;
  const text = helloText({ nonce, pool: pool.poolId, member: member.memberId, host: pool.hostMemberId });
  if (!identity.verify(member.publicKey, text, frame.sig)) return null;
  return { ws, poolId: pool.poolId, memberId: member.memberId, streams: new Set(), alive: true, deliver: (out) => sendTo(ws, out) };
}

function sendTo(ws, frame) {
  return new Promise((resolve) => {
    if (ws.readyState !== ws.OPEN) return resolve();
    ws.send(encode(frame), () => resolve());
  });
}

function accept(ws, client) {
  const nonce = crypto.randomBytes(24).toString('base64url');
  let conn = null;
  const timer = setTimeout(() => ws.close(4401, 'Log in first'), LIMITS.helloMs);
  ws.on('pong', () => { if (conn) conn.alive = true; });
  ws.on('message', (data, isBinary) => {
    if (!conn && data.length > LIMITS.helloBytes) return ws.close(4400, 'Log in first');
    const frame = isBinary ? null : parse(data.toString('utf8'));
    if (!frame) return ws.close(4400, 'Malformed frame');
    if (!conn) {
      conn = login(ws, nonce, frame);
      clearTimeout(timer);
      if (!conn) {
        noteFailedLogin(client);
        return ws.close(4403, 'Not a member of a pool relayed here');
      }
      return register(conn);
    }
    return route(conn, frame);
  });
  ws.on('close', () => {
    clearTimeout(timer);
    if (conn) unregister(conn);
  });
  ws.on('error', () => ws.terminate());
  sendTo(ws, { t: 'challenge', nonce });
}

// The client's address, as the rest of the app sees it (behind a proxy when trusted).
function clientOf(req) {
  const forwarded = getConfig().trustProxy ? String(req.headers['x-forwarded-for'] || '').split(',').pop().trim() : '';
  return forwarded || req.socket.remoteAddress || '';
}

const recentFailures = (client, now) => (failedLogins.get(client) || []).filter((at) => now - at < 60_000);

function noteFailedLogin(client, now = Date.now()) {
  if (failedLogins.size > 10_000) for (const [key, times] of failedLogins) if (!times.some((at) => now - at < 60_000)) failedLogins.delete(key);
  failedLogins.set(client, [...recentFailures(client, now), now]);
}

// Only clients that keep failing to log in wait; members logging in are never held up.
const mayTryLogin = (client, now = Date.now()) => recentFailures(client, now).length < FAILED_LOGINS_PER_MINUTE;

// Heartbeats find connections that died without closing; old streams end.
function sweep() {
  for (const conn of connections.values()) {
    if (!conn.ws) continue;
    if (!conn.alive) {
      conn.ws.terminate();
      continue;
    }
    conn.alive = false;
    conn.ws.ping();
  }
  const now = Date.now();
  for (const [id, stream] of streams) {
    if (now - stream.at > LIMITS.streamIdleMs) endStream(id, [[stream.asker, { t: 'fail', s: id, code: 'timeout' }], [stream.answerer, { t: 'cancel', s: id }]]);
  }
}

/** Serves the relay on the server's WebSocket upgrades (beside the live gateway). */
function attach(server) {
  const wss = new WebSocketServer({ noServer: true, maxPayload: LIMITS.frameBytes });
  server.on('upgrade', (req, socket, head) => {
    let pathname;
    try { pathname = new URL(req.url, 'http://local').pathname; } catch { return; }
    if (pathname !== PATH) return;
    req.upgradeClaimed = true;
    const client = clientOf(req);
    if (!mayTryLogin(client)) {
      socket.end('HTTP/1.1 429 Too Many Requests\r\nConnection: close\r\n\r\n');
      return;
    }
    wss.handleUpgrade(req, socket, head, (ws) => accept(ws, client));
  });
  if (!sweeper) {
    sweeper = setInterval(sweep, LIMITS.heartbeatMs);
    sweeper.unref();
  }
  server.on('close', () => wss.close());
  return wss;
}

// ------------------------------------------------------------------ the host's own end

/**
 * The host's own end in one of its pools: it asks relayed members through
 * the hub directly, without a WebSocket.
 */
function localEndpoint(userId, poolId) {
  const key = `${userId}|${poolId}`;
  if (localEndpoints.has(key)) return localEndpoints.get(key).endpoint;
  const memberId = identity.ensure(userId).memberId;
  const conn = { ws: null, poolId, memberId, streams: new Set(), deliver: async (frame) => { endpoint.handleFrame(frame); } };
  const endpoint = new RelayEndpoint({ userId, poolId, send: async (frame) => route(conn, frame) });
  localEndpoints.set(key, { endpoint, conn });
  return endpoint;
}

/** Ends every connection of a pool (relaying turned off, pool closed). */
function closePool(poolId) {
  for (const conn of [...connections.values()]) if (conn.poolId === poolId) conn.ws.close(4410, 'Relaying stopped for this pool');
  for (const [key, local] of localEndpoints) {
    if (local.conn.poolId === poolId) {
      dropStreams(local.conn);
      local.endpoint.closeAll('Relaying stopped for this pool.');
      localEndpoints.delete(key);
    }
  }
}

/** Ends one member's connection (removed from the pool). */
function closeMember(poolId, memberId) {
  connections.get(keyOf(poolId, memberId))?.ws.close(4410, 'No longer in this pool');
}

function forTests() {
  return { connections, streams, localEndpoints, route, sweep };
}

module.exports = { PATH, events, attach, localEndpoint, isOnline, online, closePool, closeMember, forTests };
