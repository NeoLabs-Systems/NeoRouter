'use strict';

// The canonical chat format is the OpenAI Chat Completions wire format. It is
// the most widely implemented dialect, so most upstreams need no translation
// and the Anthropic/Responses translators only have to map to one shape.

const crypto = require('node:crypto');

const PASSTHROUGH_FIELDS = [
  'temperature', 'top_p', 'stop', 'seed', 'presence_penalty', 'frequency_penalty', 'n', 'logit_bias',
  'logprobs', 'top_logprobs', 'user', 'parallel_tool_calls', 'reasoning_effort', 'response_format', 'tool_choice',
];

function completionId() {
  return `chatcmpl-${crypto.randomBytes(12).toString('hex')}`;
}

function normalizeRequest(body) {
  const request = {
    messages: Array.isArray(body.messages) ? body.messages : [],
    stream: body.stream === true,
  };
  if (Array.isArray(body.tools) && body.tools.length) request.tools = body.tools;
  const maxTokens = body.max_completion_tokens ?? body.max_tokens;
  if (Number.isFinite(maxTokens)) request.max_tokens = maxTokens;
  for (const field of PASSTHROUGH_FIELDS) {
    if (body[field] !== undefined && body[field] !== null) request[field] = body[field];
  }
  return request;
}

function contentParts(content) {
  if (typeof content === 'string') return [{ type: 'text', text: content }];
  return Array.isArray(content) ? content : [];
}

function textOf(content) {
  if (typeof content === 'string') return content;
  return contentParts(content).filter((part) => part.type === 'text').map((part) => part.text || '').join('');
}

// What the request needs from a model. Routing uses this as hard filters.
function requirements(request) {
  const hasVision = request.messages.some((message) => contentParts(message.content)
    .some((part) => part.type === 'image_url' || part.type === 'input_image' || part.type === 'image'));
  const format = request.response_format?.type;
  return {
    tools: Boolean(request.tools?.length),
    vision: hasVision,
    json: format === 'json_object' || format === 'json_schema',
    stream: request.stream,
  };
}

// Rough token estimate (≈4 chars per token, images counted flat). Used only
// for routing and budget reservations; billing uses provider-reported usage.
function estimateInputTokens(request) {
  let chars = 0;
  let images = 0;
  for (const message of request.messages) {
    for (const part of contentParts(message.content)) {
      if (part.type === 'text') chars += (part.text || '').length;
      else images += 1;
    }
    if (message.tool_calls) chars += JSON.stringify(message.tool_calls).length;
  }
  if (request.tools) chars += JSON.stringify(request.tools).length;
  return Math.ceil(chars / 4) + images * 850 + request.messages.length * 4;
}

function normalizeUsage(usage) {
  if (!usage) return null;
  const input = usage.prompt_tokens ?? usage.input_tokens ?? 0;
  const output = usage.completion_tokens ?? usage.output_tokens ?? 0;
  return {
    prompt_tokens: input,
    completion_tokens: output,
    total_tokens: usage.total_tokens ?? input + output,
    ...(usage.prompt_tokens_details ? { prompt_tokens_details: usage.prompt_tokens_details } : {}),
    ...(usage.completion_tokens_details ? { completion_tokens_details: usage.completion_tokens_details } : {}),
  };
}

// The charge a provider reports in its usage block (OpenRouter, Kilo, …), in
// USD; null when the provider does not report one.
function reportedCost(usage) {
  const value = Number(usage?.cost ?? usage?.total_cost);
  return Number.isFinite(value) ? value : null;
}

function buildResponse({ id = completionId(), model, message, finishReason = 'stop', usage = null, created = Math.floor(Date.now() / 1000) }) {
  return {
    id,
    object: 'chat.completion',
    created,
    model,
    choices: [{ index: 0, message, finish_reason: finishReason }],
    ...(usage ? { usage } : {}),
  };
}

function buildChunk({ id, model, created, delta = {}, finishReason = null, usage }) {
  const chunk = {
    id,
    object: 'chat.completion.chunk',
    created,
    model,
    choices: [{ index: 0, delta, finish_reason: finishReason }],
  };
  if (usage) chunk.usage = usage;
  return chunk;
}

// Folds a stream of canonical chunks into a full response. Used to validate
// streams (empty / truncated tool JSON) and to record usage.
class StreamAccumulator {
  constructor() {
    this.content = '';
    this.reasoning = '';
    this.toolCalls = [];
    this.finishReason = null;
    this.usage = null;
    this.reportedCost = null;
    this.chunks = 0;
    this.id = null;
    this.model = null;
  }

  push(chunk) {
    this.chunks += 1;
    this.id = this.id || chunk.id;
    this.model = this.model || chunk.model;
    if (chunk.usage) {
      this.usage = normalizeUsage(chunk.usage);
      this.reportedCost = reportedCost(chunk.usage);
    }
    for (const choice of chunk.choices || []) {
      const delta = choice.delta || {};
      if (typeof delta.content === 'string') this.content += delta.content;
      if (typeof delta.reasoning_content === 'string') this.reasoning += delta.reasoning_content;
      if (typeof delta.reasoning === 'string') this.reasoning += delta.reasoning;
      for (const call of delta.tool_calls || []) {
        const index = call.index ?? this.toolCalls.length;
        const target = this.toolCalls[index] || (this.toolCalls[index] = { id: '', type: 'function', function: { name: '', arguments: '' } });
        if (call.id) target.id = call.id;
        if (call.function?.name) target.function.name += call.function.name;
        if (call.function?.arguments) target.function.arguments += call.function.arguments;
      }
      if (choice.finish_reason) this.finishReason = choice.finish_reason;
    }
  }

  hasAnswer() {
    return this.content.trim().length > 0 || this.toolCalls.length > 0;
  }

  toMessage() {
    const message = { role: 'assistant', content: this.content || (this.toolCalls.length ? null : '') };
    if (this.toolCalls.length) message.tool_calls = this.toolCalls.filter(Boolean);
    if (this.reasoning) message.reasoning_content = this.reasoning;
    return message;
  }
}

function invalidToolCalls(toolCalls) {
  return (toolCalls || []).some((call) => {
    if (!call?.function?.name) return true;
    const args = call.function.arguments;
    if (!args) return false;
    try { JSON.parse(args); return false; } catch { return true; }
  });
}

module.exports = {
  completionId,
  reportedCost,
  normalizeRequest,
  contentParts,
  textOf,
  requirements,
  estimateInputTokens,
  normalizeUsage,
  buildResponse,
  buildChunk,
  StreamAccumulator,
  invalidToolCalls,
};
