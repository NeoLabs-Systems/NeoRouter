'use strict';

// ChatGPT through the Codex login: the user's ChatGPT plan (Plus, Pro)
// instead of API credit, the way OpenClaw and OpenCode connect it. The
// Codex backend speaks the Responses API (stateless: store=false, always
// streamed), so chat requests are translated both ways here. The account
// secret holds the OAuth tokens as JSON; they are refreshed when needed.
const crypto = require('node:crypto');
const { BaseAdapter } = require('./base_adapter');
const { readSse } = require('../http');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS, classifyHttpError } = require('../provider_error');
const { completionId, buildChunk, buildResponse, textOf, contentParts, StreamAccumulator } = require('../../capabilities/chat/canonical');
const oauth = require('../../services/providers/codex_oauth');

// The backend may require Codex's own instructions; neutral ones are tried first.
const NEUTRAL_INSTRUCTIONS = 'You are a helpful assistant. Follow the developer and user messages.';
const PROMPT_URL = 'https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/prompt.md';
const BRIDGE = 'You are answering an API request routed through NeoRouter, not working in a terminal: ignore any coding-agent instructions above about files, shells or sandboxes, and follow the developer and user messages. Use only the tools listed in this request.';
let codexPrompt = null;
let needsCodexPrompt = false;

async function officialInstructions() {
  if (!codexPrompt) {
    const response = await fetch(PROMPT_URL, { signal: AbortSignal.timeout(15_000) });
    if (!response.ok) throw new ProviderError(KINDS.SERVER, 'Could not load the Codex instructions the ChatGPT backend requires.');
    codexPrompt = await response.text();
  }
  return codexPrompt;
}

function imagePart(part) {
  const url = typeof part.image_url === 'string' ? part.image_url : part.image_url?.url;
  return { type: 'input_image', image_url: url };
}

// Canonical (OpenAI Chat) messages → Responses input items.
function toInput(messages) {
  const input = [];
  for (const message of messages) {
    if (message.role === 'system' || message.role === 'developer') {
      input.push({ type: 'message', role: 'developer', content: [{ type: 'input_text', text: textOf(message.content) }] });
    } else if (message.role === 'user') {
      const parts = contentParts(message.content).map((part) => (part.type === 'image_url' ? imagePart(part) : { type: 'input_text', text: part.text ?? '' }));
      input.push({ type: 'message', role: 'user', content: parts });
    } else if (message.role === 'assistant') {
      const text = textOf(message.content);
      if (text) input.push({ type: 'message', role: 'assistant', content: [{ type: 'output_text', text }] });
      for (const call of message.tool_calls || []) {
        input.push({ type: 'function_call', call_id: call.id, name: call.function?.name, arguments: call.function?.arguments || '{}' });
      }
    } else if (message.role === 'tool') {
      input.push({ type: 'function_call_output', call_id: message.tool_call_id, output: textOf(message.content) });
    }
  }
  return input;
}

function toolChoice(choice) {
  if (!choice || typeof choice === 'string') return choice;
  if (choice.type === 'function') return { type: 'function', name: choice.function?.name };
  return undefined;
}

function buildBody(canonical, modelId, instructions) {
  const body = {
    model: modelId,
    instructions,
    input: [...(instructions === NEUTRAL_INSTRUCTIONS ? [] : [{ type: 'message', role: 'developer', content: [{ type: 'input_text', text: BRIDGE }] }]), ...toInput(canonical.messages)],
    store: false,
    stream: true,
    include: ['reasoning.encrypted_content'],
  };
  if (canonical.tools?.length) {
    body.tools = canonical.tools.map((tool) => ({ type: 'function', name: tool.function.name, description: tool.function.description || '', parameters: tool.function.parameters || { type: 'object', properties: {} }, strict: false }));
    body.tool_choice = toolChoice(canonical.tool_choice) ?? 'auto';
    body.parallel_tool_calls = canonical.parallel_tool_calls !== false;
  }
  if (canonical.reasoning_effort) body.reasoning = { effort: canonical.reasoning_effort, summary: 'auto' };
  return body;
}

// Responses stream events → canonical chunks.
async function* toChunks(events, modelId) {
  const id = completionId();
  const created = Math.floor(Date.now() / 1000);
  const calls = new Map();
  let started = false;
  let sawTools = false;
  let usage = null;
  let finish = 'stop';
  const chunk = (delta, finishReason = null, extra) => buildChunk({ id, model: modelId, created, delta, finishReason, usage: extra });
  for await (const { data } of events) {
    let event;
    try { event = JSON.parse(data); } catch { continue; }
    if (!started) {
      started = true;
      yield chunk({ role: 'assistant', content: '' });
    }
    switch (event.type) {
      case 'response.output_text.delta':
        if (event.delta) yield chunk({ content: event.delta });
        break;
      case 'response.reasoning_summary_text.delta':
        if (event.delta) yield chunk({ reasoning_content: event.delta });
        break;
      case 'response.output_item.added':
        if (event.item?.type === 'function_call') {
          const index = calls.size;
          calls.set(event.item.id, index);
          sawTools = true;
          yield chunk({ tool_calls: [{ index, id: event.item.call_id, type: 'function', function: { name: event.item.name, arguments: '' } }] });
        }
        break;
      case 'response.function_call_arguments.delta':
        if (calls.has(event.item_id) && event.delta) yield chunk({ tool_calls: [{ index: calls.get(event.item_id), function: { arguments: event.delta } }] });
        break;
      case 'response.completed':
      case 'response.incomplete': {
        const raw = event.response?.usage;
        if (raw) usage = { prompt_tokens: raw.input_tokens || 0, completion_tokens: raw.output_tokens || 0, total_tokens: (raw.input_tokens || 0) + (raw.output_tokens || 0) };
        if (event.type === 'response.incomplete') finish = 'length';
        break;
      }
      case 'response.failed':
      case 'error': {
        const error = event.response?.error || event.error || event;
        const status = /rate|limit|quota|usage/i.test(`${error.code} ${error.message}`) ? 429 : 502;
        throw classifyHttpError(status, { error: { message: error.message || 'The ChatGPT backend reported an error.' } }, null);
      }
      default:
        break;
    }
  }
  yield chunk({}, sawTools ? 'tool_calls' : finish);
  if (usage) yield buildChunk({ id, model: modelId, created, delta: {}, usage });
}

class CodexAdapter extends BaseAdapter {
  async credentials() {
    return oauth.freshCredentials(this.account);
  }

  async codexHeaders(extra = {}) {
    const credentials = await this.credentials();
    return {
      Authorization: `Bearer ${credentials.access}`,
      'chatgpt-account-id': credentials.accountId,
      'OpenAI-Beta': 'responses=experimental',
      // The Codex backend expects the Codex client's originator.
      originator: 'codex_cli_rs',
      session_id: crypto.randomUUID(),
      ...extra,
    };
  }

  async validate() {
    try {
      await this.credentials();
      await this.listModels();
      return { status: 'healthy', message: null };
    } catch (error) {
      if (error.kind === KINDS.AUTH) return { status: 'invalid', message: error.message };
      return { status: 'unknown', message: error.message };
    }
  }

  async listModels() {
    const { data } = await this.request(`${this.baseUrl}/models?client_version=${encodeURIComponent(await oauth.clientVersion())}`, { headers: await this.codexHeaders(), timeoutMs: 30_000 });
    const models = (data?.models || []).filter((model) => model.slug && (model.visibility === undefined || model.visibility === 'list'));
    return models.map((model) => normalizeModel({ id: model.slug, name: model.display_name || model.slug, context_length: model.context_window || null }, { tools: true, vision: true, reasoning: true }));
  }

  async open(canonical, modelId, signal, timeoutMs) {
    const send = async (instructions) => this.request(`${this.baseUrl}/responses`, {
      method: 'POST', headers: await this.codexHeaders({ Accept: 'text/event-stream' }), body: buildBody(canonical, modelId, instructions), signal, timeoutMs, raw: true,
    });
    if (needsCodexPrompt) return send(await officialInstructions());
    try {
      return await send(NEUTRAL_INSTRUCTIONS);
    } catch (error) {
      if (error.kind !== KINDS.BAD_REQUEST || !/instruction/i.test(error.message)) throw error;
      needsCodexPrompt = true;
      return send(await officialInstructions());
    }
  }

  async *stream(canonical, { modelId, signal, onHeaders, timeoutMs }) {
    const response = await this.open(canonical, modelId, signal, timeoutMs);
    onHeaders?.(response.headers);
    yield* toChunks(readSse(response), modelId);
  }

  // The backend only streams; a plain request folds the stream.
  async chat(canonical, { modelId, signal, timeoutMs }) {
    const response = await this.open(canonical, modelId, signal, timeoutMs);
    const accumulator = new StreamAccumulator();
    for await (const piece of toChunks(readSse(response), modelId)) accumulator.push(piece);
    if (!accumulator.hasAnswer()) throw new ProviderError(KINDS.EMPTY, 'ChatGPT returned an empty response.');
    return {
      response: buildResponse({ model: modelId, message: accumulator.toMessage(), finishReason: accumulator.finishReason || 'stop', usage: accumulator.usage }),
      headers: response.headers,
    };
  }
}

module.exports = { CodexAdapter, toInput, buildBody, toChunks };
