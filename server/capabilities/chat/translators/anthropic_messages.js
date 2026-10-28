'use strict';

const crypto = require('node:crypto');
const { normalizeUsage, buildResponse, completionId, contentParts, textOf } = require('../canonical');

const STOP_TO_ANTHROPIC = { stop: 'end_turn', length: 'max_tokens', tool_calls: 'tool_use', function_call: 'tool_use', content_filter: 'refusal' };
const STOP_FROM_ANTHROPIC = { end_turn: 'stop', stop_sequence: 'stop', max_tokens: 'length', tool_use: 'tool_calls', pause_turn: 'stop', refusal: 'content_filter' };

function messageId() {
  return `msg_${crypto.randomBytes(12).toString('hex')}`;
}

function effortFromBudget(budget) {
  if (!Number.isFinite(budget)) return undefined;
  if (budget < 4096) return 'low';
  if (budget < 16384) return 'medium';
  return 'high';
}

function blocksOf(content) {
  if (typeof content === 'string') return [{ type: 'text', text: content }];
  return Array.isArray(content) ? content : [];
}

function imageBlockToPart(block) {
  const source = block.source || {};
  const url = source.type === 'base64' ? `data:${source.media_type};base64,${source.data}` : source.url;
  return { type: 'image_url', image_url: { url } };
}

function toolResultText(content) {
  if (typeof content === 'string') return content;
  return blocksOf(content).map((block) => (block.type === 'text' ? block.text : '')).join('');
}

// ---- client request (Anthropic) -> canonical -------------------------------
function toCanonical(body) {
  const messages = [];
  const system = typeof body.system === 'string' ? body.system : blocksOf(body.system).map((block) => block.text || '').join('\n');
  if (system) messages.push({ role: 'system', content: system });
  for (const message of body.messages || []) {
    const blocks = blocksOf(message.content);
    if (message.role === 'assistant') {
      const text = blocks.filter((block) => block.type === 'text').map((block) => block.text).join('');
      const toolCalls = blocks.filter((block) => block.type === 'tool_use').map((block) => ({
        id: block.id,
        type: 'function',
        function: { name: block.name, arguments: JSON.stringify(block.input ?? {}) },
      }));
      const out = { role: 'assistant', content: text || (toolCalls.length ? null : '') };
      if (toolCalls.length) out.tool_calls = toolCalls;
      messages.push(out);
      continue;
    }
    for (const block of blocks.filter((item) => item.type === 'tool_result')) {
      const text = toolResultText(block.content);
      messages.push({ role: 'tool', tool_call_id: block.tool_use_id, content: block.is_error ? `Error: ${text}` : text });
    }
    const parts = blocks.filter((block) => block.type !== 'tool_result').map((block) => {
      if (block.type === 'image') return imageBlockToPart(block);
      if (block.type === 'text') return { type: 'text', text: block.text };
      if (block.type === 'document' && block.source?.type === 'text') return { type: 'text', text: block.source.data };
      return null;
    }).filter(Boolean);
    if (parts.length) {
      const onlyText = parts.every((part) => part.type === 'text');
      messages.push({ role: 'user', content: onlyText ? parts.map((part) => part.text).join('') : parts });
    }
  }
  const request = { messages, stream: body.stream === true };
  if (Number.isFinite(body.max_tokens)) request.max_tokens = body.max_tokens;
  if (body.temperature !== undefined) request.temperature = body.temperature;
  if (body.top_p !== undefined) request.top_p = body.top_p;
  if (Array.isArray(body.stop_sequences) && body.stop_sequences.length) request.stop = body.stop_sequences;
  if (Array.isArray(body.tools) && body.tools.length) {
    const tools = body.tools.filter((tool) => tool.input_schema || !tool.type || tool.type === 'custom');
    if (tools.length) {
      request.tools = tools.map((tool) => ({
        type: 'function',
        function: { name: tool.name, description: tool.description || '', parameters: tool.input_schema || { type: 'object', properties: {} } },
      }));
    }
  }
  const choice = body.tool_choice;
  if (choice?.type === 'any') request.tool_choice = 'required';
  else if (choice?.type === 'tool') request.tool_choice = { type: 'function', function: { name: choice.name } };
  else if (choice?.type === 'none') request.tool_choice = 'none';
  else if (choice?.type === 'auto') request.tool_choice = 'auto';
  if (choice?.disable_parallel_tool_use) request.parallel_tool_calls = false;
  if (body.thinking?.type === 'enabled') request.reasoning_effort = effortFromBudget(body.thinking.budget_tokens);
  if (body.metadata?.user_id) request.user = String(body.metadata.user_id);
  return request;
}

// ---- canonical response -> client (Anthropic) ------------------------------
function responseFromCanonical(completion, model) {
  const choice = completion.choices?.[0] || {};
  const message = choice.message || {};
  const content = [];
  const reasoning = message.reasoning_content || message.reasoning;
  if (reasoning) content.push({ type: 'thinking', thinking: reasoning, signature: '' });
  const text = textOf(message.content);
  if (text) content.push({ type: 'text', text });
  for (const call of message.tool_calls || []) {
    let input = {};
    try { input = JSON.parse(call.function?.arguments || '{}'); } catch { input = {}; }
    content.push({ type: 'tool_use', id: call.id || `toolu_${crypto.randomBytes(8).toString('hex')}`, name: call.function?.name, input });
  }
  const usage = normalizeUsage(completion.usage) || { prompt_tokens: 0, completion_tokens: 0 };
  return {
    id: messageId(),
    type: 'message',
    role: 'assistant',
    model,
    content,
    stop_reason: STOP_TO_ANTHROPIC[choice.finish_reason] || 'end_turn',
    stop_sequence: null,
    usage: { input_tokens: usage.prompt_tokens, output_tokens: usage.completion_tokens },
  };
}

// ---- canonical stream -> client (Anthropic SSE events) ---------------------
async function* streamFromCanonical(chunks, model) {
  const id = messageId();
  let started = false;
  let index = -1;
  let blockType = null;
  const toolBlocks = new Map();
  let finish = null;
  let usage = null;

  const start = () => ({
    event: 'message_start',
    data: {
      type: 'message_start',
      message: { id, type: 'message', role: 'assistant', model, content: [], stop_reason: null, stop_sequence: null, usage: { input_tokens: 0, output_tokens: 0 } },
    },
  });
  function* closeBlock() {
    if (blockType) yield { event: 'content_block_stop', data: { type: 'content_block_stop', index } };
    blockType = null;
  }
  function* openBlock(type, block) {
    yield* closeBlock();
    index += 1;
    blockType = type;
    yield { event: 'content_block_start', data: { type: 'content_block_start', index, content_block: block } };
  }

  for await (const chunk of chunks) {
    if (!started) {
      started = true;
      yield start();
    }
    if (chunk.usage) usage = normalizeUsage(chunk.usage);
    for (const choice of chunk.choices || []) {
      const delta = choice.delta || {};
      const reasoning = delta.reasoning_content ?? delta.reasoning;
      if (typeof reasoning === 'string' && reasoning) {
        if (blockType !== 'thinking') yield* openBlock('thinking', { type: 'thinking', thinking: '' });
        yield { event: 'content_block_delta', data: { type: 'content_block_delta', index, delta: { type: 'thinking_delta', thinking: reasoning } } };
      }
      if (typeof delta.content === 'string' && delta.content) {
        if (blockType !== 'text') yield* openBlock('text', { type: 'text', text: '' });
        yield { event: 'content_block_delta', data: { type: 'content_block_delta', index, delta: { type: 'text_delta', text: delta.content } } };
      }
      for (const call of delta.tool_calls || []) {
        const key = call.index ?? 0;
        if (!toolBlocks.has(key)) {
          yield* openBlock(`tool:${key}`, {
            type: 'tool_use',
            id: call.id || `toolu_${crypto.randomBytes(8).toString('hex')}`,
            name: call.function?.name || '',
            input: {},
          });
          toolBlocks.set(key, index);
        }
        if (call.function?.arguments) {
          yield { event: 'content_block_delta', data: { type: 'content_block_delta', index: toolBlocks.get(key), delta: { type: 'input_json_delta', partial_json: call.function.arguments } } };
        }
      }
      if (choice.finish_reason) finish = choice.finish_reason;
    }
  }
  if (!started) yield start();
  yield* closeBlock();
  yield {
    event: 'message_delta',
    data: {
      type: 'message_delta',
      delta: { stop_reason: STOP_TO_ANTHROPIC[finish] || 'end_turn', stop_sequence: null },
      usage: { input_tokens: usage?.prompt_tokens || 0, output_tokens: usage?.completion_tokens || 0 },
    },
  };
  yield { event: 'message_stop', data: { type: 'message_stop' } };
}

// ---- canonical request -> upstream (Anthropic) -----------------------------
function partToBlock(part) {
  if (part.type === 'text') return { type: 'text', text: part.text || '' };
  if (part.type === 'image_url') {
    const url = typeof part.image_url === 'string' ? part.image_url : part.image_url?.url;
    const match = /^data:([^;]+);base64,(.*)$/s.exec(url || '');
    if (match) return { type: 'image', source: { type: 'base64', media_type: match[1], data: match[2] } };
    return { type: 'image', source: { type: 'url', url } };
  }
  return null;
}

function fromCanonical(request, { defaultMaxTokens = 4096 } = {}) {
  const system = [];
  const messages = [];
  const push = (role, blocks) => {
    const last = messages[messages.length - 1];
    if (last && last.role === role) last.content.push(...blocks);
    else messages.push({ role, content: blocks });
  };
  for (const message of request.messages) {
    if (message.role === 'system' || message.role === 'developer') {
      system.push(textOf(message.content));
    } else if (message.role === 'tool') {
      push('user', [{ type: 'tool_result', tool_use_id: message.tool_call_id, content: textOf(message.content) }]);
    } else if (message.role === 'assistant') {
      const blocks = [];
      const text = textOf(message.content);
      if (text) blocks.push({ type: 'text', text });
      for (const call of message.tool_calls || []) {
        let input = {};
        try { input = JSON.parse(call.function?.arguments || '{}'); } catch { input = {}; }
        blocks.push({ type: 'tool_use', id: call.id, name: call.function?.name, input });
      }
      if (blocks.length) push('assistant', blocks);
    } else {
      const blocks = contentParts(message.content).map(partToBlock).filter(Boolean);
      if (blocks.length) push('user', blocks);
    }
  }
  const body = { messages, max_tokens: request.max_tokens || defaultMaxTokens };
  if (system.length) body.system = system.join('\n\n');
  if (request.temperature !== undefined) body.temperature = Math.min(1, request.temperature);
  if (request.top_p !== undefined) body.top_p = request.top_p;
  if (request.stop) body.stop_sequences = Array.isArray(request.stop) ? request.stop : [request.stop];
  if (request.tools) {
    body.tools = request.tools.map((tool) => ({
      name: tool.function.name,
      description: tool.function.description || '',
      input_schema: tool.function.parameters || { type: 'object', properties: {} },
    }));
  }
  const choice = request.tool_choice;
  if (choice === 'required') body.tool_choice = { type: 'any' };
  else if (choice === 'none') body.tool_choice = { type: 'none' };
  else if (choice === 'auto') body.tool_choice = { type: 'auto' };
  else if (choice?.function?.name) body.tool_choice = { type: 'tool', name: choice.function.name };
  if (request.parallel_tool_calls === false && body.tools) body.tool_choice = { ...(body.tool_choice || { type: 'auto' }), disable_parallel_tool_use: true };
  if (request.reasoning_effort) {
    const budget = { minimal: 1024, low: 2048, medium: 8192, high: 24576 }[request.reasoning_effort];
    if (budget && body.max_tokens > budget) {
      body.thinking = { type: 'enabled', budget_tokens: budget };
      delete body.temperature;
      delete body.top_p;
    }
  }
  if (request.response_format?.type === 'json_schema' || request.response_format?.type === 'json_object') {
    const note = request.response_format.json_schema?.schema
      ? `Respond only with JSON matching this schema: ${JSON.stringify(request.response_format.json_schema.schema)}`
      : 'Respond only with valid JSON.';
    body.system = body.system ? `${body.system}\n\n${note}` : note;
  }
  return body;
}

// ---- upstream (Anthropic) response -> canonical ----------------------------
function responseToCanonical(message, model) {
  let text = '';
  let reasoning = '';
  const toolCalls = [];
  for (const block of message.content || []) {
    if (block.type === 'text') text += block.text;
    else if (block.type === 'thinking') reasoning += block.thinking || '';
    else if (block.type === 'tool_use') {
      toolCalls.push({ id: block.id, type: 'function', function: { name: block.name, arguments: JSON.stringify(block.input ?? {}) } });
    }
  }
  const out = { role: 'assistant', content: text || (toolCalls.length ? null : '') };
  if (toolCalls.length) out.tool_calls = toolCalls;
  if (reasoning) out.reasoning_content = reasoning;
  const usage = message.usage || {};
  const input = (usage.input_tokens || 0) + (usage.cache_read_input_tokens || 0) + (usage.cache_creation_input_tokens || 0);
  return buildResponse({
    id: message.id || completionId(),
    model: message.model || model,
    message: out,
    finishReason: STOP_FROM_ANTHROPIC[message.stop_reason] || 'stop',
    usage: { prompt_tokens: input, completion_tokens: usage.output_tokens || 0, total_tokens: input + (usage.output_tokens || 0) },
  });
}

// ---- upstream (Anthropic) stream events -> canonical chunks -----------------
async function* streamToCanonical(events, model, onError) {
  let id = completionId();
  const created = Math.floor(Date.now() / 1000);
  let input = 0;
  let output = 0;
  let finish = null;
  const toolIndex = new Map();
  const chunk = (delta, finishReason = null, usage) => ({
    id, object: 'chat.completion.chunk', created, model, choices: [{ index: 0, delta, finish_reason: finishReason }], ...(usage ? { usage } : {}),
  });
  for await (const { event, data } of events) {
    let payload;
    try { payload = JSON.parse(data); } catch { continue; }
    const type = payload.type || event;
    if (type === 'error') {
      onError(payload);
      return;
    }
    if (type === 'message_start') {
      id = payload.message?.id || id;
      const usage = payload.message?.usage || {};
      input = (usage.input_tokens || 0) + (usage.cache_read_input_tokens || 0) + (usage.cache_creation_input_tokens || 0);
      yield chunk({ role: 'assistant', content: '' });
    } else if (type === 'content_block_start' && payload.content_block?.type === 'tool_use') {
      const index = toolIndex.size;
      toolIndex.set(payload.index, index);
      yield chunk({ tool_calls: [{ index, id: payload.content_block.id, type: 'function', function: { name: payload.content_block.name, arguments: '' } }] });
    } else if (type === 'content_block_delta') {
      const delta = payload.delta || {};
      if (delta.type === 'text_delta') yield chunk({ content: delta.text });
      else if (delta.type === 'thinking_delta') yield chunk({ reasoning_content: delta.thinking });
      else if (delta.type === 'input_json_delta' && toolIndex.has(payload.index)) {
        yield chunk({ tool_calls: [{ index: toolIndex.get(payload.index), function: { arguments: delta.partial_json } }] });
      }
    } else if (type === 'message_delta') {
      finish = STOP_FROM_ANTHROPIC[payload.delta?.stop_reason] || finish;
      if (payload.usage?.output_tokens !== undefined) output = payload.usage.output_tokens;
      if (payload.usage?.input_tokens) input = payload.usage.input_tokens;
    } else if (type === 'message_stop') {
      break;
    }
  }
  yield chunk({}, finish || 'stop', { prompt_tokens: input, completion_tokens: output, total_tokens: input + output });
}

module.exports = {
  toCanonical,
  responseFromCanonical,
  streamFromCanonical,
  fromCanonical,
  responseToCanonical,
  streamToCanonical,
};
