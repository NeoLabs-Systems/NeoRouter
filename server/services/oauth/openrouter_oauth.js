'use strict';

const crypto = require('node:crypto');
const { HttpError } = require('../../middleware/error_handler');
const accounts = require('../providers/account_service');
const catalog = require('../catalog/catalog_service');
const { createLogger } = require('../../utils/logger');

// OpenRouter's PKCE flow: the user approves in the browser, OpenRouter
// redirects back with a one-time code, and the server swaps it for an API key
// that is stored like any manually entered key. No key ever passes the UI.
const AUTH_URL = 'https://openrouter.ai/auth';
const EXCHANGE_URL = 'https://openrouter.ai/api/v1/auth/keys';
const FLOW_TTL_MS = 10 * 60 * 1000;
const logger = createLogger('openrouter-oauth');
const flows = new Map();

const base64url = (buffer) => buffer.toString('base64url');

function prune(now = Date.now()) {
  for (const [state, flow] of flows) if (flow.expiresAt <= now) flows.delete(state);
}

// Only redirect back into this app, never to an arbitrary site.
function safeReturnTo(returnTo, requestHost) {
  try {
    const url = new URL(returnTo);
    const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname);
    if (!/^https?:$/.test(url.protocol)) return null;
    if (loopback || url.host === requestHost) return url;
  } catch {
    return null;
  }
  return null;
}

// userId owns the new account; createdBy is who connected it (a team member
// working in the owner's space, or the owner).
function start({ userId, createdBy = userId, origin, requestHost, returnTo }) {
  prune();
  const target = safeReturnTo(returnTo, requestHost);
  if (!target) throw new HttpError(400, 'INVALID_RETURN_URL', 'The return URL must point to this NeoRouter instance.');
  const state = base64url(crypto.randomBytes(18));
  const verifier = base64url(crypto.randomBytes(48));
  const challenge = base64url(crypto.createHash('sha256').update(verifier).digest());
  flows.set(state, { userId, createdBy, verifier, returnTo: target.toString(), expiresAt: Date.now() + FLOW_TTL_MS });
  const callback = `${origin.replace(/\/+$/, '')}/api/v1/oauth/openrouter/callback/${state}`;
  const url = `${AUTH_URL}?callback_url=${encodeURIComponent(callback)}&code_challenge=${challenge}&code_challenge_method=S256`;
  return { url };
}

function withResult(returnTo, params) {
  const url = new URL(returnTo);
  for (const [key, value] of Object.entries(params)) url.searchParams.set(key, value);
  return url.toString();
}

async function complete({ state, code }) {
  prune();
  const flow = flows.get(state);
  flows.delete(state);
  if (!flow) throw new HttpError(400, 'OAUTH_EXPIRED', 'This sign-in link expired. Start the connection again.');
  if (!code) return withResult(flow.returnTo, { oauth: 'openrouter', oauth_error: 'cancelled' });
  try {
    const response = await fetch(EXCHANGE_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code, code_verifier: flow.verifier, code_challenge_method: 'S256' }),
      signal: AbortSignal.timeout(20_000),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.key) throw new Error(data?.error?.message || `OpenRouter returned HTTP ${response.status}`);
    const { account } = await accounts.create(flow.userId, { definitionId: 'openrouter', secret: data.key, billingMode: 'paid' }, flow.createdBy);
    await catalog.syncAccount(account.id).catch((error) => logger.warn('Initial model sync failed', { error: error.message }));
    return withResult(flow.returnTo, { oauth: 'openrouter', oauth_status: 'connected' });
  } catch (error) {
    logger.warn('OpenRouter connection failed', { error: error.message });
    return withResult(flow.returnTo, { oauth: 'openrouter', oauth_error: String(error.message).slice(0, 200) });
  }
}

module.exports = { start, complete, safeReturnTo };
