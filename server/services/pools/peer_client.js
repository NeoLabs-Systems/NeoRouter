'use strict';

// Outgoing control calls to other pool members, and the address rules for
// partners: HTTPS on the public internet; plain HTTP and private or local
// networks only where the instance allows private provider addresses (the
// admin's own LAN). Redirects are followed by hand and checked at each hop.
// A member without a public address has a relay address instead; calls to it
// go through the pool's host (relay/), sealed for that member.
const { request } = require('../../providers/http');
const { isPrivateDestination } = require('../../providers/net_guard');
const accounts = require('../providers/account_service');
const { HttpError } = require('../../middleware/error_handler');
const relay = require('./relay');

const TIMEOUT_MS = 15_000;

// The origin of a NeoRouter ("https://router.example.com"), from any URL on
// it, or a member's relay address as it is.
function originOf(value) {
  const relayed = relay.address.parse(value);
  if (relayed) return relay.address.format(relayed.poolId, relayed.memberId);
  let url;
  try { url = new URL(String(value || '').trim()); } catch { return null; }
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) return null;
  return url.origin;
}

async function assertReachable(userId, origin) {
  if (relay.address.isRelay(origin)) {
    if (!relay.canUse(userId, origin)) throw new HttpError(400, 'POOL_RELAY_UNAVAILABLE', 'This partner has no public address, and the pool’s host does not pass traffic along.');
    return;
  }
  const restricted = accounts.publicOnly(userId);
  const isPrivate = await isPrivateDestination(origin);
  if (isPrivate && restricted) throw new HttpError(400, 'POOL_PRIVATE_ADDRESS', 'This NeoRouter is on a local or private network. Only the instance admin can join pools there.');
  if (!isPrivate && new URL(origin).protocol !== 'https:') throw new HttpError(400, 'POOL_HTTPS_REQUIRED', 'Pool partners must be reached over HTTPS.');
}

// POST {origin}/federation/v1/{path} with a JSON body; errors keep the
// partner's own code and message where it sent one.
async function post(userId, origin, path, body) {
  await assertReachable(userId, origin);
  try {
    const { data } = await request(`${origin}/federation/v1/${path}`, {
      method: 'POST',
      body,
      timeoutMs: TIMEOUT_MS,
      publicOnly: accounts.publicOnly(userId),
      owner: userId,
      headers: { 'User-Agent': 'NeoRouter-Pool' },
    });
    return data;
  } catch (error) {
    if (error instanceof HttpError) throw error;
    const status = error.status && error.status < 500 ? error.status : 502;
    const reached = Boolean(error.status);
    // A pool error the partner explained (an expired invite, say) is passed on as it is.
    const remote = error.body?.error;
    if (reached && typeof remote?.code === 'string' && remote.code.startsWith('POOL_') && typeof remote.message === 'string') {
      throw new HttpError(status, remote.code, remote.message.slice(0, 300));
    }
    throw new HttpError(status, reached ? 'POOL_PARTNER_REFUSED' : 'POOL_PARTNER_UNREACHABLE',
      reached ? `The other NeoRouter answered: ${error.message}` : `The other NeoRouter could not be reached (${error.message}).`);
  }
}

module.exports = { originOf, assertReachable, post };
