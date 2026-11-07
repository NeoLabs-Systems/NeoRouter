'use strict';

// Pools: people on separate NeoRouters who share their free capacity.
//
// Control goes through the host (who created the pool): it keeps the member
// list and the invites. Requests never go through the host: every two
// members are linked directly (links.js).
//
// Joining with an invite link:
//   1. the joiner's server asks the host for a preview (pool, members)
//   2. it sends a signed "join" with the invite token and a key for the host;
//      the host answers with a key for the joiner, the member list and one
//      signed introduction ticket per other member
//   3. with each ticket the joiner introduces itself to that member, who
//      checks the host's signature and exchanges keys the same way
// Later changes (someone left, was removed, joined while a member was
// offline) are picked up by sync(), which compares with the host's signed
// member list.
const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const { randomToken, sha256 } = require('../../utils/crypto');
const identity = require('./identity');
const protocol = require('./protocol');
const store = require('./store');
const links = require('./links');
const client = require('./peer_client');
const sharePolicy = require('./share_policy');
const peerPrivacy = require('./peer_privacy');
const reachability = require('./reachability');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('pools');

const INVITE_TTL_MS = 7 * 86_400_000;
const MAX_ACTIVE_INVITES = 20;
const MAX_MEMBERS = 20;
const TICKET_TTL_MS = 10 * 60_000;
const TOKEN_PATTERN = /^[A-Za-z0-9_-]{16,128}$/;

const fail = (status, code, message) => new HttpError(status, code, message);

function cleanName(value, fallback) {
  const name = String(value || '').replace(/[\u0000-\u001f]/g, '').replace(/\s+/g, ' ').trim().slice(0, 60);
  return name || fallback;
}

function requirePool(userId) {
  const pool = store.poolOf(userId);
  if (!pool) throw fail(404, 'POOL_NOT_FOUND', 'You are not in a pool.');
  return pool;
}

function requireHost(userId) {
  const pool = requirePool(userId);
  if (!pool.isHost) throw fail(403, 'POOL_HOST_ONLY', 'Only the pool’s host can do this.');
  return pool;
}

function assertNotInPool(userId) {
  if (store.poolOf(userId)) throw fail(409, 'POOL_ALREADY_JOINED', 'You are already in a pool. Leave it first.');
}

function ownOrigin(selfUrl) {
  const origin = client.originOf(selfUrl);
  if (!origin) throw fail(400, 'POOL_URL_UNKNOWN', 'This NeoRouter’s public address is unknown. Set NEOROUTER_PUBLIC_URL so partners can reach it.');
  return origin;
}

// A member as sent by another server, checked field by field.
function sanitizeMember(raw) {
  if (!raw || typeof raw !== 'object') return null;
  const memberId = String(raw.memberId || '');
  const publicKey = String(raw.publicKey || '');
  const url = client.originOf(raw.url);
  if (!/^[0-9a-f-]{36}$/i.test(memberId) || !/^[A-Za-z0-9+/=]{40,200}$/.test(publicKey) || !url) return null;
  return { memberId, name: cleanName(raw.name, 'Partner'), url, publicKey, joinedAt: typeof raw.joinedAt === 'string' ? raw.joinedAt.slice(0, 40) : null };
}

const publicMember = (member) => ({ memberId: member.memberId, name: member.name, url: member.url, publicKey: member.publicKey, joinedAt: member.joinedAt });

function selfMember(userId, pool) {
  const me = identity.ensure(userId);
  return { memberId: me.memberId, name: pool.selfName, url: pool.selfUrl, publicKey: me.publicKey };
}

function assertPeerKey(value) {
  if (typeof value !== 'string' || !/^nrt_[a-f0-9]{8}_[A-Za-z0-9_-]{20,100}$/.test(value)) throw fail(400, 'POOL_MESSAGE_INVALID', 'The partner key is malformed.');
}

// Only these of the person's own connections are shared; all others not.
function setShared(userId, accountIds = []) {
  const wanted = new Set(accountIds.map(String));
  const rows = getDatabase().prepare("SELECT id FROM provider_accounts WHERE user_id = ? AND definition_id != 'neorouter_peer'").all(userId);
  for (const row of rows) sharePolicy.setAccountShared(userId, row.id, wanted.has(row.id));
}

// Own connections with free models: what can be shared.
function shareableAccounts(userId) {
  return getDatabase().prepare(`SELECT a.id, a.label, a.definition_id, a.pool_share, a.enabled,
      (SELECT COUNT(*) FROM models m WHERE m.account_id = a.id AND m.is_free = 1 AND m.available = 1 AND m.capability = 'chat') free_models
    FROM provider_accounts a WHERE a.user_id = ? AND a.definition_id != 'neorouter_peer' ORDER BY a.created_at`).all(userId)
    .filter((row) => row.free_models > 0)
    .map((row) => ({ id: row.id, label: row.label, definitionId: row.definition_id, shared: row.pool_share === 1, enabled: row.enabled === 1, freeModels: row.free_models }));
}

// ---------------------------------------------------------------- creating

function create(userId, { name, selfName, selfUrl, sharedAccountIds = [] }) {
  assertNotInPool(userId);
  const url = ownOrigin(selfUrl);
  const me = identity.ensure(userId);
  const ownName = cleanName(selfName, 'Me');
  store.insertPool({
    userId, poolId: crypto.randomUUID(), name: cleanName(name, 'My pool'), isHost: true,
    hostMemberId: me.memberId, hostPublicKey: me.publicKey, hostUrl: url, selfUrl: url, selfName: ownName, share: {},
  });
  store.upsertMember(userId, { memberId: me.memberId, name: ownName, url, publicKey: me.publicKey });
  setShared(userId, sharedAccountIds);
  return summary(userId);
}

function createInvite(userId) {
  const pool = requireHost(userId);
  if (store.members(userId).length >= MAX_MEMBERS) throw fail(409, 'POOL_FULL', `A pool has at most ${MAX_MEMBERS} members.`);
  if (store.activeInviteCount(userId) >= MAX_ACTIVE_INVITES) throw fail(429, 'POOL_TOO_MANY_INVITES', 'Too many open invites. Revoke some first.');
  const token = randomToken(24);
  const expiresAt = Date.now() + INVITE_TTL_MS;
  const id = store.insertInvite(userId, sha256(token), expiresAt);
  return { id, link: `${pool.selfUrl}/app/?pool=${token}`, expiresAt: new Date(expiresAt).toISOString() };
}

function revokeInvite(userId, inviteId) {
  requireHost(userId);
  if (!store.revokeInvite(userId, inviteId)) throw fail(404, 'POOL_INVITE_NOT_FOUND', 'This invite does not exist.');
}

function parseInvite(link) {
  let url;
  try { url = new URL(String(link || '').trim()); } catch { url = null; }
  const token = url?.searchParams.get('pool');
  const origin = url ? client.originOf(url.origin) : null;
  if (!origin || !token || !TOKEN_PATTERN.test(token)) throw fail(400, 'POOL_INVITE_INVALID', 'This is not a pool invite link.');
  return { origin, token };
}

function validInvite(token) {
  const invite = typeof token === 'string' && TOKEN_PATTERN.test(token) ? store.inviteByHash(sha256(token)) : null;
  const pool = invite ? store.poolOf(invite.user_id) : null;
  if (!invite || invite.used_at || invite.revoked_at || invite.expires_at <= Date.now() || !pool?.isHost) {
    throw fail(404, 'POOL_INVITE_INVALID', 'This invite link is no longer valid. Ask for a new one.');
  }
  return { invite, pool };
}

// ---------------------------------------------------------------- joining

async function preview(userId, link) {
  const { origin, token } = parseInvite(link);
  const info = await client.post(userId, origin, 'preview', { token });
  return {
    url: origin,
    pool: { name: cleanName(info?.pool?.name, 'Pool') },
    host: { name: cleanName(info?.host?.name, 'Host') },
    members: (info?.members || []).slice(0, MAX_MEMBERS).map((member) => ({ name: cleanName(member?.name, 'Partner') })),
  };
}

function handlePreview(body) {
  const { pool } = validInvite(body?.token);
  return {
    v: protocol.VERSION,
    pool: { id: pool.poolId, name: pool.name },
    host: { memberId: pool.hostMemberId, name: pool.selfName, publicKey: pool.hostPublicKey },
    members: store.members(pool.userId).map((member) => ({ name: member.name })),
  };
}

// Links us with a partner whose key we were given: a new link, a key for
// them, and the partner as a connection. Replaces an older link to them.
async function accept(userId, member, peerKey) {
  assertPeerKey(peerKey);
  const existing = store.linkToMember(userId, member.memberId);
  if (existing) links.teardown(userId, existing);
  const link = store.insertLink(userId, member);
  const ourKey = links.issueInboundKey(userId, link);
  await links.connectOutbound(userId, store.link(userId, link.id), peerKey);
  return ourKey;
}

function ticketFor(hostUserId, pool, newMember, target) {
  const ticket = { pool: pool.poolId, from: newMember.memberId, fromKey: newMember.publicKey, to: target.memberId, exp: Date.now() + TICKET_TTL_MS };
  return { ticket, signature: protocol.signObject(hostUserId, ticket), member: publicMember(target) };
}

async function join(userId, { link, selfName, selfUrl, sharedAccountIds = [] }) {
  assertNotInPool(userId);
  const { origin, token } = parseInvite(link);
  const url = ownOrigin(selfUrl);
  const info = await client.post(userId, origin, 'preview', { token });
  if (info?.v !== protocol.VERSION) throw fail(400, 'POOL_VERSION_MISMATCH', 'The host speaks another pool protocol version. Update both NeoRouters to the latest release.');
  const host = sanitizeMember({ ...info.host, url: origin });
  if (!host) throw fail(502, 'POOL_PARTNER_REFUSED', 'The host sent an invalid answer.');
  const me = identity.ensure(userId);
  if (host.memberId === me.memberId) throw fail(400, 'POOL_OWN_INVITE', 'This is an invite to your own pool.');
  const ownName = cleanName(selfName, 'Partner');
  // Saved before the handshake: the host lists our shared models while it
  // answers, and our side must already know the pool for that.
  store.insertPool({
    userId, poolId: String(info.pool?.id || ''), name: cleanName(info.pool?.name, 'Pool'), isHost: false,
    hostMemberId: host.memberId, hostPublicKey: host.publicKey, hostUrl: origin, selfUrl: url, selfName: ownName, share: {},
  });
  setShared(userId, sharedAccountIds);
  const hostLink = store.insertLink(userId, host);
  let welcome;
  try {
    const peerKey = links.issueInboundKey(userId, hostLink);
    const message = protocol.envelope(userId, 'join', { token, member: { memberId: me.memberId, name: ownName, url, publicKey: me.publicKey }, peerKey }, host.memberId);
    welcome = protocol.open(await client.post(userId, origin, 'join', message), { type: 'welcome', publicKey: host.publicKey }).data;
    assertPeerKey(welcome?.peerKey);
    if (welcome.pool?.id !== info.pool?.id) throw fail(502, 'POOL_PARTNER_REFUSED', 'The host answered for another pool.');
  } catch (error) {
    leaveLocally(userId);
    throw error;
  }
  for (const raw of welcome.members || []) {
    const member = sanitizeMember(raw);
    if (member) store.upsertMember(userId, member);
  }
  await links.connectOutbound(userId, store.link(userId, hostLink.id), welcome.peerKey);
  for (const ticket of (welcome.tickets || []).slice(0, MAX_MEMBERS)) {
    try {
      await introduce(userId, ticket);
    } catch (error) {
      logger.warn('Introduction to a pool member failed; sync will try again', { error: error.message });
    }
  }
  return summary(userId);
}

async function handleJoin(message) {
  const data = message?.body?.data || {};
  const { invite, pool } = validInvite(data.token);
  const hostUserId = pool.userId;
  const member = sanitizeMember(data.member);
  if (!member) throw fail(400, 'POOL_MESSAGE_INVALID', 'The member details are malformed.');
  const body = protocol.open(message, { type: 'join', publicKey: member.publicKey });
  if (body.from !== member.memberId || body.to !== pool.hostMemberId) throw fail(400, 'POOL_MESSAGE_INVALID', 'The message is addressed wrongly.');
  if (store.member(hostUserId, member.memberId)) throw fail(409, 'POOL_ALREADY_MEMBER', 'You are already in this pool.');
  if (store.members(hostUserId).length >= MAX_MEMBERS) throw fail(409, 'POOL_FULL', `A pool has at most ${MAX_MEMBERS} members.`);
  assertPeerKey(data.peerKey);
  await client.assertReachable(hostUserId, member.url);
  if (!store.useInvite(invite.id, member.memberId)) throw fail(404, 'POOL_INVITE_INVALID', 'This invite link is no longer valid. Ask for a new one.');
  const peerKey = await accept(hostUserId, member, data.peerKey);
  store.upsertMember(hostUserId, member);
  const joined = store.member(hostUserId, member.memberId);
  const tickets = store.members(hostUserId)
    .filter((other) => other.memberId !== pool.hostMemberId && other.memberId !== member.memberId)
    .map((other) => ticketFor(hostUserId, pool, joined, other));
  return protocol.envelope(hostUserId, 'welcome', {
    pool: { id: pool.poolId, name: pool.name },
    peerKey,
    members: store.members(hostUserId).map(publicMember),
    tickets,
  }, member.memberId);
}

// ---------------------------------------------------------------- introductions

async function introduce(userId, { ticket, signature, member }) {
  const pool = requirePool(userId);
  const target = sanitizeMember(member);
  if (!target || target.memberId !== ticket?.to) throw fail(400, 'POOL_MESSAGE_INVALID', 'The introduction is malformed.');
  store.upsertMember(userId, target);
  const existing = store.linkToMember(userId, target.memberId);
  if (existing) links.teardown(userId, existing);
  const link = store.insertLink(userId, target);
  try {
    const peerKey = links.issueInboundKey(userId, link);
    const message = protocol.envelope(userId, 'introduce', { ticket, signature, member: selfMember(userId, pool), peerKey }, target.memberId);
    const reply = protocol.open(await client.post(userId, target.url, 'introduce', message), { type: 'introduced', publicKey: target.publicKey }).data;
    assertPeerKey(reply?.peerKey);
    await links.connectOutbound(userId, store.link(userId, link.id), reply.peerKey);
  } catch (error) {
    links.teardown(userId, store.link(userId, link.id) || link);
    throw error;
  }
}

async function handleIntroduce(message) {
  const body = message?.body || {};
  const data = body.data || {};
  const userId = identity.userForMember(body.to);
  const pool = userId ? store.poolOf(userId) : null;
  if (!pool) throw fail(404, 'POOL_MEMBER_UNKNOWN', 'This pool member is not on this NeoRouter.');
  const { ticket, signature } = data;
  if (!ticket || ticket.to !== body.to || ticket.pool !== pool.poolId || !(ticket.exp > Date.now())) {
    throw fail(401, 'POOL_SIGNATURE_INVALID', 'The introduction ticket is not valid.');
  }
  if (!protocol.verifyObject(pool.hostPublicKey, ticket, signature)) throw fail(401, 'POOL_SIGNATURE_INVALID', 'The introduction was not signed by the pool’s host.');
  const member = sanitizeMember(data.member);
  if (!member || member.memberId !== ticket.from || member.publicKey !== ticket.fromKey) throw fail(401, 'POOL_SIGNATURE_INVALID', 'The introduction does not match its ticket.');
  const opened = protocol.open(message, { type: 'introduce', publicKey: member.publicKey });
  if (opened.from !== member.memberId) throw fail(401, 'POOL_SIGNATURE_INVALID', 'The sender does not match.');
  await client.assertReachable(userId, member.url);
  const peerKey = await accept(userId, member, data.peerKey);
  store.upsertMember(userId, member);
  return protocol.envelope(userId, 'introduced', { peerKey }, member.memberId);
}

// ---------------------------------------------------------------- messages from members

// The recipient (body.to), its pool and the sending member, after checking the signature.
function fromMember(message, type) {
  const body = message?.body || {};
  const userId = identity.userForMember(body.to);
  const pool = userId ? store.poolOf(userId) : null;
  const sender = pool ? store.member(userId, String(body.from || '')) : null;
  if (!sender) throw fail(404, 'POOL_MEMBER_UNKNOWN', 'This pool member is not known here.');
  const opened = protocol.open(message, { type, publicKey: sender.publicKey });
  return { userId, pool, sender, data: opened.data || {} };
}

function handleMemberList(message) {
  const { userId, pool, sender } = fromMember(message, 'member_list_request');
  if (!pool.isHost) throw fail(403, 'POOL_HOST_ONLY', 'Only the host keeps the member list.');
  return protocol.envelope(userId, 'member_list', { pool: pool.poolId, name: pool.name, members: store.members(userId).map(publicMember) }, sender.memberId);
}

function handleTicket(message) {
  const { userId, pool, sender, data } = fromMember(message, 'ticket_request');
  if (!pool.isHost) throw fail(403, 'POOL_HOST_ONLY', 'Only the host introduces members.');
  const target = store.member(userId, String(data.target || ''));
  if (!target || target.memberId === sender.memberId) throw fail(404, 'POOL_MEMBER_UNKNOWN', 'This pool member is not known here.');
  return protocol.envelope(userId, 'ticket', ticketFor(userId, pool, sender, target), sender.memberId);
}

function dropMember(userId, memberId) {
  const link = store.linkToMember(userId, memberId);
  if (link) links.teardown(userId, link);
  store.removeMember(userId, memberId);
}

function handleLeave(message) {
  const { userId, sender } = fromMember(message, 'leave');
  dropMember(userId, sender.memberId);
  return { ok: true };
}

function handleRemove(message) {
  const { userId, pool, sender, data } = fromMember(message, 'remove');
  if (sender.memberId !== pool.hostMemberId) throw fail(403, 'POOL_HOST_ONLY', 'Only the host can remove members.');
  if (data.memberId === identity.ensure(userId).memberId) leaveLocally(userId);
  else dropMember(userId, String(data.memberId || ''));
  return { ok: true };
}

// A partner changed what it shares: refresh its models here now, instead of
// at the next regular model sync.
async function handleSharedChanged(message) {
  const { userId, sender } = fromMember(message, 'shared_changed');
  const link = store.linkToMember(userId, sender.memberId);
  if (link?.accountId) {
    try {
      await require('../catalog/catalog_service').syncAccount(link.accountId);
      // Whatever went wrong while the partner was paused or sharing less is
      // forgotten, and the limits it reported start over.
      require('../routing/fixed').markAccountFixed(link.accountId);
      const refs = getDatabase().prepare('SELECT id FROM models WHERE account_id = ?').all(link.accountId).map((row) => row.id);
      require('../quota/quota_service').forgetObserved(link.accountId, refs);
      store.updateLink(link.id, { lastError: null });
    } catch (error) {
      store.updateLink(link.id, { lastError: error.message });
    }
  }
  return { ok: true };
}

function handleDissolve(message) {
  const { userId, pool, sender } = fromMember(message, 'dissolve');
  if (sender.memberId !== pool.hostMemberId) throw fail(403, 'POOL_HOST_ONLY', 'Only the host can close the pool.');
  leaveLocally(userId);
  return { ok: true };
}

// ---------------------------------------------------------------- leaving and removing

// Everything about the pool goes, here and now: partner keys stop working at
// once, whether or not the partners hear about it.
function leaveLocally(userId) {
  for (const link of store.links(userId)) links.teardown(userId, link);
  getDatabase().prepare('UPDATE provider_accounts SET pool_share = 0 WHERE user_id = ?').run(userId);
  sharePolicy.resetForTests();
  store.deletePool(userId);
}

async function notify(userId, members, type, data = {}) {
  await Promise.allSettled(members.map((member) => client.post(userId, member.url, type, protocol.envelope(userId, type, data, member.memberId))));
}

async function leave(userId) {
  const pool = requirePool(userId);
  const me = identity.ensure(userId).memberId;
  const others = store.members(userId).filter((member) => member.memberId !== me);
  // A host leaving closes the pool for everyone.
  await notify(userId, others, pool.isHost ? 'dissolve' : 'leave');
  leaveLocally(userId);
}

async function removeMember(userId, memberId) {
  requireHost(userId);
  const me = identity.ensure(userId).memberId;
  const target = store.member(userId, memberId);
  if (!target || memberId === me) throw fail(404, 'POOL_MEMBER_UNKNOWN', 'This pool member does not exist.');
  dropMember(userId, memberId);
  const others = store.members(userId).filter((member) => member.memberId !== me);
  await notify(userId, [...others, target], 'remove', { memberId });
}

// ---------------------------------------------------------------- syncing

// Compares with the host's signed member list: links to members who left go,
// members we have no link with yet are introduced (the later joiner of two
// starts, so two members never introduce themselves to each other at once).
async function sync(userId) {
  const pool = store.poolOf(userId);
  if (!pool || pool.isHost) return;
  const me = identity.ensure(userId);
  const reply = await client.post(userId, pool.hostUrl, 'members', protocol.envelope(userId, 'member_list_request', {}, pool.hostMemberId));
  const data = protocol.open(reply, { type: 'member_list', publicKey: pool.hostPublicKey }).data || {};
  if (data.pool !== pool.poolId) return;
  const listed = (data.members || []).slice(0, MAX_MEMBERS).map(sanitizeMember).filter(Boolean);
  const self = listed.find((member) => member.memberId === me.memberId);
  if (!self) {
    leaveLocally(userId);
    return;
  }
  const listedIds = new Set(listed.map((member) => member.memberId));
  for (const member of store.members(userId)) if (!listedIds.has(member.memberId)) dropMember(userId, member.memberId);
  for (const member of listed) store.upsertMember(userId, member);
  if (data.name) store.updatePool(userId, { name: cleanName(data.name, pool.name) });
  for (const member of listed) {
    if (member.memberId === me.memberId || store.linkToMember(userId, member.memberId)) continue;
    if (member.memberId !== pool.hostMemberId && String(member.joinedAt || '') > String(self.joinedAt || '')) continue;
    try {
      const ticketReply = await client.post(userId, pool.hostUrl, 'ticket', protocol.envelope(userId, 'ticket_request', { target: member.memberId }, pool.hostMemberId));
      await introduce(userId, protocol.open(ticketReply, { type: 'ticket', publicKey: pool.hostPublicKey }).data);
    } catch (error) {
      logger.warn('Pool introduction during sync failed', { error: error.message });
    }
  }
  store.updatePool(userId, { lastSyncAt: new Date().toISOString() });
}

async function syncAll() {
  for (const userId of store.usersInPools()) {
    try {
      await sync(userId);
    } catch (error) {
      logger.warn('Pool sync failed', { error: error.message });
    }
  }
}

// ---------------------------------------------------------------- settings

// Tells every partner to refresh our models.
async function announceShared(userId) {
  const me = identity.ensure(userId).memberId;
  await notify(userId, store.members(userId).filter((member) => member.memberId !== me), 'shared_changed');
}

async function updateShare(userId, input) {
  const pool = requirePool(userId);
  const next = { ...pool.share };
  for (const field of Object.keys(sharePolicy.DEFAULT_SHARE)) if (input[field] !== undefined) next[field] = input[field];
  store.updatePool(userId, { share: sharePolicy.shareSettings({ share: next }) });
  if (Array.isArray(input.sharedAccountIds)) setShared(userId, input.sharedAccountIds);
  await announceShared(userId);
  return summary(userId);
}

async function setAccountShared(userId, accountId, shared) {
  requirePool(userId);
  if (!sharePolicy.setAccountShared(userId, accountId, shared)) throw fail(404, 'ACCOUNT_NOT_FOUND', 'This connection does not exist.');
  await announceShared(userId);
  return summary(userId);
}

// The quick switch: off pauses giving and using for every partner at once;
// on restores each partner as it was.
async function setPoolPaused(userId, paused) {
  requirePool(userId);
  store.updatePool(userId, { paused });
  for (const link of store.links(userId)) {
    links.applyEnabled(userId, link);
    if (!paused && link.accountId) require('../routing/fixed').markAccountFixed(link.accountId);
  }
  // Partners refresh what we offer: nothing while paused, everything again after.
  await announceShared(userId);
  return summary(userId);
}

async function setPartnerPaused(userId, linkId, paused) {
  requirePool(userId);
  const link = store.link(userId, linkId);
  if (!link) throw fail(404, 'POOL_PARTNER_NOT_FOUND', 'This partner does not exist.');
  links.setPaused(userId, link, paused);
  if (!paused && link.accountId) require('../routing/fixed').markAccountFixed(link.accountId);
  const member = store.member(userId, link.memberId);
  if (member) await notify(userId, [member], 'shared_changed');
  return summary(userId);
}

// ---------------------------------------------------------------- summary

function partnerView(userId, pool, member) {
  const link = store.linkToMember(userId, member.memberId);
  const account = link?.accountId
    ? getDatabase().prepare(`SELECT a.status, a.status_message, a.enabled,
        (SELECT COUNT(*) FROM models m WHERE m.account_id = a.id AND m.available = 1 AND m.capability = 'chat') models
      FROM provider_accounts a WHERE a.id = ?`).get(link.accountId)
    : null;
  const balance = link ? sharePolicy.balance(link.id) : null;
  const traffic = require('./traffic');
  return {
    memberId: member.memberId,
    name: member.name,
    isHost: member.memberId === pool.hostMemberId,
    linkId: link?.id || null,
    // connecting: no link yet; paused: by us; offline: we cannot reach them.
    state: !link ? 'connecting' : link.status === 'paused' || pool.paused ? 'paused' : !account || account.status === 'error' || reachability.isDown(link.accountId) ? 'offline' : 'online',
    problem: link?.lastError || (account?.status === 'error' ? account.status_message : null) || null,
    models: account?.models || 0,
    balance: balance ? { state: balance.state, factor: balance.factor, week: { given: balance.given, taken: balance.taken } } : null,
    today: link ? { given: traffic.today(link.id, 'given'), taken: traffic.today(link.id, 'taken') } : null,
  };
}

function summary(userId, { canManage = true } = {}) {
  const pool = store.poolOf(userId);
  const privacy = peerPrivacy.getSettings(userId);
  if (!pool) return { pool: null, partners: [], share: sharePolicy.shareSettings(null), invites: [], privacy, shareable: canManage ? shareableAccounts(userId) : [] };
  const me = identity.ensure(userId).memberId;
  return {
    pool: { id: pool.poolId, name: pool.name, isHost: pool.isHost, paused: pool.paused, selfName: pool.selfName, joinedAt: pool.joinedAt, lastSyncAt: pool.lastSyncAt },
    partners: store.members(userId).filter((member) => member.memberId !== me).map((member) => partnerView(userId, pool, member)),
    share: sharePolicy.shareSettings(pool),
    invites: pool.isHost && canManage ? store.invites(userId) : [],
    privacy,
    shareable: canManage ? shareableAccounts(userId) : [],
  };
}

module.exports = {
  MAX_MEMBERS,
  create, createInvite, revokeInvite, parseInvite, preview, join, leave, removeMember, sync, syncAll,
  updateShare, setAccountShared, setPartnerPaused, setPoolPaused, summary,
  handlePreview, handleJoin, handleIntroduce, handleMemberList, handleTicket, handleLeave, handleRemove, handleDissolve, handleSharedChanged,
};
