'use strict';

// Sign-in for the Nous Portal connection: Portal's device flow
// (POST /api/oauth/device/code), which is the same flow Hermes Agent uses, so
// it works on a server without a browser. The access token it hands out is
// itself the inference JWT for https://inference-api.nousresearch.com/v1, so
// it is stored as the account secret and sent as the bearer.
// Refresh tokens are single-use and rotate on every refresh: reuse is read as
// theft and revokes the session, so a rotated token is written back to the
// account before the call that needed it returns.
const { getDatabase } = require('../../db/database');
const { encryptString } = require('../../utils/crypto');
const { ProviderError, KINDS } = require('../../providers/provider_error');

// Overridable for tests only.
const portalBase = () => (process.env.NEOROUTER_NOUS_PORTAL_URL || 'https://portal.nousresearch.com').replace(/\/+$/, '');
// Portal has no client registration for other products yet; hermes-cli is the
// client id its own device flow uses and the only one it accepts.
const CLIENT_ID = 'hermes-cli';
const SCOPE = 'inference:invoke';
const REFRESH_MARGIN_MS = 5 * 60_000;
const DEVICE_CODE_TTL_MS = 15 * 60_000;

const pendingLogins = new Map();
const refreshing = new Map();

async function post(url, { form, headers = {} } = {}) {
  const response = await fetch(url, {
    method: 'POST',
    headers: {
      ...(form ? { 'Content-Type': 'application/x-www-form-urlencoded' } : { 'Content-Type': 'application/json' }),
      Accept: 'application/json',
      ...headers,
    },
    body: form ? new URLSearchParams(form).toString() : undefined,
    signal: AbortSignal.timeout(20_000),
  });
  const text = await response.text();
  let data = null;
  try { data = JSON.parse(text); } catch { data = null; }
  return { status: response.status, ok: response.ok, data, text };
}

function decodeJwt(token) {
  try {
    return JSON.parse(Buffer.from(String(token).split('.')[1], 'base64url').toString('utf8'));
  } catch {
    return null;
  }
}

// The account behind a Portal token, from its JWT claims.
function accountOf(token) {
  const claims = decodeJwt(token);
  if (!claims) return { userId: null, email: null, tier: null };
  return {
    userId: claims.sub || null,
    email: typeof claims.email === 'string' ? claims.email : null,
    tier: claims.account_tier || null,
  };
}

function credentialsFrom(tokens, previous = {}) {
  const { userId, email, tier } = accountOf(tokens.access_token);
  return {
    access: tokens.access_token,
    // Portal rotates refresh tokens; none in the answer means keep the old one.
    refresh: tokens.refresh_token || previous.refresh || null,
    expires: Date.now() + (Number(tokens.expires_in) || 3600) * 1000,
    userId: userId || previous.userId || null,
    email: email || previous.email || null,
    tier: tier || previous.tier || null,
  };
}

/** Starts a device-code sign-in: the code the user approves in Nous Portal. */
async function startDeviceLogin() {
  const started = await post(`${portalBase()}/api/oauth/device/code`, {
    form: { client_id: CLIENT_ID, scope: SCOPE },
  });
  if (!started.ok || !started.data?.device_code) {
    const message = started.data?.error_description || started.data?.error;
    throw new ProviderError(
      started.status === 400 && started.data?.error === 'invalid_client' ? KINDS.SERVER : KINDS.NETWORK,
      message ? `Nous Portal did not start a sign-in: ${message}.` : 'Nous Portal did not start a device sign-in. Try again in a moment.',
    );
  }
  const loginId = String(started.data.device_code);
  pendingLogins.set(loginId, {
    deviceCode: loginId,
    userCode: String(started.data.user_code || ''),
    expiresAt: Date.now() + (Number(started.data.expires_in) || 600) * 1000,
  });
  return {
    loginId,
    userCode: String(started.data.user_code || ''),
    verificationUrl: String(started.data.verification_uri_complete || started.data.verification_uri || `${portalBase()}/manage-subscription`),
    interval: Number(started.data.interval) || 5,
    expiresInSeconds: Math.min(DEVICE_CODE_TTL_MS, (Number(started.data.expires_in) || 600) * 1000) / 1000,
  };
}

/**
 * One poll: { status: 'pending' | 'slow_down' } until approved, then
 * { status: 'done', secret, email }.
 */
async function pollDeviceLogin(loginId) {
  const pending = pendingLogins.get(String(loginId || ''));
  if (!pending || pending.expiresAt < Date.now()) {
    pendingLogins.delete(String(loginId || ''));
    return { status: 'expired' };
  }
  const poll = await post(`${portalBase()}/api/oauth/token`, {
    form: {
      grant_type: 'urn:ietf:params:oauth:grant-type:device_code',
      client_id: CLIENT_ID,
      device_code: pending.deviceCode,
    },
  });
  if (poll.ok && poll.data?.access_token) {
    pendingLogins.delete(String(loginId));
    const credentials = credentialsFrom(poll.data);
    return { status: 'done', secret: JSON.stringify(credentials), email: credentials.email, tier: credentials.tier };
  }
  const error = String(poll.data?.error || '');
  if (error === 'authorization_pending' || error === 'network') return { status: 'pending' };
  if (error === 'slow_down') return { status: 'slow_down' };
  pendingLogins.delete(String(loginId));
  if (error === 'access_denied') throw new ProviderError(KINDS.AUTH, 'The sign-in was declined in Nous Portal.');
  if (error === 'expired_token') return { status: 'expired' };
  throw new ProviderError(KINDS.AUTH, poll.data?.error_description || 'Nous Portal did not hand out a key.');
}

// A signed-in connection stores { access, refresh, expires, … }. A key pasted
// from the Portal's API-keys page is a plain string and a bearer as it is, with
// nothing to refresh.
function parseSecret(secret) {
  if (typeof secret === 'string' && secret.trim() && !secret.trim().startsWith('{')) {
    return { access: secret.trim(), refresh: null, expires: null };
  }
  try {
    const value = JSON.parse(secret || '');
    if (value?.access) return value;
  } catch {
    // fall through
  }
  throw new ProviderError(KINDS.AUTH, 'This Nous Portal connection has no valid sign-in. Connect it again.');
}

async function refresh(account, current) {
  // The refresh token goes in the x-nous-refresh-token header, not the body.
  const result = await post(`${portalBase()}/api/oauth/token`, {
    form: { grant_type: 'refresh_token', client_id: CLIENT_ID },
    headers: { 'x-nous-refresh-token': current.refresh },
  });
  if (!result.ok || !result.data?.access_token) {
    const error = String(result.data?.error || '');
    const terminal = ['invalid_grant', 'refresh_token_reused', 'revoked', 'revoked_token', 'expired_token'].includes(error);
    throw new ProviderError(
      terminal ? KINDS.AUTH : KINDS.SERVER,
      terminal ? 'The Nous Portal sign-in expired or was revoked. Connect it again.' : 'Nous Portal did not refresh the sign-in. Try again in a moment.',
    );
  }
  const next = credentialsFrom(result.data, current);
  const secret = JSON.stringify(next);
  // Keep the in-memory account and the stored secret in step.
  account.secret = secret;
  if (account.id) getDatabase().prepare('UPDATE provider_accounts SET secret_encrypted = ? WHERE id = ?').run(encryptString(secret), account.id);
  return next;
}

/** Valid credentials for an account, refreshed when about to expire (one refresh at a time). */
async function freshCredentials(account) {
  const current = parseSecret(account.secret);
  if (!current.refresh) return current;
  if (current.expires && current.expires - Date.now() > REFRESH_MARGIN_MS) return current;
  // An account that is not saved yet (the check while connecting) uses the
  // token it just got: refresh tokens rotate, so refreshing without storing
  // the result would leave the saved connection with a dead one.
  if (!account.id) return current;
  const key = account.id || current.refresh;
  if (!refreshing.has(key)) refreshing.set(key, refresh(account, current).finally(() => refreshing.delete(key)));
  return refreshing.get(key);
}

// Whether a stored secret is a Portal sign-in (refreshable tokens) rather than
// a key pasted from the API-keys page.
function isSignIn(secret) {
  try {
    return Boolean(parseSecret(secret).refresh);
  } catch {
    return false;
  }
}

module.exports = { startDeviceLogin, pollDeviceLogin, freshCredentials, parseSecret, isSignIn, accountOf, CLIENT_ID, SCOPE };
