'use strict';

// Each person taking part in a pool has a signing key pair (Ed25519). Partners
// recognise each other by the public key, not by the address, and every
// control message (join, introduction, member list, leaving) is signed with it.
// The private key is stored encrypted with the instance's master key.
const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { encryptString, decryptString } = require('../../utils/crypto');

const cache = new Map();

function toRecord(row) {
  return {
    userId: row.user_id,
    memberId: row.member_id,
    publicKey: row.public_key,
    privateKey: crypto.createPrivateKey({ key: Buffer.from(decryptString(row.private_key_encrypted), 'base64'), format: 'der', type: 'pkcs8' }),
  };
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
  if (existing) return existing;
  const { publicKey, privateKey } = crypto.generateKeyPairSync('ed25519');
  const publicDer = publicKey.export({ format: 'der', type: 'spki' }).toString('base64');
  const privateDer = privateKey.export({ format: 'der', type: 'pkcs8' }).toString('base64');
  getDatabase().prepare('INSERT INTO pool_identities (user_id, member_id, public_key, private_key_encrypted) VALUES (?, ?, ?, ?)')
    .run(userId, crypto.randomUUID(), publicDer, encryptString(privateDer));
  return get(userId);
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

module.exports = { ensure, get, userForMember, sign, verify, forgetForTests };
