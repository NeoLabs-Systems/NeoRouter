'use strict';

// Each person taking part in a pool has a signing key pair (Ed25519). Partners
// recognise each other by the public key, not by the address, and every
// control message (join, introduction, member list, leaving) is signed with it.
// A second key pair (X25519) seals what travels through a pool's host
// (relay/seal.js); its public half is signed with the pool key, so partners
// know it belongs to this person. Both private keys are stored encrypted
// with the instance's master key.
const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { encryptString, decryptString } = require('../../utils/crypto');

const cache = new Map();

const BOX_LABEL = 'neorouter-pool-box:';
const privateKeyFrom = (encrypted) => crypto.createPrivateKey({ key: Buffer.from(decryptString(encrypted), 'base64'), format: 'der', type: 'pkcs8' });

function toRecord(row) {
  return {
    userId: row.user_id,
    memberId: row.member_id,
    publicKey: row.public_key,
    privateKey: privateKeyFrom(row.private_key_encrypted),
    boxKey: row.box_public_key || null,
    boxPrivateKey: row.box_private_key_encrypted ? privateKeyFrom(row.box_private_key_encrypted) : null,
  };
}

// Adds the sealing key pair to an identity made before it existed.
function withBoxKey(record) {
  if (record.boxKey) return record;
  const { publicKey, privateKey } = crypto.generateKeyPairSync('x25519');
  getDatabase().prepare('UPDATE pool_identities SET box_public_key = ?, box_private_key_encrypted = ? WHERE user_id = ? AND box_public_key IS NULL')
    .run(publicKey.export({ format: 'der', type: 'spki' }).toString('base64'), encryptString(privateKey.export({ format: 'der', type: 'pkcs8' }).toString('base64')), record.userId);
  cache.delete(record.userId);
  return get(record.userId);
}

function get(userId) {
  if (cache.has(userId)) return cache.get(userId);
  const row = getDatabase().prepare('SELECT * FROM pool_identities WHERE user_id = ?').get(userId);
  if (!row) return null;
  const record = toRecord(row);
  cache.set(userId, record);
  return record;
}

function ensure(userId) {
  const existing = get(userId);
  if (existing) return withBoxKey(existing);
  const { publicKey, privateKey } = crypto.generateKeyPairSync('ed25519');
  const publicDer = publicKey.export({ format: 'der', type: 'spki' }).toString('base64');
  const privateDer = privateKey.export({ format: 'der', type: 'pkcs8' }).toString('base64');
  getDatabase().prepare('INSERT INTO pool_identities (user_id, member_id, public_key, private_key_encrypted) VALUES (?, ?, ?, ?)')
    .run(userId, crypto.randomUUID(), publicDer, encryptString(privateDer));
  return withBoxKey(get(userId));
}

/** This person's sealing key and its signature by their pool key, as partners receive them. */
function boxCard(userId) {
  const me = ensure(userId);
  return { boxKey: me.boxKey, boxSig: sign(userId, BOX_LABEL + me.boxKey) };
}

/** Whether boxSig shows that boxKey belongs to the holder of publicKey. */
function verifyBoxKey(publicKey, boxKey, boxSig) {
  return typeof boxKey === 'string' && /^[A-Za-z0-9+/=]{40,120}$/.test(boxKey) && verify(publicKey, BOX_LABEL + boxKey, boxSig);
}

function userForMember(memberId) {
  return getDatabase().prepare('SELECT user_id FROM pool_identities WHERE member_id = ?').get(String(memberId || ''))?.user_id || null;
}

function sign(userId, bytes) {
  return crypto.sign(null, Buffer.from(bytes), ensure(userId).privateKey).toString('base64');
}

function verify(publicKey, bytes, signature) {
  try {
    const key = crypto.createPublicKey({ key: Buffer.from(String(publicKey), 'base64'), format: 'der', type: 'spki' });
    if (key.asymmetricKeyType !== 'ed25519') return false;
    return crypto.verify(null, Buffer.from(bytes), key, Buffer.from(String(signature || ''), 'base64'));
  } catch {
    return false;
  }
}

function forgetForTests() {
  cache.clear();
}

module.exports = { ensure, get, userForMember, sign, verify, boxCard, verifyBoxKey, forgetForTests };
