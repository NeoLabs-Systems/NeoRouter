'use strict';

const crypto = require('node:crypto');
const { HttpError } = require('../../middleware/error_handler');
const keys = require('../router_keys/router_key_service');
const { randomToken, sha256, timingSafeStringEqual } = require('../../utils/crypto');

// "Connect with NeoRouter": apps get a router key without copy-paste.
// Two standard OAuth 2.0 flows, for public clients only (no client secret):
// - authorization code with PKCE (RFC 7636) for apps with a browser callback;
// - the device flow (RFC 8628) for apps without one, such as a server on the
//   local network or a CLI: the app shows a short code, the user approves it
//   here and the app polls for the key.
// Apps don't register. They name themselves; the person approving sees that
// name and where the key goes, and decides what the key may use. The key is
// only made when the app exchanges its code, so abandoned approvals leave
// nothing behind. Pending requests live in memory for a few minutes.
const REQUEST_TTL_MS = 10 * 60_000;
const CODE_TTL_MS = 2 * 60_000;
const DEVICE_INTERVAL_S = 5;
const USER_CODE_ALPHABET = 'BCDFGHJKLMNPQRSTVWXZ';
// What an app may say it will use (the gateway's capabilities plus live voice).
const SCOPES = ['chat', 'embedding', 'image', 'speech', 'transcription', 'live', 'moderation', 'video', 'search'];
const UNSAFE_SCHEMES = new Set(['javascript:', 'data:', 'file:', 'vbscript:', 'blob:', 'about:']);

const requests = new Map();
const codes = new Map();

const oauthError = (code, description, status = 400) => Object.assign(new HttpError(status, code, description), { oauth: { error: code, error_description: description } });

function prune(now = Date.now()) {
  for (const [id, item] of requests) if (item.expiresAt <= now) requests.delete(id);
  for (const [code, item] of codes) if (item.expiresAt <= now) codes.delete(code);
}

function isLoopback(hostname) {
  return ['localhost', '127.0.0.1', '[::1]', '::1'].includes(hostname);
}

// Any absolute URL without a fragment, except schemes that run code. Custom
// schemes (mobile apps) are allowed; PKCE makes an intercepted code useless.
function parseRedirect(value) {
  let url;
  try {
    url = new URL(String(value || ''));
  } catch {
    return null;
  }
  if (UNSAFE_SCHEMES.has(url.protocol) || url.hash) return null;
  return url;
}

function describeRedirect(url) {
  const web = url.protocol === 'http:' || url.protocol === 'https:';
  return {
    host: web ? url.host : url.protocol.replace(/:$/, ''),
    // Plain http beyond this machine can be read on the network.
    secure: url.protocol !== 'http:' || isLoopback(url.hostname),
  };
}

function parseScopes(value) {
  const asked = String(value || '').split(/[\s,]+/).filter(Boolean);
  return [...new Set(asked.filter((scope) => SCOPES.includes(scope)))];
}

function appName(clientId, clientName) {
  const name = String(clientName || clientId || '').trim().replace(/\s+/g, ' ');
  return name.slice(0, 80);
}

function userCode() {
  const bytes = crypto.randomBytes(8);
  const chars = [...bytes].map((byte) => USER_CODE_ALPHABET[byte % USER_CODE_ALPHABET.length]).join('');
  return `${chars.slice(0, 4)}-${chars.slice(4)}`;
}

const normalizeUserCode = (value) => String(value || '').toUpperCase().replace(/[^A-Z]/g, '');

/**
 * Validates an authorization request (GET /oauth/authorize). Errors the app
 * should hear about come back with `redirect`; a bad redirect_uri never
 * redirects anywhere.
 */
function startAuthorization(query) {
  prune();
  const redirect = parseRedirect(query.redirect_uri);
  if (!redirect) throw oauthError('invalid_request', 'redirect_uri must be an absolute URL.');
  const clientId = String(query.client_id || '').trim();
  const fail = (error, description) => {
    const url = new URL(redirect);
    url.searchParams.set('error', error);
    url.searchParams.set('error_description', description);
    if (query.state) url.searchParams.set('state', String(query.state));
    return { redirect: url.toString() };
  };
  if (!clientId || clientId.length > 200) return fail('invalid_request', 'client_id is required.');
  if (query.response_type !== 'code') return fail('unsupported_response_type', 'Only response_type=code is supported.');
  if (!/^[A-Za-z0-9_-]{43,128}$/.test(String(query.code_challenge || ''))) return fail('invalid_request', 'A PKCE code_challenge is required.');
  if ((query.code_challenge_method || 'S256') !== 'S256') return fail('invalid_request', 'Only code_challenge_method=S256 is supported.');
  const id = randomToken(18);
  requests.set(id, {
    id,
    kind: 'code',
    clientId,
    name: appName(clientId, query.client_name),
    redirectUri: redirect.toString(),
    redirect: describeRedirect(redirect),
    challenge: String(query.code_challenge),
    state: query.state ? String(query.state).slice(0, 1024) : null,
    scopes: parseScopes(query.scope),
    expiresAt: Date.now() + REQUEST_TTL_MS,
    status: 'pending',
  });
  return { requestId: id };
}

/** Starts a device-flow request (POST /oauth/device/code). */
function startDevice(body, origin) {
  prune();
  const clientId = String(body.client_id || '').trim();
  if (!clientId || clientId.length > 200) throw oauthError('invalid_request', 'client_id is required.');
  const id = randomToken(18);
  const deviceCode = randomToken(32);
  const code = userCode();
  requests.set(id, {
    id,
    kind: 'device',
    clientId,
    name: appName(clientId, body.client_name),
    redirect: null,
    deviceCodeHash: sha256(deviceCode),
    userCode: normalizeUserCode(code),
    scopes: parseScopes(body.scope),
    expiresAt: Date.now() + REQUEST_TTL_MS,
    interval: DEVICE_INTERVAL_S,
    lastPollAt: 0,
    status: 'pending',
  });
  const app = `${origin}/app/`;
  return {
    device_code: deviceCode,
    user_code: code,
    verification_uri: `${app}?connect=`,
    verification_uri_complete: `${app}?connect=${encodeURIComponent(id)}`,
    expires_in: REQUEST_TTL_MS / 1000,
    interval: DEVICE_INTERVAL_S,
  };
}

// A request by its id (from the link) or by the code a user typed.
function find(reference) {
  prune();
  const byId = requests.get(String(reference || ''));
  if (byId) return byId;
  const typed = normalizeUserCode(reference);
  if (typed.length !== 8) return null;
  return [...requests.values()].find((item) => item.kind === 'device' && item.userCode === typed) || null;
}

function pending(reference) {
  const item = find(reference);
  if (!item) throw new HttpError(404, 'CONNECT_REQUEST_NOT_FOUND', 'This connection request expired or does not exist. Start it again in the app.');
  if (item.status !== 'pending') throw new HttpError(409, 'CONNECT_REQUEST_ANSWERED', 'This connection request was already answered.');
  return item;
}

/** What the approval screen shows. */
function describe(reference) {
  const item = pending(reference);
  return {
    id: item.id,
    kind: item.kind,
    name: item.name,
    clientId: item.clientId,
    redirect: item.redirect,
    scopes: item.scopes,
    expiresAt: new Date(item.expiresAt).toISOString(),
  };
}

function withParams(uri, params) {
  const url = new URL(uri);
  for (const [key, value] of Object.entries(params)) if (value !== null && value !== undefined) url.searchParams.set(key, value);
  return url.toString();
}

/**
 * The user approved: `access` holds the key options (free/plan/paid, budget).
 * Returns where the browser goes next (code flow) or nothing (device flow).
 */
function approve(reference, { ownerId, actorId, access }) {
  const item = pending(reference);
  // Validates the options now, so the app never receives a failing code.
  keys.kinds(access);
  const approval = { ownerId, actorId, access };
  if (item.kind === 'device') {
    item.status = 'approved';
    item.approval = approval;
    return { redirect: null };
  }
  requests.delete(item.id);
  const code = randomToken(32);
  codes.set(code, { request: item, approval, expiresAt: Date.now() + CODE_TTL_MS });
  return { redirect: withParams(item.redirectUri, { code, state: item.state }) };
}

function deny(reference) {
  const item = pending(reference);
  if (item.kind === 'device') {
    item.status = 'denied';
    return { redirect: null };
  }
  requests.delete(item.id);
  return { redirect: withParams(item.redirectUri, { error: 'access_denied', error_description: 'The request was declined.', state: item.state }) };
}

function issueKey(request, approval, origin) {
  const { access } = approval;
  const { key, secret } = keys.create(approval.ownerId, {
    name: request.name,
    allowFree: access.allowFree,
    allowPlan: access.allowPlan,
    allowPaid: access.allowPaid,
    budgetUsd: access.allowPaid ? access.budgetUsd ?? null : null,
    budgetPeriod: access.budgetPeriod,
  }, approval.actorId, { name: request.name, origin: request.redirect?.host || null, scopes: request.scopes });
  return {
    access_token: secret,
    token_type: 'Bearer',
    scope: request.scopes.join(' '),
    // Where the key works, so apps need nothing else.
    base_url: `${origin}/v1`,
    key_id: key.id,
  };
}

function exchangeCode(body, origin) {
  const entry = codes.get(String(body.code || ''));
  codes.delete(String(body.code || ''));
  if (!entry || entry.expiresAt <= Date.now()) throw oauthError('invalid_grant', 'The code is invalid or expired.');
  const { request } = entry;
  if (String(body.client_id || '') !== request.clientId) throw oauthError('invalid_grant', 'client_id does not match the authorization request.');
  if (String(body.redirect_uri || '') !== request.redirectUri) throw oauthError('invalid_grant', 'redirect_uri does not match the authorization request.');
  const verifier = String(body.code_verifier || '');
  const challenge = crypto.createHash('sha256').update(verifier).digest('base64url');
  if (!verifier || !timingSafeStringEqual(challenge, request.challenge)) throw oauthError('invalid_grant', 'The code_verifier does not match.');
  return issueKey(request, entry.approval, origin);
}

function exchangeDevice(body, origin, now = Date.now()) {
  prune(now);
  const hash = sha256(String(body.device_code || ''));
  const item = [...requests.values()].find((entry) => entry.kind === 'device' && entry.deviceCodeHash === hash);
  if (!item) throw oauthError('expired_token', 'The device code expired. Start again.');
  if (String(body.client_id || '') !== item.clientId) throw oauthError('invalid_grant', 'client_id does not match the device request.');
  if (item.status === 'denied') {
    requests.delete(item.id);
    throw oauthError('access_denied', 'The request was declined.');
  }
  if (item.status === 'pending') {
    const early = now - item.lastPollAt < item.interval * 1000;
    item.lastPollAt = now;
    if (early) {
      item.interval += 5;
      throw oauthError('slow_down', 'Polling too often.');
    }
    throw oauthError('authorization_pending', 'Waiting for the user to approve.');
  }
  requests.delete(item.id);
  return issueKey(item, item.approval, origin);
}

/** POST /oauth/token. */
function exchange(body, origin) {
  if (body.grant_type === 'authorization_code') return exchangeCode(body, origin);
  if (body.grant_type === 'urn:ietf:params:oauth:grant-type:device_code') return exchangeDevice(body, origin);
  throw oauthError('unsupported_grant_type', 'Use authorization_code or the device_code grant.');
}

function metadata(origin) {
  return {
    issuer: origin,
    authorization_endpoint: `${origin}/oauth/authorize`,
    token_endpoint: `${origin}/oauth/token`,
    device_authorization_endpoint: `${origin}/oauth/device/code`,
    response_types_supported: ['code'],
    grant_types_supported: ['authorization_code', 'urn:ietf:params:oauth:grant-type:device_code'],
    code_challenge_methods_supported: ['S256'],
    token_endpoint_auth_methods_supported: ['none'],
    scopes_supported: SCOPES,
    // NeoRouter extension: where issued keys are used.
    api_base_url: `${origin}/v1`,
  };
}

function resetForTests() {
  requests.clear();
  codes.clear();
}

module.exports = { startAuthorization, startDevice, describe, approve, deny, exchange, metadata, SCOPES, resetForTests };
