'use strict';

const { BaseAdapter } = require('./base_adapter');
const { request, readSse, assertNoEmbeddedError } = require('../http');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

function stripSchemaKeys(value, keys) {
  if (Array.isArray(value)) return value.map((item) => stripSchemaKeys(item, keys));
  if (!value || typeof value !== 'object') return value;
  const result = {};
  for (const [key, inner] of Object.entries(value)) {
    if (!keys.includes(key)) result[key] = stripSchemaKeys(inner, keys);
  }
  return result;
}

function modelList(data) {
  if (Array.isArray(data)) return data;
  if (Array.isArray(data?.data)) return data.data;
  if (Array.isArray(data?.models)) return data.models;
  if (Array.isArray(data?.result)) return data.result;
  return [];
}

class OpenAICompatibleAdapter extends BaseAdapter {
  chatPath() {
    return this.definition.chatPath || '/chat/completions';
  }

  buildBody(canonical, modelId, stream) {
    const quirks = this.definition.quirks || {};
    const body = { ...canonical, model: modelId, stream };
    if (stream && quirks.streamUsage !== false) body.stream_options = { include_usage: true };
    if (body.max_tokens !== undefined && quirks.maxTokensField && quirks.maxTokensField !== 'max_tokens') {
      body[quirks.maxTokensField] = body.max_tokens;
      delete body.max_tokens;
    }
    if (quirks.singleToolCall && body.tools) body.parallel_tool_calls = false;
    for (const field of quirks.dropFields || []) delete body[field];
    if (quirks.stripSchemaKeys && body.tools) body.tools = stripSchemaKeys(body.tools, quirks.stripSchemaKeys);
    if (quirks.minMaxTokens && body.max_tokens !== undefined) body.max_tokens = Math.max(quirks.minMaxTokens, body.max_tokens);
    return body;
  }

  // Some providers serve /models publicly, so a GET proves nothing about the
  // key. Those definitions opt into a 1-token chat probe instead.
  async validate() {
    if (!this.definition.validate?.chatProbe) return super.validate();
    try {
      const model = (await this.listModels()).find((entry) => entry.kind === 'chat');
      if (!model) return { status: 'unknown', message: 'No chat model available to probe the key.' };
      await this.chat({ messages: [{ role: 'user', content: 'ping' }], max_tokens: 1 }, { modelId: model.id, timeoutMs: 30_000 });
      return { status: 'healthy', message: null };
    } catch (error) {
      if (error.kind === KINDS.AUTH) return { status: 'invalid', message: error.message };
      return { status: 'unknown', message: error.message };
    }
  }

  async listModels() {
    const spec = this.definition.models || {};
    if (spec.static) return spec.static.map((entry) => normalizeModel(entry));
    // Models the user typed in for endpoints whose /models is missing or incomplete.
    const manual = (this.options.manualModels || []).map((id) => normalizeModel({ id }));
    let listed = [];
    try {
      const { data } = await this.http(this.options.modelsPath || spec.path || '/models', { timeoutMs: 30_000 });
      listed = modelList(data)
        .map((entry) => (typeof entry === 'string' ? { id: entry } : entry))
        .map((entry) => normalizeModel(entry))
        .filter((model) => model.id);
    } catch (error) {
      if (!manual.length) throw error;
    }
    const known = new Set(listed.map((model) => model.id));
    return [...listed, ...manual.filter((model) => !known.has(model.id))];
  }

  async chat(canonical, { modelId, signal, timeoutMs }) {
    const { data, headers } = await this.http(this.chatPath(), {
      method: 'POST',
      body: this.buildBody(canonical, modelId, false),
      signal,
      timeoutMs: timeoutMs || this.timeoutMs,
    });
    if (typeof data !== 'object' || data === null) {
      throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned a non-JSON response.');
    }
    assertNoEmbeddedError(data);
    return { response: data, headers };
  }

  async *stream(canonical, { modelId, signal, onHeaders }) {
    const response = await request(this.url(this.chatPath()), {
      method: 'POST',
      headers: this.headers({ Accept: 'text/event-stream' }),
      body: this.buildBody(canonical, modelId, true),
      signal,
      raw: true,
    });
    onHeaders?.(response.headers);
    const contentType = response.headers.get('content-type') || '';
    if (contentType.includes('application/json')) {
      // Some upstreams ignore stream=true and answer with one JSON body.
      const data = await response.json();
      assertNoEmbeddedError(data);
      yield* completionAsChunks(data);
      return;
    }
    for await (const { data } of readSse(response)) {
      if (data === '[DONE]') return;
      let chunk;
      try {
        chunk = JSON.parse(data);
      } catch {
        continue;
      }
      assertNoEmbeddedError(chunk);
      yield chunk;
    }
  }

  async fetchQuota() {
    const probe = this.definition.quota?.probe;
    if (!probe) return null;
    const { data } = await this.http(probe.path, { timeoutMs: 20_000 });
    return probe.parse(data);
  }
}

function* completionAsChunks(data) {
  const choice = data.choices?.[0] || {};
  const message = choice.message || {};
  const base = { id: data.id, object: 'chat.completion.chunk', created: data.created, model: data.model };
  const delta = { role: 'assistant' };
  if (message.content) delta.content = message.content;
  if (message.reasoning_content) delta.reasoning_content = message.reasoning_content;
  if (message.tool_calls) delta.tool_calls = message.tool_calls.map((call, index) => ({ index, ...call }));
  yield { ...base, choices: [{ index: 0, delta, finish_reason: null }] };
  yield { ...base, choices: [{ index: 0, delta: {}, finish_reason: choice.finish_reason || 'stop' }], ...(data.usage ? { usage: data.usage } : {}) };
}

module.exports = { OpenAICompatibleAdapter, completionAsChunks, stripSchemaKeys, modelList };
