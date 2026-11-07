'use strict';

// Database access for pools, members, invites and partner links. No logic
// beyond reading and writing rows lives here.
const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');

const db = () => getDatabase();

function poolOf(userId) {
  const row = db().prepare('SELECT * FROM pools WHERE user_id = ?').get(userId);
  if (!row) return null;
  return {
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
  };
}

function insertPool(pool) {
  db().prepare(`INSERT INTO pools (user_id, pool_id, name, is_host, host_member_id, host_public_key, host_url, self_url, self_name, share_json)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
    .run(pool.userId, pool.poolId, pool.name, pool.isHost ? 1 : 0, pool.hostMemberId, pool.hostPublicKey, pool.hostUrl, pool.selfUrl, pool.selfName, JSON.stringify(pool.share || {}));
}

function updatePool(userId, fields) {
  const columns = { name: 'name', selfName: 'self_name', selfUrl: 'self_url', share: 'share_json', lastSyncAt: 'last_sync_at', paused: 'paused' };
  for (const [field, column] of Object.entries(columns)) {
    if (fields[field] === undefined) continue;
    const value = field === 'share' ? JSON.stringify(fields[field]) : field === 'paused' ? (fields[field] ? 1 : 0) : fields[field];
    db().prepare(`UPDATE pools SET ${column} = ? WHERE user_id = ?`).run(value, userId);
  }
}

function deletePool(userId) {
  db().prepare('DELETE FROM pools WHERE user_id = ?').run(userId);
  db().prepare('DELETE FROM pool_members WHERE user_id = ?').run(userId);
  db().prepare('DELETE FROM pool_invites WHERE user_id = ?').run(userId);
}

const memberRow = (row) => ({ memberId: row.member_id, name: row.name, url: row.url, publicKey: row.public_key, joinedAt: row.joined_at });

function members(userId) {
  return db().prepare('SELECT * FROM pool_members WHERE user_id = ? ORDER BY joined_at').all(userId).map(memberRow);
}

function member(userId, memberId) {
  const row = db().prepare('SELECT * FROM pool_members WHERE user_id = ? AND member_id = ?').get(userId, memberId);
  return row ? memberRow(row) : null;
}

function upsertMember(userId, { memberId, name, url, publicKey }) {
  db().prepare(`INSERT INTO pool_members (user_id, member_id, name, url, public_key) VALUES (?, ?, ?, ?, ?)
    ON CONFLICT (user_id, member_id) DO UPDATE SET name = excluded.name, url = excluded.url`)
    .run(userId, memberId, name, url, publicKey);
}

function removeMember(userId, memberId) {
  db().prepare('DELETE FROM pool_members WHERE user_id = ? AND member_id = ?').run(userId, memberId);
}

const linkRow = (row) => (row ? {
  id: row.id,
  userId: row.user_id,
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

function links(userId) {
  return db().prepare('SELECT * FROM peer_links WHERE user_id = ? ORDER BY created_at').all(userId).map(linkRow);
}

function link(userId, linkId) {
  return linkRow(db().prepare('SELECT * FROM peer_links WHERE user_id = ? AND id = ?').get(userId, linkId));
}

function linkById(linkId) {
  return linkRow(db().prepare('SELECT * FROM peer_links WHERE id = ?').get(linkId));
}

function linkToMember(userId, memberId) {
  return linkRow(db().prepare('SELECT * FROM peer_links WHERE user_id = ? AND member_id = ?').get(userId, memberId));
}

function linkForAccount(accountId) {
  return linkRow(db().prepare('SELECT * FROM peer_links WHERE account_id = ?').get(accountId));
}

function insertLink(userId, { memberId, name, url, publicKey }) {
  const id = crypto.randomUUID();
  db().prepare('INSERT INTO peer_links (id, user_id, member_id, name, url, public_key) VALUES (?, ?, ?, ?, ?, ?)')
    .run(id, userId, memberId, name, url, publicKey);
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

function invites(userId) {
  return db().prepare('SELECT * FROM pool_invites WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ? ORDER BY created_at DESC')
    .all(userId, Date.now())
    .map((row) => ({ id: row.id, expiresAt: new Date(row.expires_at).toISOString(), createdAt: row.created_at }));
}

function insertInvite(userId, tokenHash, expiresAt) {
  const id = crypto.randomUUID();
  db().prepare('INSERT INTO pool_invites (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)').run(id, userId, tokenHash, expiresAt);
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

function revokeInvite(userId, inviteId) {
  return db().prepare("UPDATE pool_invites SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ? AND user_id = ? AND revoked_at IS NULL")
    .run(inviteId, userId).changes === 1;
}

function activeInviteCount(userId) {
  return db().prepare('SELECT COUNT(*) count FROM pool_invites WHERE user_id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?').get(userId, Date.now()).count;
}

function usersInPools() {
  return db().prepare('SELECT user_id FROM pools').all().map((row) => row.user_id);
}

module.exports = {
  poolOf, insertPool, updatePool, deletePool,
  members, member, upsertMember, removeMember,
  links, link, linkById, linkToMember, linkForAccount, insertLink, updateLink, deleteLink,
  invites, insertInvite, inviteByHash, useInvite, revokeInvite, activeInviteCount,
  usersInPools,
};
