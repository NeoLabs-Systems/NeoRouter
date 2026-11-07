'use strict';

// One partner link in both directions:
//   inbound   a router key on this instance the partner calls us with. It
//             reaches free models only, is hidden from the keys page, and is
//             stored as a hash like every key.
//   outbound  a provider account ("Pool partner") holding the partner's key
//             for us, encrypted like every provider key. Through it the
//             router uses the partner like any other connection.
const { getDatabase } = require('../../db/database');
const routerKeys = require('../router_keys/router_key_service');
const accounts = require('../providers/account_service');
const catalog = require('../catalog/catalog_service');
const store = require('./store');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('pools');
const PEER_DEFINITION = 'neorouter_peer';

// Creates the key the partner will use us with, and returns its secret
// (sent to the partner once, never stored in clear). Lowest priority, so the
// owner's own requests go first whenever both wait for the same capacity.
function issueInboundKey(userId, link) {
  const { key, secret } = routerKeys.create(userId, { name: `Pool: ${link.name}`, allowFree: true, allowPlan: false, allowPaid: false, profile: 'balanced', priority: 'low' }, userId);
  getDatabase().prepare('UPDATE router_keys SET peer_link_id = ? WHERE id = ?').run(link.id, key.id);
  store.updateLink(link.id, { routerKeyId: key.id });
  return secret;
}

// Adds the partner as a connection. A partner that cannot be reached right
// now (behind NAT, offline) still gets the link; the error is shown and the
// regular model sync tries again.
async function connectOutbound(userId, link, peerKey) {
  try {
    const { account } = await accounts.create(userId, {
      definitionId: PEER_DEFINITION, baseUrl: `${link.url}/v1`, secret: peerKey, label: link.name, slug: `pool-${link.name}`, billingMode: 'free_tier',
    }, userId, null);
    store.updateLink(link.id, { accountId: account.id, lastError: null });
    applyEnabled(userId, { ...link, accountId: account.id });
    try {
      await catalog.syncAccount(account.id);
    } catch (error) {
      store.updateLink(link.id, { lastError: error.message });
    }
  } catch (error) {
    logger.warn('Pool partner could not be added as a connection', { linkId: link.id, error: error.message });
    store.updateLink(link.id, { lastError: error.message });
  }
}

// Removes both directions at once: the partner's key stops working here
// immediately, whatever the partner does.
function teardown(userId, link) {
  if (link.routerKeyId) getDatabase().prepare('DELETE FROM router_keys WHERE id = ? AND user_id = ?').run(link.routerKeyId, userId);
  if (link.accountId) {
    try { accounts.remove(userId, link.accountId); } catch { /* already gone */ }
  }
  store.deleteLink(link.id);
  require('./share_policy').forget(link.id);
}

// The partner's connection is used only while neither this link nor the
// whole pool is paused.
function applyEnabled(userId, link) {
  if (!link.accountId) return;
  const usable = link.status === 'active' && !store.poolOf(userId)?.paused;
  getDatabase().prepare('UPDATE provider_accounts SET enabled = ? WHERE id = ? AND user_id = ?').run(usable ? 1 : 0, link.accountId, userId);
}

// Paused: we neither give to nor take from this partner.
function setPaused(userId, link, paused) {
  store.updateLink(link.id, { status: paused ? 'paused' : 'active' });
  applyEnabled(userId, { ...link, status: paused ? 'paused' : 'active' });
}

function isPeerEntry(entry) {
  return entry?.definitionId === PEER_DEFINITION;
}

module.exports = { PEER_DEFINITION, issueInboundKey, connectOutbound, teardown, setPaused, applyEnabled, isPeerEntry };
