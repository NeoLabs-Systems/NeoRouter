'use strict';

// Sign-in for the ChatGPT (Codex) connection, following the Codex CLI's own
// OAuth flow (openai/codex, codex-rs/login): a device code the user approves
// at auth.openai.com/codex/device, so it works on a server without a browser.
// Tokens are stored as the account secret (encrypted) and refreshed shortly
// before they expire.
const { getDatabase } = require('../../db/database');
const { encryptString } = require('../../utils/crypto');
const { ProviderError, KINDS } = require('../../providers/provider_error');

// Overridable for tests only.
const ISSUER = process.env.NEOROUTER_CODEX_ISSUER || 'https://auth.openai.com';
const CLIENT_ID = 'app_EMoamEEZ73f0CkXaXp7hrann';
const VERIFICATION_URL = `${ISSUER}/codex/device`;
const DEVICE_REDIRECT = `${ISSUER}/deviceauth/callback`;
const ACCOUNT_CLAIM = 'https://api.openai.com/auth';
const REFRESH_MARGIN_MS = 5 * 60_000;
const DEVICE_CODE_TTL_MS = 15 * 60_000;
// Sent as client_version when listing models; recent enough to see current models.
const CLIENT_VERSION = '0.120.0';

const pendingLogins = new Map();
const refreshing = new Map();

async function post(url, body, { form = false } = {}) {
  const response = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': form ? 'application/x-www-form-urlencoded' : 'application/json' },
    body: form ? new URLSearchParams(body) : JSON.stringify(body),
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

function accountIdOf(tokens) {
  for (const token of [tokens.id_token, tokens.access_token]) {
    const claims = decodeJwt(token)?.[ACCOUNT_CLAIM];
    if (claims?.chatgpt_account_id) return { accountId: claims.chatgpt_account_id, plan: claims.chatgpt_plan_type || null };
  }
  return { accountId: null, plan: null };
}

function credentialsFrom(tokens, previous = {}) {
  const { accountId, plan } = accountIdOf(tokens);
  return {
    access: tokens.access_token,
    refresh: tokens.refresh_token || previous.refresh,
    expires: Date.now() + (Number(tokens.expires_in) || 3600) * 1000,
    accountId: accountId || previous.accountId,
    plan: plan || previous.plan || null,
  };
}

/** Starts a device-code login: returns the code the user enters at the verification URL. */
async function startDeviceLogin() {
  const { ok, status, data } = await post(`${ISSUER}/api/accounts/deviceauth/usercode`, { client_id: CLIENT_ID });
  if (!ok || !data?.device_auth_id) {
    throw new ProviderError(status === 404 ? KINDS.NOT_FOUND : KINDS.SERVER, 'OpenAI did not start a device sign-in. Try again in a moment.');
  }
  const userCode = data.user_code || data.usercode;
  pendingLogins.set(data.device_auth_id, { userCode, expiresAt: Date.now() + DEVICE_CODE_TTL_MS });
  return { loginId: data.device_auth_id, userCode, verificationUrl: VERIFICATION_URL, interval: Number(data.interval) || 5, expiresInSeconds: DEVICE_CODE_TTL_MS / 1000 };
}

/** One poll: { status: 'pending' } until approved, then { status: 'done', secret } (JSON to store). */
async function pollDeviceLogin(loginId) {
  const pending = pendingLogins.get(loginId);
  if (!pending || pending.expiresAt < Date.now()) {
    pendingLogins.delete(loginId);
    return { status: 'expired' };
  }
  const poll = await post(`${ISSUER}/api/accounts/deviceauth/token`, { device_auth_id: loginId, user_code: pending.userCode });
  if (poll.status === 403 || poll.status === 404) return { status: 'pending' };
  if (!poll.ok || !poll.data?.authorization_code) throw new ProviderError(KINDS.AUTH, 'OpenAI refused the sign-in.');
  const exchange = await post(`${ISSUER}/oauth/token`, {
    grant_type: 'authorization_code',
    code: poll.data.authorization_code,
    redirect_uri: DEVICE_REDIRECT,
    client_id: CLIENT_ID,
    code_verifier: poll.data.code_verifier,
  }, { form: true });
  pendingLogins.delete(loginId);
  if (!exchange.ok || !exchange.data?.access_token) throw new ProviderError(KINDS.AUTH, 'OpenAI did not issue tokens for this sign-in.');
  const credentials = credentialsFrom(exchange.data);
  if (!credentials.accountId) throw new ProviderError(KINDS.AUTH, 'This OpenAI login has no ChatGPT account. Sign in with the account that has your ChatGPT plan.');
  return { status: 'done', secret: JSON.stringify(credentials), plan: credentials.plan };
}

function parseSecret(secret) {
  try {
    const value = JSON.parse(secret || '');
    if (value?.access && value?.refresh) return value;
  } catch {
    // fall through
  }
  throw new ProviderError(KINDS.AUTH, 'This ChatGPT connection has no valid sign-in. Connect it again.');
}

async function refresh(account, current) {
  const result = await post(`${ISSUER}/oauth/token`, { grant_type: 'refresh_token', refresh_token: current.refresh, client_id: CLIENT_ID }, { form: true });
  if (!result.ok || !result.data?.access_token) {
    throw new ProviderError(KINDS.AUTH, 'The ChatGPT sign-in expired or was revoked. Connect it again.');
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
  if (current.expires - Date.now() > REFRESH_MARGIN_MS) return current;
  // An account that is not saved yet (the check while connecting) uses the
  // token it just got: refresh tokens rotate, so refreshing without storing
  // the result would leave the saved connection with a dead one.
  if (!account.id) return current;
  const key = account.id || current.refresh;
  if (!refreshing.has(key)) refreshing.set(key, refresh(account, current).finally(() => refreshing.delete(key)));
  return refreshing.get(key);
}

module.exports = { startDeviceLogin, pollDeviceLogin, freshCredentials, parseSecret, accountIdOf, CLIENT_ID, CLIENT_VERSION, VERIFICATION_URL };
