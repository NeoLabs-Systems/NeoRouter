'use strict';

// Error kinds drive routing decisions (penalties, cooldowns, fallback). Keep
// this list closed: every kind must be handled in routing/learning.js.
const KINDS = Object.freeze({
  AUTH: 'auth',
  RATE_LIMIT: 'rate_limit',
  QUOTA_EXHAUSTED: 'quota_exhausted',
  PAYMENT: 'payment',
  CONTEXT_LENGTH: 'context_length',
  BAD_REQUEST: 'bad_request',
  NOT_FOUND: 'not_found',
  SERVER: 'server',
  TIMEOUT: 'timeout',
  NETWORK: 'network',
  EMPTY: 'empty',
  INVALID_OUTPUT: 'invalid_output',
  TRUNCATED: 'truncated',
  ABORTED: 'aborted',
});

class ProviderError extends Error {
  // needsAction: only a person can fix this (a consent, a verification, a
  // plan); trying again by itself cannot help.
  constructor(kind, message, { status = null, retryAfterMs = null, body = null, headers = null, learned = null, needsAction = false } = {}) {
    super(message);
    this.name = 'ProviderError';
    this.kind = kind;
    this.status = status;
    this.retryAfterMs = retryAfterMs;
    this.body = body;
    this.headers = headers;
    this.learned = learned;
    this.needsAction = needsAction;
  }
}

const QUOTA_PATTERNS = [
  /per.?day/i, /daily/i, /quota/i, /exhaust/i, /\brpd\b/i, /\btpd\b/i, /monthly/i, /(per|\/) ?month\b/i, /insufficient.?(credit|balance|quota)/i,
];
const USAGE_LIMIT = /exceeds? your plan'?s (set )?usage limit|(usage|quota|credit) limit (exceeded|reached)|out of (search )?credits/i;
const CONTEXT_PATTERNS = [
  /context.?(length|window)/i, /maximum.?context/i, /too many tokens/i, /prompt is too long/i,
  /reduce the length/i, /input.{0,20}too long/i, /exceeds.{0,30}(token|context)/i,
];

function parseRetryAfter(headers) {
  if (!headers) return null;
  const get = (name) => (typeof headers.get === 'function' ? headers.get(name) : headers[name]);
  const raw = get('retry-after');
  if (raw) {
    const seconds = Number(raw);
    if (Number.isFinite(seconds)) return Math.max(0, seconds * 1000);
    const date = Date.parse(raw);
    if (Number.isFinite(date)) return Math.max(0, date - Date.now());
  }
  const ms = Number(get('retry-after-ms'));
  return Number.isFinite(ms) && ms > 0 ? ms : null;
}

function extractMessage(body) {
  if (!body) return '';
  if (typeof body === 'string') return body.slice(0, 2000);
  // Cloudflare: { errors: [{ message }] }; ElevenLabs: { detail: { message } };
  // Brave: { error: { detail } }; Tavily: { detail: { error } }.
  const candidate = body.error?.message || body.error?.msg || (typeof body.error?.detail === 'string' && body.error.detail) || body.message
    || body.detail?.message || body.detail?.error || body.detail || body.errors?.[0]?.message || body.error;
  if (typeof candidate === 'string') return candidate.slice(0, 2000);
  return JSON.stringify(body).slice(0, 2000);
}

// Pulls "limit: 1000"-style numbers out of rate-limit messages so the quota
// tracker can learn limits that the provider does not publish in headers.
function learnFromMessage(message) {
  const text = String(message || '');
  const match = text.match(/limit[^0-9]{0,20}(\d[\d,]*)/i);
  if (!match) return null;
  const value = Number(match[1].replace(/,/g, ''));
  if (!Number.isFinite(value) || value < 0) return null;
  // "limit: 0" means this model has no free quota at all on this account.
  if (value === 0) return { metric: 'rpd', value: 0 };
  let metric = null;
  const tokens = /token/i.test(text);
  if (/per.?minute|\bpm\b|rpm|tpm/i.test(text)) metric = tokens ? 'tpm' : 'rpm';
  else if (/per.?day|daily|rpd|tpd/i.test(text)) metric = tokens ? 'tpd' : 'rpd';
  return metric ? { metric, value } : null;
}

const ACCESS_REFUSED = /not available on the .{0,30}plan|model agreement|not allowed to access|needs your consent|phone verification|verify your (phone|account)|upgrade your plan|requires? global regions?|objected to passing your inputs|data (policy|retention) settings?/i;

// "back at 12:00 UTC", "back in about 11 min": a planned outage that says when it ends.
function announcedReturn(message, now = Date.now()) {
  const text = String(message || '');
  const minutes = text.match(/back in (?:about |around |~)?(\d{1,3}) ?min/i);
  if (minutes) return Number(minutes[1]) * 60_000;
  const clock = text.match(/back (?:at|by|from) (\d{1,2}):(\d{2}) ?UTC/i);
  if (!clock) return null;
  const at = new Date(now);
  at.setUTCHours(Number(clock[1]), Number(clock[2]), 0, 0);
  if (at.getTime() <= now) at.setUTCDate(at.getUTCDate() + 1);
  return at.getTime() - now;
}
// The account cannot pay: no balance, or no subscription behind the key. The
// status is not reliable here (OpenCode Go says 403 for it).
const NEEDS_PAYMENT = /(active|valid).{0,40}subscription is required|insufficient (account )?(funds|balance|credits?)/i;

function classifyHttpError(status, body, headers) {
  const message = extractMessage(body) || `HTTP ${status}`;
  const retryAfterMs = parseRetryAfter(headers);
  const options = { status, retryAfterMs, body, headers };
  if (CONTEXT_PATTERNS.some((pattern) => pattern.test(message)) && (status === 400 || status === 413 || status === 422)) {
    return new ProviderError(KINDS.CONTEXT_LENGTH, message, options);
  }
  // A model this account may not use yet (Groq: "requires terms acceptance")
  // will keep failing the same way: treat it as unavailable, not as a bad request.
  if ((status === 400 || status === 403) && /terms acceptance|accept the terms|requires? (you to )?accept/i.test(message)) {
    return new ProviderError(KINDS.NOT_FOUND, message, { ...options, needsAction: true });
  }
  if ((status === 400 || status === 401 || status === 403) && NEEDS_PAYMENT.test(message)) {
    return new ProviderError(KINDS.PAYMENT, message, options);
  }
  // A setting only a person can change (a workspace's region): this model, not the key.
  if (status === 400 && ACCESS_REFUSED.test(message)) return new ProviderError(KINDS.NOT_FOUND, message, { ...options, needsAction: true });
  if (status === 401 || status === 403) {
    if (status === 403 && /region|country|location|not available in your|not (yet )?available to you|being rolled out|no access to (this )?model/i.test(message)) {
      return new ProviderError(KINDS.NOT_FOUND, message, options);
    }
    // The key is fine; this model or plan wants something only a person can
    // do. Retrying soon or re-checking the key cannot help.
    if (ACCESS_REFUSED.test(message)) return new ProviderError(KINDS.NOT_FOUND, message, { ...options, needsAction: true });
    return new ProviderError(KINDS.AUTH, message, options);
  }
  if (status === 402) return new ProviderError(KINDS.PAYMENT, message, options);
  // The plan's allowance is used up until it renews (Tavily answers 432 for
  // its plan limit and 433 for a pay-as-you-go cap): nothing wrong with the key.
  // A 429 is read below: "usage limit reached" there is often a short window.
  if (status === 432 || status === 433 || (status !== 429 && USAGE_LIMIT.test(message))) return new ProviderError(KINDS.QUOTA_EXHAUSTED, message, options);
  if (status === 404) return new ProviderError(KINDS.NOT_FOUND, message, options);
  if (status === 408) return new ProviderError(KINDS.TIMEOUT, message, options);
  if (status === 429) {
    const kind = QUOTA_PATTERNS.some((pattern) => pattern.test(message)) ? KINDS.QUOTA_EXHAUSTED : KINDS.RATE_LIMIT;
    return new ProviderError(kind, message, { ...options, learned: learnFromMessage(message) });
  }
  if (status >= 500) return new ProviderError(KINDS.SERVER, message, { ...options, retryAfterMs: retryAfterMs ?? announcedReturn(message) });
  return new ProviderError(KINDS.BAD_REQUEST, message, options);
}

function fromNetworkError(error) {
  if (error instanceof ProviderError) return error;
  if (error?.name === 'TimeoutError') return new ProviderError(KINDS.TIMEOUT, 'The provider did not respond in time.');
  if (error?.name === 'AbortError') return new ProviderError(KINDS.ABORTED, 'The request was aborted.');
  const cause = error?.cause?.code || error?.code || '';
  return new ProviderError(KINDS.NETWORK, `Network error${cause ? ` (${cause})` : ''}: ${error?.message || error}`);
}

module.exports = { KINDS, ProviderError, classifyHttpError, fromNetworkError, parseRetryAfter, extractMessage, learnFromMessage, announcedReturn };
