'use strict';

// The wire format between pool partners. Every control message is an
// envelope { body, signature }:
//   body = { v, type, from, to, ts, nonce, data }
//   signature = Ed25519 over the canonical JSON of body (sorted keys)
// A message is accepted only with the right type and version, a timestamp
// within a few minutes and a nonce not seen before (replays fail), and a
// signature by the key the receiver expects.
const crypto = require('node:crypto');
const identity = require('./identity');
const { HttpError } = require('../../middleware/error_handler');

const VERSION = 1;
const MAX_SKEW_MS = 5 * 60_000;
const seenNonces = new Map();

// JSON with object keys sorted at every level, so both sides sign the same bytes.
function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).filter((key) => value[key] !== undefined).sort().map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`;
  }
  return JSON.stringify(value ?? null);
}

function envelope(userId, type, data, to = null) {
  const self = identity.ensure(userId);
  const body = { v: VERSION, type, from: self.memberId, to, ts: Date.now(), nonce: crypto.randomBytes(16).toString('base64url'), data };
  return { body, signature: identity.sign(userId, canonical(body)) };
}

function rememberNonce(nonce, now) {
  for (const [value, at] of seenNonces) if (now - at > MAX_SKEW_MS * 2) seenNonces.delete(value);
  if (seenNonces.has(nonce)) return false;
  seenNonces.set(nonce, now);
  return true;
}

const reject = (message) => new HttpError(401, 'POOL_SIGNATURE_INVALID', message);

/**
 * Checks an envelope: version, type, freshness, nonce and signature (by
 * publicKey). Returns the body.
 */
function open(message, { type, publicKey, now = Date.now() }) {
  const body = message?.body;
  if (!body || typeof body !== 'object') throw new HttpError(400, 'POOL_MESSAGE_INVALID', 'The message is malformed.');
  if (body.v !== VERSION) throw new HttpError(400, 'POOL_VERSION_MISMATCH', 'The other NeoRouter speaks another pool protocol version. Update both to the latest release.');
  if (body.type !== type) throw new HttpError(400, 'POOL_MESSAGE_INVALID', 'Unexpected message type.');
  if (typeof body.ts !== 'number' || Math.abs(now - body.ts) > MAX_SKEW_MS) throw reject('The message is too old, or the clocks of the two servers differ by more than five minutes.');
  if (!identity.verify(publicKey, canonical(body), message.signature)) throw reject('The signature does not match.');
  if (typeof body.nonce !== 'string' || !rememberNonce(body.nonce, now)) throw reject('This message was already received.');
  return body;
}

// A detached signature over a plain object (introduction tickets, member lists).
function signObject(userId, value) {
  return identity.sign(userId, canonical(value));
}

function verifyObject(publicKey, value, signature) {
  return identity.verify(publicKey, canonical(value), signature);
}

module.exports = { VERSION, canonical, envelope, open, signObject, verifyObject };
