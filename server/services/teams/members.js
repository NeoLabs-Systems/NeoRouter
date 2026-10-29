'use strict';

const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const audit = require('./audit');
const permissions = require('./permissions');

// The only account fields teammates see about each other.
function publicUser(userId) {
  if (!userId) return null;
  const row = getDatabase().prepare('SELECT id, username, display_name FROM users WHERE id = ?').get(userId);
  if (!row) return null;
  return { id: row.id, username: row.username, displayName: row.display_name || row.username };
}

function membership(memberUserId) {
  const row = getDatabase().prepare('SELECT * FROM team_members WHERE member_user_id = ?').get(memberUserId);
  if (!row) return null;
  return { ownerUserId: row.owner_user_id, permissions: permissions.parse(row.permissions_json), createdAt: row.created_at };
}

function list(ownerUserId) {
  return getDatabase().prepare('SELECT * FROM team_members WHERE owner_user_id = ? ORDER BY created_at').all(ownerUserId)
    .map((row) => ({ user: publicUser(row.member_user_id), permissions: permissions.parse(row.permissions_json), joinedAt: row.created_at }));
}

function hasMembers(ownerUserId) {
  return Boolean(getDatabase().prepare('SELECT 1 FROM team_members WHERE owner_user_id = ? LIMIT 1').get(ownerUserId));
}

function assertCanJoin(ownerUserId, memberUserId) {
  if (ownerUserId === memberUserId) throw new HttpError(400, 'INVITE_SELF', 'You can’t use your own invite link.');
  const current = membership(memberUserId);
  if (current) {
    const owner = publicUser(current.ownerUserId);
    throw new HttpError(409, 'ALREADY_MEMBER', `You are already in ${owner?.displayName || 'another'} team. Leave it first.`);
  }
}

function add({ ownerUserId, memberUserId, granted, inviteId }) {
  assertCanJoin(ownerUserId, memberUserId);
  getDatabase().prepare('INSERT INTO team_members (member_user_id, owner_user_id, permissions_json, invite_id) VALUES (?, ?, ?, ?)')
    .run(memberUserId, ownerUserId, JSON.stringify(permissions.normalize(granted)), inviteId);
  audit.record('member.join', { actorUserId: memberUserId, subjectUserId: ownerUserId, detail: { inviteId } });
}

function requireMember(ownerUserId, memberUserId) {
  const current = membership(memberUserId);
  if (!current || current.ownerUserId !== ownerUserId) throw new HttpError(404, 'MEMBER_NOT_FOUND', 'This person is not in your team.');
  return current;
}

function setPermissions(ownerUserId, memberUserId, granted) {
  requireMember(ownerUserId, memberUserId);
  const next = permissions.normalize(granted);
  getDatabase().prepare('UPDATE team_members SET permissions_json = ? WHERE member_user_id = ?').run(JSON.stringify(next), memberUserId);
  audit.record('member.permissions', { actorUserId: ownerUserId, subjectUserId: memberUserId, detail: { permissions: next } });
}

function release(ownerUserId, memberUserId) {
  requireMember(ownerUserId, memberUserId);
  getDatabase().prepare('DELETE FROM team_members WHERE member_user_id = ?').run(memberUserId);
  audit.record('member.release', { actorUserId: ownerUserId, subjectUserId: memberUserId });
}

function leave(memberUserId) {
  const current = membership(memberUserId);
  if (!current) throw new HttpError(404, 'NOT_A_MEMBER', 'You are not in a team.');
  getDatabase().prepare('DELETE FROM team_members WHERE member_user_id = ?').run(memberUserId);
  audit.record('member.leave', { actorUserId: memberUserId, subjectUserId: current.ownerUserId });
}

module.exports = { publicUser, membership, list, hasMembers, assertCanJoin, add, setPermissions, release, leave };
