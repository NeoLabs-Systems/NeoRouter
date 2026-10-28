'use strict';

const crypto = require('node:crypto');
const { HttpError } = require('../../../middleware/error_handler');
const { normalizeUsage, textOf } = require('../canonical');

const hex = (bytes = 12) => crypto.randomBytes(bytes).toString('hex');

function contentToParts(content) {
  if (typeof content === 'string') return content;
  const parts = (content || []).map((part) => {
    if (part.type === 'input_text' || part.type === 'output_text' || part.type === 'text') return { type: 'text', text: part.text || '' };
    if (part.type === 'input_image') {
      const url = typeof part.image_url === 'string' ? part.image_url : part.image_url?.url;
      return url ? { type: 'image_url', image_url: { url, ...(part.detail ? { detail: part.detail } : {}) } } : null;
    }
    return null;
  }).filter(Boolean);
  return parts.every((part) => part.type === 'text') ? parts.map((part) => part.text).join('') : parts;
}

// ---- client request (Responses) -> canonical -------------------------------
function toCanonical(body) {
  if (body.previous_response_id) {
    throw new HttpError(400, 'UNSUPPORTED_PARAMETER', 'previous_response_id is not supported. Send the full conversation in `input`.');
  }
  const messages = [];
  if (body.instructions) messages.push({ role: 'system', content: String(body.instructions) });
  const items = typeof body.input === 'string' ? [{ role: 'user', content: body.input }] : (body.input || []);
  for (const item of items) {
    const type = item.type || 'message';
    if (type === 'message') {
      const role = item.role === 'developer' ? 'system' : item.role;
      messages.push({ role, content: contentToParts(item.content) });
    } else if (type === 'function_call') {
      const call = { id: item.call_id || item.id, type: 'function', function: { name: item.name, arguments: item.arguments || '{}' } };
      const last = messages[messages.length - 1];
      if (last?.role === 'assistant' && last.tool_calls && !last.content) last.tool_calls.push(call);
      else messages.push({ role: 'assistant', content: null, tool_calls: [call] });
    } else if (type === 'function_call_output') {
      const output = typeof item.output === 'string' ? item.output : JSON.stringify(item.output);
      messages.push({ role: 'tool', tool_call_id: item.call_id, content: output });
    }
  }
  const request = { messages, stream: body.stream === true };
  if (Number.isFinite(body.max_output_tokens)) request.max_tokens = body.max_output_tokens;
  if (body.temperature !== undefined) request.temperature = body.temperature;
  if (body.top_p !== undefined) request.top_p = body.top_p;
  if (body.user) request.user = body.user;
  if (body.parallel_tool_calls !== undefined) request.parallel_tool_calls = body.parallel_tool_calls;
  if (body.reasoning?.effort) request.reasoning_effort = body.reasoning.effort;
  const tools = (body.tools || []).filter((tool) => tool.type === 'function');
  if (tools.length) {
    request.tools = tools.map((tool) => ({
      type: 'function',
      function: { name: tool.name, description: tool.description || '', parameters: tool.parameters || { type: 'object', properties: {} }, ...(tool.strict !== undefined ? { strict: tool.strict } : {}) },
    }));
  }
  if (typeof body.tool_choice === 'string') request.tool_choice = body.tool_choice;
  else if (body.tool_choice?.type === 'function') request.tool_choice = { type: 'function', function: { name: body.tool_choice.name } };
  const format = body.text?.format;
  if (format?.type === 'json_schema') {
    request.response_format = { type: 'json_schema', json_schema: { name: format.name || 'response', schema: format.schema, ...(format.strict !== undefined ? { strict: format.strict } : {}) } };
  } else if (format?.type === 'json_object') {
    request.response_format = { type: 'json_object' };
  }
  return request;
}

function usageOf(usage) {
  const normalized = normalizeUsage(usage) || { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 };
  return {
    input_tokens: normalized.prompt_tokens,
    output_tokens: normalized.completion_tokens,
    total_tokens: normalized.total_tokens,
    input_tokens_details: { cached_tokens: normalized.prompt_tokens_details?.cached_tokens || 0 },
    output_tokens_details: { reasoning_tokens: normalized.completion_tokens_details?.reasoning_tokens || 0 },
  };
}

function baseResponse(id, model, createdAt, status) {
  return {
    id,
    object: 'response',
    created_at: createdAt,
    status,
    model,
    output: [],
    parallel_tool_calls: true,
    tool_choice: 'auto',
    tools: [],
    error: null,
    incomplete_details: null,
  };
}

function finalize(response, finishReason, usage) {
  if (finishReason === 'length') {
    response.status = 'incomplete';
    response.incomplete_details = { reason: 'max_output_tokens' };
  } else {
    response.status = 'completed';
  }
  response.usage = usageOf(usage);
  response.output_text = response.output
    .filter((item) => item.type === 'message')
    .flatMap((item) => item.content)
    .map((part) => part.text)
    .join('');
  return response;
}

// ---- canonical response -> client (Responses) ------------------------------
function responseFromCanonical(completion, model) {
  const choice = completion.choices?.[0] || {};
  const message = choice.message || {};
  const response = baseResponse(`resp_${hex()}`, model, Math.floor(Date.now() / 1000), 'completed');
  const reasoning = message.reasoning_content || message.reasoning;
  if (reasoning) response.output.push({ type: 'reasoning', id: `rs_${hex()}`, summary: [{ type: 'summary_text', text: reasoning }] });
  const text = textOf(message.content);
  if (text) {
    response.output.push({
      type: 'message', id: `msg_${hex()}`, status: 'completed', role: 'assistant',
      content: [{ type: 'output_text', text, annotations: [] }],
    });
  }
  for (const call of message.tool_calls || []) {
    response.output.push({
      type: 'function_call', id: `fc_${hex()}`, call_id: call.id, name: call.function?.name, arguments: call.function?.arguments || '{}', status: 'completed',
    });
  }
  return finalize(response, choice.finish_reason, completion.usage);
}

// ---- canonical stream -> client (Responses SSE events) ---------------------
async function* streamFromCanonical(chunks, model) {
  const response = baseResponse(`resp_${hex()}`, model, Math.floor(Date.now() / 1000), 'in_progress');
  let sequence = 0;
  const emit = (type, payload) => ({ event: type, data: { type, sequence_number: sequence++, ...payload } });
  let textItem = null;
  const toolItems = new Map();
  let finish = null;
  let usage = null;

  yield emit('response.created', { response: { ...response, output: [] } });
  yield emit('response.in_progress', { response: { ...response, output: [] } });

  function* closeText() {
    if (!textItem) return;
    const { item, outputIndex } = textItem;
    const part = item.content[0];
    yield emit('response.output_text.done', { item_id: item.id, output_index: outputIndex, content_index: 0, text: part.text });
    yield emit('response.content_part.done', { item_id: item.id, output_index: outputIndex, content_index: 0, part });
    item.status = 'completed';
    yield emit('response.output_item.done', { output_index: outputIndex, item });
    textItem = null;
  }

  for await (const chunk of chunks) {
    if (chunk.usage) usage = chunk.usage;
    for (const choice of chunk.choices || []) {
      const delta = choice.delta || {};
      if (typeof delta.content === 'string' && delta.content) {
        if (!textItem) {
          const item = { type: 'message', id: `msg_${hex()}`, status: 'in_progress', role: 'assistant', content: [] };
          response.output.push(item);
          textItem = { item, outputIndex: response.output.length - 1 };
          yield emit('response.output_item.added', { output_index: textItem.outputIndex, item: { ...item, content: [] } });
          item.content.push({ type: 'output_text', text: '', annotations: [] });
          yield emit('response.content_part.added', { item_id: item.id, output_index: textItem.outputIndex, content_index: 0, part: { type: 'output_text', text: '', annotations: [] } });
        }
        textItem.item.content[0].text += delta.content;
        yield emit('response.output_text.delta', { item_id: textItem.item.id, output_index: textItem.outputIndex, content_index: 0, delta: delta.content });
      }
      for (const call of delta.tool_calls || []) {
        const key = call.index ?? 0;
        let entry = toolItems.get(key);
        if (!entry) {
          yield* closeText();
          const item = { type: 'function_call', id: `fc_${hex()}`, call_id: call.id || `call_${hex(8)}`, name: call.function?.name || '', arguments: '', status: 'in_progress' };
          response.output.push(item);
          entry = { item, outputIndex: response.output.length - 1 };
          toolItems.set(key, entry);
          yield emit('response.output_item.added', { output_index: entry.outputIndex, item: { ...item } });
        }
        if (call.function?.arguments) {
          entry.item.arguments += call.function.arguments;
          yield emit('response.function_call_arguments.delta', { item_id: entry.item.id, output_index: entry.outputIndex, delta: call.function.arguments });
        }
      }
      if (choice.finish_reason) finish = choice.finish_reason;
    }
  }
  yield* closeText();
  for (const { item, outputIndex } of toolItems.values()) {
    item.status = 'completed';
    yield emit('response.function_call_arguments.done', { item_id: item.id, output_index: outputIndex, arguments: item.arguments });
    yield emit('response.output_item.done', { output_index: outputIndex, item });
  }
  finalize(response, finish, usage);
  yield emit(response.status === 'incomplete' ? 'response.incomplete' : 'response.completed', { response });
}

module.exports = { toCanonical, responseFromCanonical, streamFromCanonical };
