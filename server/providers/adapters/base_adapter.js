'use strict';

const { request } = require('../http');
const { KINDS } = require('../provider_error');
const { getConfig } = require('../../config/env');

// An adapter speaks one upstream dialect. It receives the provider definition
// (static facts) and the account (base URL, secret, options) and exposes:
//   validate()            -> { status: 'healthy'|'invalid'|'unknown', message }
//   listModels()          -> normalized model records (see model_info.js)
//   chat(req, ctx)        -> { response: canonical completion, headers }
//   stream(req, ctx)      -> async iterator of canonical chunks; ctx.onHeaders(headers)
//   fetchQuota()          -> optional [{ metric, limit, remaining, resetAt }]
class BaseAdapter {
  constructor(definition, account) {
    this.definition = definition;
    this.account = account;
    this.secret = account.secret || null;
    this.options = account.options || {};
    // Definitions may template account fields into the URL, e.g. {accountId}.
    this.baseUrl = String(account.baseUrl || definition.baseUrl || '')
      .replace(/\{(\w+)\}/g, (_match, field) => encodeURIComponent(this.options[field] || ''))
      .replace(/\/+$/, '');
  }

  get timeoutMs() {
    return this.definition.quirks?.timeoutMs || getConfig().upstreamTimeoutMs;
  }

  url(pathname) {
    if (/^https?:\/\//.test(pathname)) return pathname;
    return `${this.baseUrl}${pathname.startsWith('/') ? '' : '/'}${pathname}`;
  }

  authHeaders() {
    const auth = this.definition.auth || { type: 'bearer' };
    const secret = this.secret || auth.anonymousKey || null;
    if (!secret || auth.type === 'none') return {};
    // Custom endpoints may expect the key in a named header (e.g. x-api-key).
    if (this.options.authHeader) return { [this.options.authHeader]: secret };
    if (auth.type === 'header') return { [auth.header]: secret };
    return { Authorization: `Bearer ${secret}` };
  }

  headers(extra = {}) {
    const headers = {
      ...(this.definition.quirks?.userAgent ? { 'User-Agent': this.definition.quirks.userAgent } : {}),
      ...(this.definition.headers || {}),
      ...(this.options.headers || {}),
      ...this.authHeaders(),
      ...extra,
    };
    return headers;
  }

  async http(pathname, options = {}) {
    return request(this.url(pathname), {
      timeoutMs: this.timeoutMs,
      ...options,
      headers: this.headers(options.headers),
    });
  }

  // Default validation: an authenticated GET. 401/403 is a definite
  // rejection; anything else unexpected is "unknown", never "invalid".
  async validate() {
    const spec = this.definition.validate;
    if (spec === false) return { status: 'unknown', message: 'This provider cannot be validated without spending quota.' };
    const pathname = spec?.path || this.definition.models?.path || '/models';
    try {
      const { data } = await this.http(pathname, { timeoutMs: 20_000 });
      if (typeof data === 'string' && /<html/i.test(data)) {
        return { status: 'unknown', message: 'The provider returned an HTML page instead of JSON.' };
      }
      return { status: 'healthy', message: null };
    } catch (error) {
      if (error.kind === KINDS.AUTH) return { status: 'invalid', message: error.message };
      return { status: 'unknown', message: error.message };
    }
  }

  async listModels() {
    return [];
  }

  async fetchQuota() {
    return null;
  }
}

module.exports = { BaseAdapter };
