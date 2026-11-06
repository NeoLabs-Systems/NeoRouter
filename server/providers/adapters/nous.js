'use strict';

// Nous Portal over the device sign-in: the account secret holds the OAuth
// tokens as JSON, so the bearer is the inference JWT they carry, refreshed
// shortly before it expires. Everything else is OpenAI-compatible. An account
// connected with a pasted API key keeps that key as its bearer.
const { OpenAICompatibleAdapter } = require('./openai_compatible');
const { ProviderError, KINDS } = require('../provider_error');
const oauth = require('../../services/providers/nous_oauth');

class NousAdapter extends OpenAICompatibleAdapter {
  // The bearer for the call being made; prepare() keeps it current, because
  // the OpenAI-compatible paths read headers synchronously.
  token = null;

  // A pasted API key is a plain secret: no refresh, and no Portal account
  // endpoint to read credits from.
  isSignedIn() {
    return oauth.isSignIn(this.secret);
  }

  async prepare() {
    this.token = this.isSignedIn() ? (await oauth.freshCredentials(this.account)).access : this.secret;
    if (!this.token) throw new ProviderError(KINDS.AUTH, 'This Nous Portal connection has no valid sign-in. Connect it again.');
  }

  authHeaders() {
    return this.token ? { Authorization: `Bearer ${this.token}` } : {};
  }

  async validate() {
    await this.prepare().catch(() => {});
    return super.validate();
  }

  async listModels() {
    await this.prepare();
    return super.listModels();
  }

  async chat(canonical, ctx) {
    await this.prepare();
    return super.chat(canonical, ctx);
  }

  async *stream(canonical, ctx) {
    await this.prepare();
    yield* super.stream(canonical, ctx);
  }

  // Credits left, from Portal's own account endpoint: a signed-in connection
  // has them; an API key does not (Portal answers 401 for a key).
  async fetchQuota() {
    if (!this.isSignedIn()) return null;
    await this.prepare();
    const portal = (process.env.NEOROUTER_NOUS_PORTAL_URL || 'https://portal.nousresearch.com').replace(/\/+$/, '');
    const { data } = await this.request(`${portal}/api/oauth/account`, {
      headers: { Authorization: `Bearer ${this.token}`, Accept: 'application/json' },
      timeoutMs: 20_000,
    });
    const access = data?.paid_service_access || {};
    const remaining = Number(access.total_usable_credits ?? data?.subscription?.credits_remaining);
    return Number.isFinite(remaining) ? [{ metric: 'balance', limit: null, remaining }] : [];
  }
}

module.exports = { NousAdapter };
