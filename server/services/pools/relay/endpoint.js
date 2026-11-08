'use strict';

// One person's end of the relay in one pool. It asks partners (request) and
// answers them (incoming 'open' frames), over a channel that is either a
// WebSocket to the host (connector.js) or, on the host itself, the hub
// (hub.js). Everything it sends is sealed for the one partner (seal.js).
//
// Answering is deliberately narrow: only the calls a partner can make over the
// network anyway (models, chat completions and the pool messages addressed to
// a member) are handed to this NeoRouter (loopback.js), with a short list of
// headers. Anything else is refused before it reaches the app.
const crypto = require('node:crypto');
const identity = require('../identity');
const store = require('../store');
const seal = require('./seal');
const loopback = require('./loopback');
const { LIMITS, validStreamFrame } = require('./frames');

const ALLOWED = [
  ['GET', /^\/v1\/models(\?[\w=&.%-]*)?$/],
  ['POST', /^\/v1\/chat\/completions$/],
  ['POST', /^\/federation\/v1\/(introduce|leave|remove|dissolve|shared_changed)$/],
];
const REQUEST_HEADERS = new Set(['authorization', 'content-type', 'accept', 'user-agent', 'x-neorouter-hop']);
const RESPONSE_HEADERS = /^(content-type|retry-after|x-ratelimit-[a-z-]+)$/;

const allowed = (method, path) => ALLOWED.some(([verb, pattern]) => verb === method && pattern.test(path));

function pickHeaders(headers, keep) {
  const picked = {};
  for (const [name, value] of Object.entries(headers || {})) {
    const key = name.toLowerCase();
    if (keep(key) && value !== undefined) picked[key] = String(Array.isArray(value) ? value.join(', ') : value).slice(0, 8192);
  }
  return picked;
}

// An error the HTTP layer reads as "could not reach the partner".
function unreachable(message, code = 'RELAY_UNREACHABLE') {
  const error = new Error(message);
  error.code = code;
  return error;
}

class RelayEndpoint {
  /**
   * send(frame) delivers a frame to the hub and resolves once it is written.
   * onUnknownSender(memberId) may refresh the member list once when a
   * partner's sealing key is not known yet.
   */
  constructor({ userId, poolId, send, onUnknownSender = null }) {
    this.userId = userId;
    this.poolId = poolId;
    this.send = send;
    this.onUnknownSender = onUnknownSender;
    this.streams = new Map();
    // Requests already answered (`${from}|${stream}` → when), so a replay is refused.
    this.answered = new Map();
  }

  own() {
    const me = identity.ensure(this.userId);
    return { privateKey: me.boxPrivateKey, ownBoxKey: me.boxKey, ownMemberId: me.memberId };
  }

  // The request direction of a stream (asker → answerer, keyed by the stream id
  // alone) and the answer direction (answerer → asker, also keyed by the
  // answerer's fresh salt).
  directions(partner, streamId, { asking }) {
    const own = this.own();
    const pair = seal.pairKey(own, partner, this.poolId);
    const [asker, answerer] = asking ? [own.ownMemberId, partner.memberId] : [partner.memberId, own.ownMemberId];
    const base = { poolId: this.poolId, streamId };
    return {
      request: seal.direction(pair, { ...base, from: asker, to: answerer }),
      answer: (salt) => seal.direction(pair, { ...base, from: answerer, to: asker, salt }),
    };
  }

  // Remembers a request id; false when it was seen before (a replay).
  firstTime(id, now = Date.now()) {
    for (const [key, at] of this.answered) if (now - at > LIMITS.requestSkewMs * 2) this.answered.delete(key);
    if (this.answered.has(id)) return false;
    this.answered.set(id, now);
    return true;
  }

  partner(memberId) {
    const member = store.member(this.userId, this.poolId, memberId);
    return member?.boxKey ? member : null;
  }

  // ------------------------------------------------------------------ asking

  /** Sends a request to a partner through the host; resolves with a fetch Response. */
  request(memberId, { method = 'GET', path, headers = {}, body, signal } = {}) {
    const partner = this.partner(memberId);
    if (!partner) return Promise.reject(unreachable('This partner cannot be reached through the host yet.'));
    const bytes = body === undefined || body === null ? Buffer.alloc(0) : Buffer.from(body);
    if (bytes.length > LIMITS.requestBodyBytes) return Promise.reject(unreachable('The request is too large to pass through the host.', 'RELAY_TOO_LARGE'));
    if (signal?.aborted) return Promise.reject(signal.reason);
    const streamId = crypto.randomBytes(16).toString('base64url');
    const keys = this.directions(partner, streamId, { asking: true });
    let answer = null;
    return new Promise((resolve, reject) => {
      let controller = null;
      let headSeen = false;
      const finish = (error) => {
        if (!this.streams.has(streamId)) return;
        this.streams.delete(streamId);
        signal?.removeEventListener('abort', onAbort);
        if (!headSeen) reject(error);
        else if (error) controller?.error(error);
        else controller?.close();
      };
      const onAbort = () => {
        this.send({ t: 'cancel', s: streamId }).catch(() => {});
        finish(signal.reason);
      };
      signal?.addEventListener('abort', onAbort, { once: true });
      this.streams.set(streamId, {
        role: 'asking',
        frame: (frame) => {
          try {
            if (frame.t === 'fail') return finish(unreachable(`The partner could not be reached through the host (${frame.code || 'failed'}).`));
            if (frame.t === 'head' && !headSeen) {
              answer = keys.answer(frame.r);
              const head = JSON.parse(answer.open('head', frame.seq, frame.d).toString('utf8'));
              headSeen = true;
              const stream = new ReadableStream({
                start: (c) => { controller = c; },
                cancel: () => {
                  this.send({ t: 'cancel', s: streamId }).catch(() => {});
                  this.streams.delete(streamId);
                },
              });
              resolve(new Response(stream, { status: Number(head.status) || 502, headers: pickHeaders(head.headers, (key) => RESPONSE_HEADERS.test(key)) }));
              return undefined;
            }
            if (frame.t === 'data' && headSeen) {
              controller?.enqueue(new Uint8Array(answer.open('data', frame.seq, frame.d)));
              return undefined;
            }
            if (frame.t === 'end' && headSeen) {
              answer.open('end', frame.seq, frame.d);
              return finish(null);
            }
            return finish(unreachable('The partner sent an unexpected answer through the host.'));
          } catch (error) {
            // A frame that does not open was changed or replayed on the way: the stream ends.
            this.send({ t: 'cancel', s: streamId }).catch(() => {});
            return finish(unreachable(`An answer through the host could not be verified: ${error.message}`, 'RELAY_SEAL'));
          }
        },
        close: (reason) => finish(unreachable(reason)),
      });
      const payload = JSON.stringify({ ts: Date.now(), method, path, headers: pickHeaders(headers, (key) => REQUEST_HEADERS.has(key)), body: bytes.toString('base64') });
      this.send({ t: 'open', s: streamId, to: memberId, ...keys.request.seal('open', payload) })
        .catch((error) => finish(unreachable(`The host could not be reached: ${error.message}`)));
    });
  }

  // ------------------------------------------------------------------ answering

  // The stream's slot is already taken (handleFrame), so the limit holds while we wait.
  async answer(frame, abort) {
    const streamId = frame.s;
    const refuse = (code) => this.send({ t: 'fail', s: streamId, code }).catch(() => {});
    let partner = this.partner(frame.from);
    if (!partner && this.onUnknownSender) {
      try { await this.onUnknownSender(frame.from); } catch { /* the check below refuses it */ }
      partner = this.partner(frame.from);
    }
    if (!partner) return refuse('unknown_sender');
    const keys = this.directions(partner, streamId, { asking: false });
    let request;
    try {
      request = JSON.parse(keys.request.open('open', frame.seq, frame.d).toString('utf8'));
    } catch {
      return refuse('unreadable');
    }
    // A request the host kept and sends again is refused: too old, or already seen.
    if (!(Math.abs(Date.now() - Number(request?.ts)) <= LIMITS.requestSkewMs) || !this.firstTime(`${partner.memberId}|${streamId}`)) return refuse('replayed');
    const salt = crypto.randomBytes(16).toString('base64url');
    const out = keys.answer(salt);
    let headSent = false;
    const reply = async (type, payload) => {
      const sealed = out.seal(type, payload);
      const frameOut = { t: type, s: streamId, ...sealed, ...(headSent ? {} : { r: salt }) };
      headSent = true;
      return this.send(frameOut);
    };
    try {
      const method = String(request?.method || '').toUpperCase();
      const path = String(request?.path || '');
      if (!allowed(method, path)) {
        await reply('head', JSON.stringify({ status: 403, headers: { 'content-type': 'application/json' } }));
        await reply('data', JSON.stringify({ error: { code: 'POOL_RELAY_REFUSED', message: 'This call is not passed through a pool host.' } }));
        await reply('end', '');
        return;
      }
      const body = Buffer.from(String(request.body || ''), 'base64');
      const headers = { ...pickHeaders(request.headers, (key) => REQUEST_HEADERS.has(key)), 'content-length': String(body.length) };
      const response = await loopback.dispatch({ method, path, headers, body }, abort.signal);
      await reply('head', JSON.stringify({ status: response.statusCode, headers: pickHeaders(response.headers, (key) => RESPONSE_HEADERS.test(key)) }));
      for await (const chunk of response) {
        for (let offset = 0; offset < chunk.length; offset += LIMITS.chunkBytes) await reply('data', chunk.subarray(offset, offset + LIMITS.chunkBytes));
      }
      await reply('end', '');
    } catch {
      if (!abort.signal.aborted) await refuse('failed');
    }
    return undefined;
  }

  // ------------------------------------------------------------------ frames from the hub

  handleFrame(frame) {
    if (!validStreamFrame(frame)) return;
    if (frame.t === 'open') {
      if (!frame.from || this.streams.has(frame.s)) return;
      if (this.streams.size >= LIMITS.streamsPerConnection) {
        this.send({ t: 'fail', s: frame.s, code: 'busy' }).catch(() => {});
        return;
      }
      const abort = new AbortController();
      this.streams.set(frame.s, { role: 'answering', frame: (next) => { if (next.t === 'cancel') abort.abort(); }, close: () => abort.abort() });
      this.answer(frame, abort).finally(() => this.streams.delete(frame.s));
      return;
    }
    this.streams.get(frame.s)?.frame(frame);
  }

  /** The connection went away: every open stream ends. */
  closeAll(reason = 'The connection to the host was lost.') {
    for (const stream of [...this.streams.values()]) stream.close(reason);
    this.streams.clear();
  }
}

module.exports = { RelayEndpoint, allowed };
