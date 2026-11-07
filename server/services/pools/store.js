'use strict';

// Database access for pools, members, invites, shared connections and partner
// links. A person may be in several pools; everything is keyed by (person,
// pool). No logic beyond reading and writing rows lives here.
const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');

const db = () => getDatabase();

const poolRow = (row) => (row ? {
  userId: row.user_id,
  poolId: row.pool_id,
  name: row.name,
  isHost: row.is_host === 1,
  hostMemberId: row.host_member_id,
  hostPublicKey: row.host_public_key,
  hostUrl: row.host_url,
  selfUrl: row.self_url,
  selfName: row.self_name,
  share: JSON.parse(row.share_json || '{}'),
  paused: row.paused === 1,
  joinedAt: row.joined_at,
  lastSyncAt: row.last_sync_at,
} : null);

function pool(userId, poolId) {
  return poolRow(db().prepare('SELECT * FROM pools WHERE user_id = ? AND pool_id = ?').get(userId, String(poolId || '')));
}

function pools(userId) {
  return db().prepare('SELECT * FROM pools WHERE user_id = ? ORDER BY joined_at, rowid').all(userId).map(poolRow);
}

function insertPool(value) {
  db().prepare(`INSERT INTO pools (user_id, pool_id, name, is_host, host_member_id, host_public_key, host_url, self_url, self_name, share_json)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
    .run(value.userId, value.poolId, value.name, value.isHost ? 1 : 0, value.hostMemberId, value.hostPublicKey, value.hostUrl, value.selfUrl, value.selfName, JSON.stringify(value.share || {}));
}

function updatePool(userId, poolId, fields) {
  const columns = { name: 'name', selfName: 'self_name', selfUrl: 'self_url', share: 'share_json', lastSyncAt: 'last_sync_at', paused: 'paused' };
  for (const [field, column] of Object.entries(columns)) {
    if (fields[field] === undefined) continue;
    const value = field === 'share' ? JSON.stringify(fields[field]) : field === 'paused' ? (fields[field] ? 1 : 0) : fields[field];
    db().prepare(`UPDATE pools SET ${column} = ? WHERE user_id = ? AND pool_id = ?`).run(value, userId, poolId);
  }
}

function deletePool(userId, poolId) {
  for (const table of ['pools', 'pool_members', 'pool_invites', 'pool_shared_accounts']) {
    db().prepare(`DELETE FROM ${table} WHERE user_id = ? AND pool_id = ?`).run(userId, poolId);
  }
}

const memberRow = (row) => ({ memberId: row.member_id, name: row.name, url: row.url, publicKey: row.public_key, joinedAt: row.joined_at });

function members(userId, poolId) {
  return db().prepare('SELECT * FROM pool_members WHERE user_id = ? AND pool_id = ? ORDER BY joined_at, rowid').all(userId, poolId).map(memberRow);
}

function member(userId, poolId, memberId) {
  const row = db().prepare('SELECT * FROM pool_members WHERE user_id = ? AND pool_id = ? AND member_id = ?').get(userId, poolId, memberId);
  return row ? memberRow(row) : null;
}

function upsertMember(userId, poolId, { memberId, name, url, publicKey }) {
  db().prepare(`INSERT INTO pool_members (user_id, pool_id, member_id, name, url, public_key) VALUES (?, ?, ?, ?, ?, ?)
    ON CONFLICT (user_id, pool_id, member_id) DO UPDATE SET name = excluded.name, url = excluded.url`)
    .run(userId, poolId, memberId, name, url, publicKey);
}

function removeMember(userId, poolId, memberId) {
  db().prepare('DELETE FROM pool_members WHERE user_id = ? AND pool_id = ? AND member_id = ?').run(userId, poolId, memberId);
}

const linkRow = (row) => (row ? {
  id: row.id,
  userId: row.user_id,
  poolId: row.pool_id,
  memberId: row.member_id,
  name: row.name,
  url: row.url,
  publicKey: row.public_key,
  accountId: row.account_id,
  routerKeyId: row.router_key_id,
  status: row.status,
  lastError: row.last_error,
  createdAt: row.created_at,
} : null);

function links(userId, poolId) {
  return db().prepare('SELECT * FROM peer_links WHERE user_id = ? AND pool_id = ? ORDER BY created_at').all(userId, poolId).map(linkRow);
}

function link(userId, linkId) {
  return linkRow(db().prepare('SELECT * FROM peer_links WHERE user_id = ? AND id = ?').get(userId, linkId));
}

function linkById(linkId) {
  return linkRow(db().prepare('SELECT * FROM peer_links WHERE id = ?').get(linkId));
}

function linkToMember(userId, poolId, memberId) {
  return linkRow(db().prepare('SELECT * FROM peer_links WHERE user_id = ? AND pool_id = ? AND member_id = ?').get(userId, poolId, memberId));
}

function linkForAccount(accountId) {
  return linkRow(db().prepare('SELECT * FROM peer_links WHERE account_id = ?').get(accountId));
}

function insertLink(userId, poolId, { memberId, name, url, publicKey }) {
  const id = crypto.randomUUID();
  db().prepare('INSERT INTO peer_links (id, user_id, pool_id, member_id, name, url, public_key) VALUES (?, ?, ?, ?, ?, ?, ?)')
    .run(id, userId, poolId, memberId, name, url, publicKey);
  return link(userId, id);
}

function updateLink(linkId, fields) {
  const columns = { accountId: 'account_id', routerKeyId: 'router_key_id', status: 'status', lastError: 'last_error', name: 'name', url: 'url' };
  for (const [field, column] of Object.entries(columns)) {
    if (fields[field] !== undefined) db().prepare(`UPDATE peer_links SET ${column} = ? WHERE id = ?`).run(fields[field], linkId);
  }
}

function deleteLink(linkId) {
  db().prepare('DELETE FROM peer_links WHERE id = ?').run(linkId);
}

function invites(userId, poolId) {
  return db().prepare('SELECT * FROM pool_invites WHERE user_id = ? AND pool_id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ? ORDER BY created_at DESC')
    .all(userId, poolId, Date.now())
    .map((row) => ({ id: row.id, expiresAt: new Date(row.expires_at).toISOString(), createdAt: row.created_at }));
}

function insertInvite(userId, poolId, tokenHash, expiresAt) {
  const id = crypto.randomUUID();
  db().prepare('INSERT INTO pool_invites (id, user_id, pool_id, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)').run(id, userId, poolId, tokenHash, expiresAt);
  return id;
}

function inviteByHash(tokenHash) {
  return db().prepare('SELECT * FROM pool_invites WHERE token_hash = ?').get(tokenHash) || null;
}

// Marks the invite used, once: false when someone else used it first.
function useInvite(inviteId, memberId) {
  return db().prepare("UPDATE pool_invites SET used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'), used_by = ? WHERE id = ? AND used_at IS NULL")
    .run(memberId, inviteId).changes === 1;
}

function revokeInvite(userId, poolId, inviteId) {
  return db().prepare("UPDATE pool_invites SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ? AND user_id = ? AND pool_id = ? AND revoked_at IS NULL")
    .run(inviteId, userId, poolId).changes === 1;
}

function activeInviteCount(userId, poolId) {
  return db().prepare('SELECT COUNT(*) count FROM pool_invites WHERE user_id = ? AND pool_id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?').get(userId, poolId, Date.now()).count;
}

// The connections shared in a pool, each at a level: free, plan or paid.
function sharedAccountIds(userId, poolId) {
  return db().prepare('SELECT account_id FROM pool_shared_accounts WHERE user_id = ? AND pool_id = ?').all(userId, poolId).map((row) => row.account_id);
}

function shareLevels(userId, poolId) {
  return Object.fromEntries(db().prepare('SELECT account_id, share_level FROM pool_shared_accounts WHERE user_id = ? AND pool_id = ?').all(userId, poolId).map((row) => [row.account_id, row.share_level]));
}

// shared: false removes it; true adds it (at `level`, or keeps its level when left out).
function setShare(userId, poolId, accountId, { shared, level } = {}) {
  const where = 'WHERE user_id = ? AND pool_id = ? AND account_id = ?';
  if (shared === false) {
    db().prepare(`DELETE FROM pool_shared_accounts ${where}`).run(userId, poolId, accountId);
    return;
  }
  db().prepare('INSERT OR IGNORE INTO pool_shared_accounts (user_id, pool_id, account_id) VALUES (?, ?, ?)').run(userId, poolId, accountId);
  if (level) db().prepare(`UPDATE pool_shared_accounts SET share_level = ? ${where}`).run(level, userId, poolId, accountId);
}

// The level a connection is shared at in a pool, or null.
function shareLevelOf(poolId, accountId) {
  return db().prepare('SELECT share_level FROM pool_shared_accounts WHERE pool_id = ? AND account_id = ?').get(poolId, accountId)?.share_level || null;
}

function poolMemberships() {
  return db().prepare('SELECT user_id, pool_id FROM pools').all().map((row) => ({ userId: row.user_id, poolId: row.pool_id }));
}

module.exports = {
  pool, pools, insertPool, updatePool, deletePool,
  members, member, upsertMember, removeMember,
  links, link, linkById, linkToMember, linkForAccount, insertLink, updateLink, deleteLink,
  invites, insertInvite, inviteByHash, useInvite, revokeInvite, activeInviteCount,
  sharedAccountIds, shareLevels, setShare, shareLevelOf, poolMemberships,
};
