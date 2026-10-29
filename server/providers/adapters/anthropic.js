'use strict';

const { BaseAdapter } = require('./base_adapter');
const { readSse } = require('../http');
const { normalizeModel } = require('../model_info');
const { classifyHttpError, ProviderError, KINDS } = require('../provider_error');
const translator = require('../../capabilities/chat/translators/anthropic_messages');

const API_VERSION = '2023-06-01';

class AnthropicAdapter extends BaseAdapter {
  authHeaders() {
    if (!this.secret) return {};
    return { 'x-api-key': this.secret, 'anthropic-version': API_VERSION };
  }

  async listModels() {
    const models = [];
    let after = null;
    for (let page = 0; page < 20; page += 1) {
      const { data } = await this.http(`/models?limit=1000${after ? `&after_id=${encodeURIComponent(after)}` : ''}`, { timeoutMs: 30_000 });
      for (const entry of data?.data || []) {
        models.push(normalizeModel({
          ...entry,
          context_window: entry.max_input_tokens,
          max_output_tokens: entry.max_tokens,
        }, { tools: true, vision: true }));
      }
      if (!data?.has_more || !data.last_id) break;
      after = data.last_id;
    }
    return models;
  }

  body(canonical, modelId, stream, maxOutput) {
    return { ...translator.fromCanonical(canonical, { defaultMaxTokens: Math.min(maxOutput || 8192, 8192) }), model: modelId, stream };
  }

  async chat(canonical, { modelId, signal, timeoutMs, model }) {
    const { data, headers } = await this.http('/messages', {
      method: 'POST',
      body: this.body(canonical, modelId, false, model?.maxOutput),
      signal,
      timeoutMs: timeoutMs || this.timeoutMs,
    });
    if (data?.type === 'error') throw classifyHttpError(502, data, headers);
    return { response: translator.responseToCanonical(data, modelId), headers };
  }

  async *stream(canonical, { modelId, signal, onHeaders, model }) {
    const response = await this.request(this.url('/messages'), {
      method: 'POST',
      headers: this.headers({ Accept: 'text/event-stream' }),
      body: this.body(canonical, modelId, true, model?.maxOutput),
      signal,
      raw: true,
    });
    onHeaders?.(response.headers);
    let failure = null;
    yield* translator.streamToCanonical(readSse(response), modelId, (payload) => {
      const status = payload.error?.type === 'overloaded_error' ? 529 : payload.error?.type === 'rate_limit_error' ? 429 : 500;
      failure = classifyHttpError(status, payload, null);
    });
    if (failure) throw failure instanceof ProviderError ? failure : new ProviderError(KINDS.SERVER, String(failure));
  }
}

module.exports = { AnthropicAdapter };
