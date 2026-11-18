'use strict';

// The relay as the rest of NeoRouter sees it. Members without a public
// address get a relay address (address.js); a request to such an address
// goes through the pool's host:
//   - on the host itself, straight into the hub (hub.js)
//   - on a member, over its connection to the host (connector.js)
// Requests reach it through the normal HTTP layer (providers/http.js
// registerTransport), so adapters and pool messages need no relay code.
const address = require('./address');
const connector = require('./connector');
const hub = require('./hub');
const loopback = require('./loopback');
const store = require('../store');
const { registerTransport } = require('../../../providers/http');

/** The relay is offered by this pool's host (seen from this person's side). */
function relayed(pool) {
  return Boolean(pool) && (pool.isHost ? pool.relayEnabled : pool.hostRelay);
}

/** How this person asks partners through the host in this pool, or null. */
function endpointFor(userId, poolId) {
  const pool = store.pool(userId, poolId);
  if (!relayed(pool)) return null;
  return pool.isHost ? hub.localEndpoint(userId, poolId) : connector.endpointFor(userId, poolId);
}

/** Whether a relay address can be used by this person at all (they are in its pool, and it relays). */
function canUse(userId, value) {
  const target = address.parse(value);
  return Boolean(target) && relayed(store.pool(userId, target.poolId));
}

/** true/false when known, null when we cannot tell (not connected to the host). */
function isOnline(userId, poolId, memberId) {
  const pool = store.pool(userId, poolId);
  if (!relayed(pool)) return null;
  return pool.isHost ? hub.isOnline(poolId, memberId) : connector.isOnline(userId, poolId, memberId);
}

// A request to relay://<pool>.<member>/<path> from the HTTP layer.
async function transport(url, init, { owner }) {
  const target = address.parse(url);
  const endpoint = target && owner ? endpointFor(owner, target.poolId) : null;
  if (!endpoint) {
    const error = new Error('Not connected to the pool host, so this partner cannot be reached right now.');
    error.code = 'RELAY_UNREACHABLE';
    throw error;
  }
  return endpoint.request(target.memberId, { method: init.method, path: target.path, headers: init.headers, body: init.body, signal: init.signal });
}

registerTransport('relay:', transport);

module.exports = {
  address,
  connector,
  hub,
  setApp: loopback.setApp,
  relayed,
  endpointFor,
  canUse,
  isOnline,
};
