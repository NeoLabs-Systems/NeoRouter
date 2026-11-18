'use strict';

// A member without a public address is reached through its pool's host. Its
// address names both: relay://<pool id>.<member id>. Older NeoRouters only
// accept http(s) addresses, so they simply leave such members out.
const UUID_SOURCE = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
const UUID = new RegExp(`^${UUID_SOURCE}$`, 'i');
const PATTERN = new RegExp(`^relay://(${UUID_SOURCE})\\.(${UUID_SOURCE})(/.*)?$`, 'i');

function format(poolId, memberId) {
  return `relay://${String(poolId).toLowerCase()}.${String(memberId).toLowerCase()}`;
}

/** { poolId, memberId, path } for a relay address (with an optional path), else null. */
function parse(value) {
  const match = PATTERN.exec(String(value || '').trim());
  return match ? { poolId: match[1].toLowerCase(), memberId: match[2].toLowerCase(), path: match[3] || '/' } : null;
}

const isRelay = (value) => parse(value) !== null;

module.exports = { UUID, format, parse, isRelay };
