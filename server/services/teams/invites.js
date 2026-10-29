'use strict';

// Invite links add the redeeming account to the owner's team with the
// permissions the link carries. Only a SHA-256 of the token is stored; the
// raw token exists once, in the link handed back to the owner.

const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const { randomToken, sha256 } = require('../../utils/crypto');
const audit = require('./audit');
const members = require('./members');
const permissions = require('./permissions');

const MAX_ACTIVE_INVITES = 25;
const QUERY_PARAM = 'invite';

function buildLink(origin, token) {
  return `${String(origin).replace(/\/+$/, '')}/app/?${QUERY_PARAM}=${token}`;
}

// Accepts the full link or just the token, however it was pasted.
function tokenFromLink(link) {
  const text = String(link || '').trim();
  if (!text) return '';
  try {
    return new URL(text).searchParams.get(QUERY_PARAM) || '';
  } catch {
    return /^[A-Za-z0-9_-]+$/.test(text) ? text : '';
  }
}

function status(row, now = Date.now()) {
  if (row.revoked_at) return 'revoked';
  if (row.expires_at && Date.parse(row.expires_at) <= now) return 'expired';
  if (row.max_uses !== null && row.use_count >= row.max_uses) return 'used';
  return 'active';
}

function publicInvite(row) {
  return {
    id: row.id,
    label: row.label,
    permissions: permissions.parse(row.permissions_json),
    singleUse: row.max_uses === 1,
    useCount: row.use_count,
    expiresAt: row.expires_at,
    createdAt: row.created_at,
    status: status(row),
  };
}

function list(ownerUserId) {
  return getDatabase().prepare('SELECT * FROM team_invites WHERE owner_user_id = ? ORDER BY created_at DESC').all(ownerUserId).map(publicInvite);
}

function create(ownerUserId, { label, permissions: granted, expiresInHours, singleUse }) {
  if (list(ownerUserId).filter((invite) => invite.status === 'active').length >= MAX_ACTIVE_INVITES) {
    throw new HttpError(429, 'INVITE_LIMIT', `You already have ${MAX_ACTIVE_INVITES} open invite links. Revoke some first.`);
  }
  const id = crypto.randomUUID();
  const token = randomToken(24);
  const scope = permissions.normalize(granted);
  const expiresAt = expiresInHours ? new Date(Date.now() + expiresInHours * 3_600_000).toISOString() : null;
  getDatabase().prepare(`INSERT INTO team_invites (id, owner_user_id, token_hash, label, permissions_json, max_uses, expires_at)
    VALUES (?, ?, ?, ?, ?, ?, ?)`).run(id, ownerUserId, sha256(token), String(label || '').trim().slice(0, 80), JSON.stringify(scope), singleUse ? 1 : null, expiresAt);
  audit.record('invite.create', { actorUserId: ownerUserId, detail: { inviteId: id, permissions: scope, singleUse: Boolean(singleUse), expiresAt } });
  return { invite: publicInvite(getDatabase().prepare('SELECT * FROM team_invites WHERE id = ?').get(id)), token };
}

function revoke(ownerUserId, inviteId) {
  const result = getDatabase().prepare("UPDATE team_invites SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ? AND owner_user_id = ? AND revoked_at IS NULL")
    .run(inviteId, ownerUserId);
  if (!result.changes) throw new HttpError(404, 'INVITE_NOT_FOUND', 'This invite link does not exist.');
  audit.record('invite.revoke', { actorUserId: ownerUserId, detail: { inviteId } });
}

// Runs for the preview and again inside the redeeming transaction, so a link
// that changed in between is caught.
function checkRedeemable(userId, link) {
  const token = tokenFromLink(link);
  const row = token ? getDatabase().prepare('SELECT * FROM team_invites WHERE token_hash = ?').get(sha256(token)) : null;
  if (!row) throw new HttpError(404, 'INVITE_INVALID', 'This invite link isn’t valid.');
  const current = status(row);
  if (current === 'revoked') throw new HttpError(410, 'INVITE_REVOKED', 'This invite link was revoked.');
  if (current === 'expired') throw new HttpError(410, 'INVITE_EXPIRED', 'This invite link has expired.');
  if (current === 'used') throw new HttpError(410, 'INVITE_USED', 'This invite link has already been used.');
  members.assertCanJoin(row.owner_user_id, userId);
  return row;
}

function preview(userId, link) {
  const row = checkRedeemable(userId, link);
  return { owner: members.publicUser(row.owner_user_id), permissions: permissions.parse(row.permissions_json), expiresAt: row.expires_at, singleUse: row.max_uses === 1 };
}

function redeem(userId, link) {
  const db = getDatabase();
  db.transaction(() => {
    const row = checkRedeemable(userId, link);
    const consumed = db.prepare(`UPDATE team_invites SET use_count = use_count + 1
      WHERE id = ? AND revoked_at IS NULL AND (max_uses IS NULL OR use_count < max_uses)`).run(row.id);
    if (!consumed.changes) throw new HttpError(410, 'INVITE_USED', 'This invite link has already been used.');
    members.add({ ownerUserId: row.owner_user_id, memberUserId: userId, granted: permissions.parse(row.permissions_json), inviteId: row.id });
  })();
}

module.exports = { MAX_ACTIVE_INVITES, buildLink, list, create, revoke, preview, redeem };
