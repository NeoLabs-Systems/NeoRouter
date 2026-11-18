'use strict';

// What travels through a pool's host is sealed between the two members, so
// the host passes it along without being able to read or change it.
//
// - Each pair of members shares a key: X25519 between their sealing keys
//   (identity.boxCard, signed with each one's pool key), stretched with HKDF
//   and bound to the pool and both member ids.
// - Each stream (one request and its answer) gets two keys from it, one per
//   direction: the request's from the asker's random stream id, the answer's
//   from that and fresh randomness of the answerer's own. A replayed request
//   therefore never makes the answerer reuse a key and nonce.
// - The pair key is static (no forward secrecy), and it is only as good as the
//   sealing keys the host introduced: a host could lie when it first lists a
//   member, as it could about that member's pool key. Later lists cannot
//   swap a known member's key (pool_service rememberMember).
// - Every frame is AES-256-GCM with its sequence number as the nonce, and the
//   pool, sender, receiver, stream, number and frame type as associated data.
//   Frames that are replayed, reordered, moved to another stream or partner,
//   or cut off before the sealed end therefore fail to open.
const crypto = require('node:crypto');

const KEY_BYTES = 32;
const TAG_BYTES = 16;
const pairCache = new Map();

function publicKeyFrom(boxKey) {
  const key = crypto.createPublicKey({ key: Buffer.from(String(boxKey), 'base64'), format: 'der', type: 'spki' });
  if (key.asymmetricKeyType !== 'x25519') throw new Error('Not a sealing key.');
  return key;
}

/** The key two members share in one pool (the same on both sides). */
function pairKey({ privateKey, ownBoxKey, ownMemberId }, { boxKey, memberId }, poolId) {
  const id = `${poolId}|${ownMemberId}|${ownBoxKey}|${memberId}|${boxKey}`;
  if (pairCache.has(id)) return pairCache.get(id);
  const shared = crypto.diffieHellman({ privateKey, publicKey: publicKeyFrom(boxKey) });
  const ids = [ownMemberId, memberId].map((value) => String(value).toLowerCase()).sort().join('|');
  const key = Buffer.from(crypto.hkdfSync('sha256', shared, Buffer.from(String(poolId)), Buffer.from(`neorouter pool relay v1|${ids}`), KEY_BYTES));
  if (pairCache.size > 1000) pairCache.clear();
  pairCache.set(id, key);
  return key;
}

function nonceFor(seq) {
  const nonce = Buffer.alloc(12);
  nonce.writeBigUInt64BE(BigInt(seq), 4);
  return nonce;
}

/**
 * Seals and opens one direction of one stream. `from` and `to` are member
 * ids; seq starts at 0 and must go up by one with every frame.
 */
function direction(pair, { poolId, streamId, from, to, salt = '' }) {
  const key = Buffer.from(crypto.hkdfSync('sha256', pair, Buffer.from(`${streamId}|${salt}`), Buffer.from(`direction|${from}>${to}`), KEY_BYTES));
  const aad = (seq, type) => Buffer.from(`${poolId}|${from}|${to}|${streamId}|${seq}|${type}`);
  let sent = 0;
  let received = 0;
  return {
    seal(type, payload) {
      const seq = sent;
      sent += 1;
      const cipher = crypto.createCipheriv('aes-256-gcm', key, nonceFor(seq));
      cipher.setAAD(aad(seq, type));
      const body = Buffer.concat([cipher.update(Buffer.from(payload)), cipher.final(), cipher.getAuthTag()]);
      return { seq, d: body.toString('base64') };
    },
    open(type, seq, data) {
      if (seq !== received) throw new Error('A relayed frame is out of order.');
      const body = Buffer.from(String(data || ''), 'base64');
      if (body.length < TAG_BYTES) throw new Error('A relayed frame is malformed.');
      const decipher = crypto.createDecipheriv('aes-256-gcm', key, nonceFor(seq));
      decipher.setAAD(aad(seq, type));
      decipher.setAuthTag(body.subarray(body.length - TAG_BYTES));
      const plain = Buffer.concat([decipher.update(body.subarray(0, body.length - TAG_BYTES)), decipher.final()]);
      received += 1;
      return plain;
    },
  };
}

function resetForTests() {
  pairCache.clear();
}

module.exports = { pairKey, direction, resetForTests };
