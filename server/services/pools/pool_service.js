'use strict';

// Pools: people on separate NeoRouters who share their free capacity. A person
// may be in several pools; each has its own partners, sharing, caps and pause
// switch.
//
// Control goes through the host (who created the pool): it keeps the member
// list and the invites. Requests never go through the host: every two
// members of a pool are linked directly (links.js).
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
// member list. Every message names its pool.
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
const traffic = require('./traffic');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('pools');

const INVITE_TTL_MS = 7 * 86_400_000;
const MAX_ACTIVE_INVITES = 20;
const MAX_MEMBERS = 20;
const MAX_POOLS = 10;
const TICKET_TTL_MS = 10 * 60_000;
const TOKEN_PATTERN = /^[A-Za-z0-9_-]{16,128}$/;

const fail = (status, code, message) => new HttpError(status, code, message);

function cleanName(value, fallback) {
  const name = String(value || '').replace(/[\u0000-\u001f]/g, '').replace(/\s+/g, ' ').trim().slice(0, 60);
  return name || fallback;
}

function requirePool(userId, poolId) {
  const found = store.pool(userId, poolId);
  if (!found) throw fail(404, 'POOL_NOT_FOUND', 'This pool does not exist, or you are not in it.');
  return found;
}

function requireHost(userId, poolId) {
  const found = requirePool(userId, poolId);
  if (!found.isHost) throw fail(403, 'POOL_HOST_ONLY', 'Only the pool’s host can do this.');
  return found;
}

function assertRoomForPool(userId) {
  if (store.pools(userId).length >= MAX_POOLS) throw fail(409, 'POOL_LIMIT', `You can be in at most ${MAX_POOLS} pools.`);
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

// In this pool, exactly these of the person's own connections are shared.
function setShared(userId, poolId, accountIds = []) {
  const wanted = new Set(accountIds.map(String));
  const rows = getDatabase().prepare("SELECT id FROM provider_accounts WHERE user_id = ? AND definition_id != 'neorouter_peer'").all(userId);
  for (const row of rows) sharePolicy.setAccountShared(userId, poolId, row.id, wanted.has(row.id));
}

// Own connections with chat models that could be shared: free ones, or paid
// ones (only ever shared after an explicit opt-in). prepaid: the provider
// reports a balance, so partner use spends it; otherwise it is billed.
function shareableAccounts(userId) {
  return getDatabase().prepare(`SELECT a.id, a.label, a.definition_id, a.enabled,
      (SELECT COUNT(*) FROM models m WHERE m.account_id = a.id AND m.is_free = 1 AND m.available = 1 AND m.capability = 'chat') free_models,
      -- Plan (subscription) connections never count: their models are not shared.
      CASE WHEN a.prepaid THEN 0 ELSE (SELECT COUNT(*) FROM models m WHERE m.account_id = a.id AND m.is_free = 0 AND m.available = 1 AND m.capability = 'chat') END paid_models,
      EXISTS (SELECT 1 FROM quota_state q WHERE q.scope_key = 'a:' || a.id AND q.metric = 'balance') prepaid
    FROM provider_accounts a WHERE a.user_id = ? AND a.definition_id != 'neorouter_peer' ORDER BY a.label COLLATE NOCASE`).all(userId)
    .filter((row) => row.free_models > 0 || row.paid_models > 0)
    .map((row) => ({ id: row.id, label: row.label, definitionId: row.definition_id, enabled: row.enabled === 1, freeModels: row.free_models, paidModels: row.paid_models, prepaid: row.prepaid === 1 }));
}

// ---------------------------------------------------------------- creating

function create(userId, { name, selfName, selfUrl, sharedAccountIds = [] }) {
  assertRoomForPool(userId);
  const url = ownOrigin(selfUrl);
  const me = identity.ensure(userId);
  const ownName = cleanName(selfName, 'Me');
  const poolId = crypto.randomUUID();
  store.insertPool({
    userId, poolId, name: cleanName(name, 'My pool'), isHost: true,
    hostMemberId: me.memberId, hostPublicKey: me.publicKey, hostUrl: url, selfUrl: url, selfName: ownName, share: {},
  });
  store.upsertMember(userId, poolId, { memberId: me.memberId, name: ownName, url, publicKey: me.publicKey });
  setShared(userId, poolId, sharedAccountIds);
  return { poolId, ...summary(userId) };
}

function createInvite(userId, poolId) {
  const pool = requireHost(userId, poolId);
  if (store.members(userId, poolId).length >= MAX_MEMBERS) throw fail(409, 'POOL_FULL', `A pool has at most ${MAX_MEMBERS} members.`);
  if (store.activeInviteCount(userId, poolId) >= MAX_ACTIVE_INVITES) throw fail(429, 'POOL_TOO_MANY_INVITES', 'Too many open invites. Revoke some first.');
  const token = randomToken(24);
  const expiresAt = Date.now() + INVITE_TTL_MS;
  const id = store.insertInvite(userId, poolId, sha256(token), expiresAt);
  // Opens a page on this server that sends the invited person to their own NeoRouter.
  return { id, link: `${pool.selfUrl}/pool/join?pool=${token}`, expiresAt: new Date(expiresAt).toISOString() };
}

function revokeInvite(userId, poolId, inviteId) {
  requireHost(userId, poolId);
  if (!store.revokeInvite(userId, poolId, inviteId)) throw fail(404, 'POOL_INVITE_NOT_FOUND', 'This invite does not exist.');
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
  const pool = invite ? store.pool(invite.user_id, invite.pool_id) : null;
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
    alreadyMember: info?.pool?.id ? Boolean(store.pool(userId, String(info.pool.id))) : false,
  };
}

function handlePreview(body) {
  const { pool } = validInvite(body?.token);
  return {
    v: protocol.VERSION,
    pool: { id: pool.poolId, name: pool.name },
    host: { memberId: pool.hostMemberId, name: pool.selfName, publicKey: pool.hostPublicKey },
    members: store.members(pool.userId, pool.poolId).map((member) => ({ name: member.name })),
  };
}

// Links us with a partner whose key we were given: a new link, a key for
// them, and the partner as a connection. Replaces an older link to them.
async function accept(userId, poolId, member, peerKey) {
  assertPeerKey(peerKey);
  const existing = store.linkToMember(userId, poolId, member.memberId);
  if (existing) links.teardown(userId, existing);
  const link = store.insertLink(userId, poolId, member);
  const ourKey = links.issueInboundKey(userId, link);
  await links.connectOutbound(userId, store.link(userId, link.id), peerKey);
  return ourKey;
}

function ticketFor(hostUserId, pool, newMember, target) {
  const ticket = { pool: pool.poolId, from: newMember.memberId, fromKey: newMember.publicKey, to: target.memberId, exp: Date.now() + TICKET_TTL_MS };
  return { ticket, signature: protocol.signObject(hostUserId, ticket), member: publicMember(target) };
}

async function join(userId, { link, selfName, selfUrl, sharedAccountIds = [] }) {
  assertRoomForPool(userId);
  const { origin, token } = parseInvite(link);
  const url = ownOrigin(selfUrl);
  const info = await client.post(userId, origin, 'preview', { token });
  if (info?.v !== protocol.VERSION) throw fail(400, 'POOL_VERSION_MISMATCH', 'The host speaks another pool protocol version. Update both NeoRouters to the latest release.');
  const host = sanitizeMember({ ...info.host, url: origin });
  const poolId = String(info?.pool?.id || '');
  if (!host || !/^[0-9a-f-]{36}$/i.test(poolId)) throw fail(502, 'POOL_PARTNER_REFUSED', 'The host sent an invalid answer.');
  const me = identity.ensure(userId);
  if (host.memberId === me.memberId) throw fail(400, 'POOL_OWN_INVITE', 'This is an invite to your own pool.');
  if (store.pool(userId, poolId)) throw fail(409, 'POOL_ALREADY_JOINED', 'You are already in this pool.');
  const ownName = cleanName(selfName, 'Partner');
  // Saved before the handshake: the host lists our shared models while it
  // answers, and our side must already know the pool for that.
  store.insertPool({
    userId, poolId, name: cleanName(info.pool?.name, 'Pool'), isHost: false,
    hostMemberId: host.memberId, hostPublicKey: host.publicKey, hostUrl: origin, selfUrl: url, selfName: ownName, share: {},
  });
  setShared(userId, poolId, sharedAccountIds);
  const hostLink = store.insertLink(userId, poolId, host);
  let welcome;
  try {
    const peerKey = links.issueInboundKey(userId, hostLink);
    const message = protocol.envelope(userId, 'join', { pool: poolId, token, member: { memberId: me.memberId, name: ownName, url, publicKey: me.publicKey }, peerKey }, host.memberId);
    welcome = protocol.open(await client.post(userId, origin, 'join', message), { type: 'welcome', publicKey: host.publicKey }).data;
    assertPeerKey(welcome?.peerKey);
    if (welcome.pool?.id !== poolId) throw fail(502, 'POOL_PARTNER_REFUSED', 'The host answered for another pool.');
  } catch (error) {
    leaveLocally(userId, poolId);
    throw error;
  }
  for (const raw of welcome.members || []) {
    const member = sanitizeMember(raw);
    if (member) store.upsertMember(userId, poolId, member);
  }
  await links.connectOutbound(userId, store.link(userId, hostLink.id), welcome.peerKey);
  for (const ticket of (welcome.tickets || []).slice(0, MAX_MEMBERS)) {
    try {
      await introduce(userId, poolId, ticket);
    } catch (error) {
      logger.warn('Introduction to a pool member failed; sync will try again', { error: error.message });
    }
  }
  return { poolId, ...summary(userId) };
}

async function handleJoin(message) {
  const data = message?.body?.data || {};
  const { invite, pool } = validInvite(data.token);
  const hostUserId = pool.userId;
  const member = sanitizeMember(data.member);
  if (!member) throw fail(400, 'POOL_MESSAGE_INVALID', 'The member details are malformed.');
  const body = protocol.open(message, { type: 'join', publicKey: member.publicKey });
  if (body.from !== member.memberId || body.to !== pool.hostMemberId || data.pool !== pool.poolId) throw fail(400, 'POOL_MESSAGE_INVALID', 'The message is addressed wrongly.');
  if (store.member(hostUserId, pool.poolId, member.memberId)) throw fail(409, 'POOL_ALREADY_MEMBER', 'You are already in this pool.');
  if (store.members(hostUserId, pool.poolId).length >= MAX_MEMBERS) throw fail(409, 'POOL_FULL', `A pool has at most ${MAX_MEMBERS} members.`);
  assertPeerKey(data.peerKey);
  await client.assertReachable(hostUserId, member.url);
  if (!store.useInvite(invite.id, member.memberId)) throw fail(404, 'POOL_INVITE_INVALID', 'This invite link is no longer valid. Ask for a new one.');
  const peerKey = await accept(hostUserId, pool.poolId, member, data.peerKey);
  store.upsertMember(hostUserId, pool.poolId, member);
  const joined = store.member(hostUserId, pool.poolId, member.memberId);
  const tickets = store.members(hostUserId, pool.poolId)
    .filter((other) => other.memberId !== pool.hostMemberId && other.memberId !== member.memberId)
    .map((other) => ticketFor(hostUserId, pool, joined, other));
  return protocol.envelope(hostUserId, 'welcome', {
    pool: { id: pool.poolId, name: pool.name },
    peerKey,
    members: store.members(hostUserId, pool.poolId).map(publicMember),
    tickets,
  }, member.memberId);
}

// ---------------------------------------------------------------- introductions

async function introduce(userId, poolId, { ticket, signature, member }) {
  const pool = requirePool(userId, poolId);
  const target = sanitizeMember(member);
  if (!target || target.memberId !== ticket?.to || ticket?.pool !== poolId) throw fail(400, 'POOL_MESSAGE_INVALID', 'The introduction is malformed.');
  store.upsertMember(userId, poolId, target);
  const existing = store.linkToMember(userId, poolId, target.memberId);
  if (existing) links.teardown(userId, existing);
  const link = store.insertLink(userId, poolId, target);
  try {
    const peerKey = links.issueInboundKey(userId, link);
    const message = protocol.envelope(userId, 'introduce', { pool: poolId, ticket, signature, member: selfMember(userId, pool), peerKey }, target.memberId);
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
  const { ticket, signature } = data;
  const userId = identity.userForMember(body.to);
  const pool = userId && ticket ? store.pool(userId, String(ticket.pool || '')) : null;
  if (!pool) throw fail(404, 'POOL_MEMBER_UNKNOWN', 'This pool member is not on this NeoRouter.');
  if (ticket.to !== body.to || data.pool !== pool.poolId || !(ticket.exp > Date.now())) {
    throw fail(401, 'POOL_SIGNATURE_INVALID', 'The introduction ticket is not valid.');
  }
  if (!protocol.verifyObject(pool.hostPublicKey, ticket, signature)) throw fail(401, 'POOL_SIGNATURE_INVALID', 'The introduction was not signed by the pool’s host.');
  const member = sanitizeMember(data.member);
  if (!member || member.memberId !== ticket.from || member.publicKey !== ticket.fromKey) throw fail(401, 'POOL_SIGNATURE_INVALID', 'The introduction does not match its ticket.');
  const opened = protocol.open(message, { type: 'introduce', publicKey: member.publicKey });
  if (opened.from !== member.memberId) throw fail(401, 'POOL_SIGNATURE_INVALID', 'The sender does not match.');
  await client.assertReachable(userId, member.url);
  const peerKey = await accept(userId, pool.poolId, member, data.peerKey);
  store.upsertMember(userId, pool.poolId, member);
  return protocol.envelope(userId, 'introduced', { pool: pool.poolId, peerKey }, member.memberId);
}

// ---------------------------------------------------------------- messages from members

// The recipient (body.to), the pool the message names and the sending
// member, after checking the signature.
function fromMember(message, type) {
  const body = message?.body || {};
  const userId = identity.userForMember(body.to);
  const pool = userId ? store.pool(userId, String(body.data?.pool || '')) : null;
  const sender = pool ? store.member(userId, pool.poolId, String(body.from || '')) : null;
  if (!sender) throw fail(404, 'POOL_MEMBER_UNKNOWN', 'This pool member is not known here.');
  const opened = protocol.open(message, { type, publicKey: sender.publicKey });
  return { userId, pool, sender, data: opened.data || {} };
}

function handleMemberList(message) {
  const { userId, pool, sender } = fromMember(message, 'member_list_request');
  if (!pool.isHost) throw fail(403, 'POOL_HOST_ONLY', 'Only the host keeps the member list.');
  return protocol.envelope(userId, 'member_list', { pool: pool.poolId, name: pool.name, members: store.members(userId, pool.poolId).map(publicMember) }, sender.memberId);
}

function handleTicket(message) {
  const { userId, pool, sender, data } = fromMember(message, 'ticket_request');
  if (!pool.isHost) throw fail(403, 'POOL_HOST_ONLY', 'Only the host introduces members.');
  const target = store.member(userId, pool.poolId, String(data.target || ''));
  if (!target || target.memberId === sender.memberId) throw fail(404, 'POOL_MEMBER_UNKNOWN', 'This pool member is not known here.');
  return protocol.envelope(userId, 'ticket', ticketFor(userId, pool, sender, target), sender.memberId);
}

function dropMember(userId, poolId, memberId) {
  const link = store.linkToMember(userId, poolId, memberId);
  if (link) links.teardown(userId, link);
  store.removeMember(userId, poolId, memberId);
}

function handleLeave(message) {
  const { userId, pool, sender } = fromMember(message, 'leave');
  dropMember(userId, pool.poolId, sender.memberId);
  return { ok: true };
}

function handleRemove(message) {
  const { userId, pool, sender, data } = fromMember(message, 'remove');
  if (sender.memberId !== pool.hostMemberId) throw fail(403, 'POOL_HOST_ONLY', 'Only the host can remove members.');
  if (data.memberId === identity.ensure(userId).memberId) leaveLocally(userId, pool.poolId);
  else dropMember(userId, pool.poolId, String(data.memberId || ''));
  return { ok: true };
}

function handleDissolve(message) {
  const { userId, pool, sender } = fromMember(message, 'dissolve');
  if (sender.memberId !== pool.hostMemberId) throw fail(403, 'POOL_HOST_ONLY', 'Only the host can close the pool.');
  leaveLocally(userId, pool.poolId);
  return { ok: true };
}

// A partner changed what it shares: refresh its models here now, instead of
// at the next regular model sync.
async function handleSharedChanged(message) {
  const { userId, pool, sender } = fromMember(message, 'shared_changed');
  const link = store.linkToMember(userId, pool.poolId, sender.memberId);
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

// ---------------------------------------------------------------- leaving and removing

// Everything about this pool goes, here and now: partner keys stop working
// at once, whether or not the partners hear about it.
function leaveLocally(userId, poolId) {
  for (const link of store.links(userId, poolId)) links.teardown(userId, link);
  store.deletePool(userId, poolId);
}

async function notify(userId, poolId, members, type, data = {}) {
  await Promise.allSettled(members.map((member) => client.post(userId, member.url, type, protocol.envelope(userId, type, { ...data, pool: poolId }, member.memberId))));
}

function othersIn(userId, poolId) {
  const me = identity.ensure(userId).memberId;
  return store.members(userId, poolId).filter((member) => member.memberId !== me);
}

async function leave(userId, poolId) {
  const pool = requirePool(userId, poolId);
  // A host leaving closes the pool for everyone.
  await notify(userId, poolId, othersIn(userId, poolId), pool.isHost ? 'dissolve' : 'leave');
  leaveLocally(userId, poolId);
}

async function removeMember(userId, poolId, memberId) {
  requireHost(userId, poolId);
  const target = store.member(userId, poolId, memberId);
  if (!target || memberId === identity.ensure(userId).memberId) throw fail(404, 'POOL_MEMBER_UNKNOWN', 'This pool member does not exist.');
  dropMember(userId, poolId, memberId);
  await notify(userId, poolId, [...othersIn(userId, poolId), target], 'remove', { memberId });
}

// ---------------------------------------------------------------- syncing

// Compares with the host's signed member list: links to members who left go,
// members we have no link with yet are introduced (the later joiner of two
// starts, so two members never introduce themselves to each other at once).
async function sync(userId, poolId) {
  const pool = store.pool(userId, poolId);
  if (!pool || pool.isHost) return;
  const me = identity.ensure(userId);
  const reply = await client.post(userId, pool.hostUrl, 'members', protocol.envelope(userId, 'member_list_request', { pool: poolId }, pool.hostMemberId));
  const data = protocol.open(reply, { type: 'member_list', publicKey: pool.hostPublicKey }).data || {};
  if (data.pool !== poolId) return;
  const listed = (data.members || []).slice(0, MAX_MEMBERS).map(sanitizeMember).filter(Boolean);
  const self = listed.find((member) => member.memberId === me.memberId);
  if (!self) {
    leaveLocally(userId, poolId);
    return;
  }
  const listedIds = new Set(listed.map((member) => member.memberId));
  for (const member of store.members(userId, poolId)) if (!listedIds.has(member.memberId)) dropMember(userId, poolId, member.memberId);
  for (const member of listed) store.upsertMember(userId, poolId, member);
  if (data.name) store.updatePool(userId, poolId, { name: cleanName(data.name, pool.name) });
  for (const member of listed) {
    if (member.memberId === me.memberId || store.linkToMember(userId, poolId, member.memberId)) continue;
    if (member.memberId !== pool.hostMemberId && String(member.joinedAt || '') > String(self.joinedAt || '')) continue;
    try {
      const ticketReply = await client.post(userId, pool.hostUrl, 'ticket', protocol.envelope(userId, 'ticket_request', { pool: poolId, target: member.memberId }, pool.hostMemberId));
      await introduce(userId, poolId, protocol.open(ticketReply, { type: 'ticket', publicKey: pool.hostPublicKey }).data);
    } catch (error) {
      logger.warn('Pool introduction during sync failed', { error: error.message });
    }
  }
  store.updatePool(userId, poolId, { lastSyncAt: new Date().toISOString() });
}

async function syncAll() {
  for (const { userId, poolId } of store.poolMemberships()) {
    try {
      await sync(userId, poolId);
    } catch (error) {
      logger.warn('Pool sync failed', { error: error.message });
    }
  }
}

// ---------------------------------------------------------------- settings

// Tells every partner in this pool to refresh our models.
async function announceShared(userId, poolId) {
  await notify(userId, poolId, othersIn(userId, poolId), 'shared_changed');
}

async function updateShare(userId, poolId, input) {
  const pool = requirePool(userId, poolId);
  const next = { ...pool.share };
  for (const field of Object.keys(sharePolicy.DEFAULT_SHARE)) if (input[field] !== undefined) next[field] = input[field];
  store.updatePool(userId, poolId, { share: sharePolicy.shareSettings({ share: next }) });
  if (Array.isArray(input.sharedAccountIds)) setShared(userId, poolId, input.sharedAccountIds);
  await announceShared(userId, poolId);
  return summary(userId);
}

// paid (optional): whether this connection's paid models are shared too.
async function setAccountShared(userId, poolId, accountId, shared, paid) {
  requirePool(userId, poolId);
  if (!sharePolicy.setAccountShared(userId, poolId, accountId, shared, paid)) throw fail(404, 'ACCOUNT_NOT_FOUND', 'This connection does not exist.');
  await announceShared(userId, poolId);
  return summary(userId);
}

// The quick switch: off pauses giving and using in this pool at once; on
// restores each partner as it was.
async function setPoolPaused(userId, poolId, paused) {
  requirePool(userId, poolId);
  store.updatePool(userId, poolId, { paused });
  for (const link of store.links(userId, poolId)) {
    links.applyEnabled(userId, link);
    if (!paused && link.accountId) require('../routing/fixed').markAccountFixed(link.accountId);
  }
  // Partners refresh what we offer: nothing while paused, everything again after.
  await announceShared(userId, poolId);
  return summary(userId);
}

async function setPartnerPaused(userId, linkId, paused) {
  const link = store.link(userId, linkId);
  if (!link) throw fail(404, 'POOL_PARTNER_NOT_FOUND', 'This partner does not exist.');
  links.setPaused(userId, link, paused);
  if (!paused && link.accountId) require('../routing/fixed').markAccountFixed(link.accountId);
  const member = store.member(userId, link.poolId, link.memberId);
  if (member) await notify(userId, link.poolId, [member], 'shared_changed');
  return summary(userId);
}

// ---------------------------------------------------------------- summary

function partnerView(userId, pool, member) {
  const link = store.linkToMember(userId, pool.poolId, member.memberId);
  const account = link?.accountId
    ? getDatabase().prepare(`SELECT a.status, a.status_message, a.enabled,
        (SELECT COUNT(*) FROM models m WHERE m.account_id = a.id AND m.available = 1 AND m.capability = 'chat') models
      FROM provider_accounts a WHERE a.id = ?`).get(link.accountId)
    : null;
  const balance = link ? sharePolicy.balance(link.id) : null;
  const offline = !account || account.status === 'error' || reachability.isDown(link?.accountId);
  return {
    memberId: member.memberId,
    name: member.name,
    isHost: member.memberId === pool.hostMemberId,
    linkId: link?.id || null,
    // connecting: no link yet; paused: by us; offline: we cannot reach them.
    state: !link ? 'connecting' : link.status === 'paused' || pool.paused ? 'paused' : offline ? 'offline' : 'online',
    problem: link?.lastError || (account?.status === 'error' ? account.status_message : null) || null,
    models: account?.models || 0,
    balance: balance ? { state: balance.state, factor: balance.factor, week: { given: balance.given, taken: balance.taken } } : null,
    today: link ? { given: traffic.today(link.id, 'given'), taken: traffic.today(link.id, 'taken') } : null,
  };
}

function poolView(userId, pool, { canManage }) {
  const me = identity.ensure(userId).memberId;
  return {
    id: pool.poolId,
    name: pool.name,
    isHost: pool.isHost,
    paused: pool.paused,
    selfName: pool.selfName,
    joinedAt: pool.joinedAt,
    lastSyncAt: pool.lastSyncAt,
    partners: store.members(userId, pool.poolId).filter((member) => member.memberId !== me).map((member) => partnerView(userId, pool, member)),
    share: sharePolicy.shareSettings(pool),
    sharedAccountIds: store.sharedAccountIds(userId, pool.poolId),
    paidAccountIds: store.paidAccountIds(userId, pool.poolId),
    invites: pool.isHost && canManage ? store.invites(userId, pool.poolId) : [],
  };
}

function summary(userId, { canManage = true } = {}) {
  return {
    pools: store.pools(userId).map((pool) => poolView(userId, pool, { canManage })),
    privacy: peerPrivacy.getSettings(userId),
    shareable: canManage ? shareableAccounts(userId) : [],
    defaults: sharePolicy.shareSettings(null),
    maxPools: MAX_POOLS,
  };
}

module.exports = {
  MAX_MEMBERS, MAX_POOLS,
  create, createInvite, revokeInvite, parseInvite, preview, join, leave, removeMember, sync, syncAll,
  updateShare, setAccountShared, setPartnerPaused, setPoolPaused, summary,
  handlePreview, handleJoin, handleIntroduce, handleMemberList, handleTicket, handleLeave, handleRemove, handleDissolve, handleSharedChanged,
};
