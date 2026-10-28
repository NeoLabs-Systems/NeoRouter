'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const anthropic = require('../../server/capabilities/chat/translators/anthropic_messages');
const responses = require('../../server/capabilities/chat/translators/openai_responses');

async function collect(iterable) {
  const items = [];
  for await (const item of iterable) items.push(item);
  return items;
}

async function* chunks(list) {
  for (const item of list) yield item;
}

test('Anthropic request with tools and tool results maps to canonical messages', () => {
  const canonical = anthropic.toCanonical({
    system: [{ type: 'text', text: 'sys' }],
    max_tokens: 100,
    tools: [{ name: 'get', input_schema: { type: 'object' } }],
    tool_choice: { type: 'any' },
    messages: [
      { role: 'user', content: 'hi' },
      { role: 'assistant', content: [{ type: 'text', text: 'calling' }, { type: 'tool_use', id: 't1', name: 'get', input: { a: 1 } }] },
      { role: 'user', content: [{ type: 'tool_result', tool_use_id: 't1', content: 'result' }, { type: 'text', text: 'thanks' }] },
    ],
  });
  assert.equal(canonical.messages[0].role, 'system');
  assert.equal(canonical.messages[2].tool_calls[0].function.arguments, '{"a":1}');
  assert.deepEqual(canonical.messages[3], { role: 'tool', tool_call_id: 't1', content: 'result' });
  assert.equal(canonical.messages[4].content, 'thanks');
  assert.equal(canonical.tool_choice, 'required');
});

test('canonical request maps back to Anthropic upstream format', () => {
  const body = anthropic.fromCanonical({
    messages: [
      { role: 'system', content: 'sys' },
      { role: 'user', content: [{ type: 'text', text: 'look' }, { type: 'image_url', image_url: { url: 'data:image/png;base64,AAAA' } }] },
      { role: 'assistant', content: null, tool_calls: [{ id: 't1', type: 'function', function: { name: 'get', arguments: '{"a":1}' } }] },
      { role: 'tool', tool_call_id: 't1', content: 'ok' },
    ],
    tools: [{ type: 'function', function: { name: 'get', parameters: { type: 'object' } } }],
  });
  assert.equal(body.system, 'sys');
  assert.equal(body.messages[0].content[1].source.media_type, 'image/png');
  assert.equal(body.messages[1].content[0].type, 'tool_use');
  assert.equal(body.messages[2].content[0].type, 'tool_result');
  assert.equal(body.max_tokens, 4096);
});

test('canonical stream becomes a well-formed Anthropic event sequence', async () => {
  const events = await collect(anthropic.streamFromCanonical(chunks([
    { choices: [{ delta: { role: 'assistant', content: 'Hel' } }] },
    { choices: [{ delta: { content: 'lo' } }] },
    { choices: [{ delta: { tool_calls: [{ index: 0, id: 'c1', function: { name: 'f', arguments: '{"x"' } }] } }] },
    { choices: [{ delta: { tool_calls: [{ index: 0, function: { arguments: ':1}' } }] } }] },
    { choices: [{ delta: {}, finish_reason: 'tool_calls' }] },
    { choices: [], usage: { prompt_tokens: 5, completion_tokens: 3 } },
  ]), 'm'));
  const types = events.map((event) => event.event);
  assert.deepEqual(types, ['message_start', 'content_block_start', 'content_block_delta', 'content_block_delta', 'content_block_stop',
    'content_block_start', 'content_block_delta', 'content_block_delta', 'content_block_stop', 'message_delta', 'message_stop']);
  assert.equal(events[9].data.delta.stop_reason, 'tool_use');
  assert.equal(events[9].data.usage.output_tokens, 3);
});

test('Anthropic upstream stream maps to canonical chunks with usage', async () => {
  const sse = [
    { event: 'message_start', data: JSON.stringify({ type: 'message_start', message: { id: 'msg_1', usage: { input_tokens: 7 } } }) },
    { event: 'content_block_start', data: JSON.stringify({ type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }) },
    { event: 'content_block_delta', data: JSON.stringify({ type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: 'Hi' } }) },
    { event: 'content_block_start', data: JSON.stringify({ type: 'content_block_start', index: 1, content_block: { type: 'tool_use', id: 'tu', name: 'f' } }) },
    { event: 'content_block_delta', data: JSON.stringify({ type: 'content_block_delta', index: 1, delta: { type: 'input_json_delta', partial_json: '{}' } }) },
    { event: 'message_delta', data: JSON.stringify({ type: 'message_delta', delta: { stop_reason: 'tool_use' }, usage: { output_tokens: 4 } }) },
    { event: 'message_stop', data: JSON.stringify({ type: 'message_stop' }) },
  ];
  const out = await collect(anthropic.streamToCanonical(chunks(sse), 'claude', () => assert.fail('no error expected')));
  assert.equal(out[1].choices[0].delta.content, 'Hi');
  assert.equal(out[2].choices[0].delta.tool_calls[0].id, 'tu');
  const last = out[out.length - 1];
  assert.equal(last.choices[0].finish_reason, 'tool_calls');
  assert.deepEqual(last.usage, { prompt_tokens: 7, completion_tokens: 4, total_tokens: 11 });
});

test('Responses input items with function calls map to canonical messages', () => {
  const canonical = responses.toCanonical({
    instructions: 'sys',
    input: [
      { role: 'user', content: [{ type: 'input_text', text: 'hi' }] },
      { type: 'function_call', call_id: 'c1', name: 'f', arguments: '{}' },
      { type: 'function_call_output', call_id: 'c1', output: 'done' },
    ],
    tools: [{ type: 'function', name: 'f', parameters: { type: 'object' } }, { type: 'web_search' }],
    max_output_tokens: 20,
    text: { format: { type: 'json_schema', name: 'x', schema: { type: 'object' } } },
  });
  assert.equal(canonical.messages.length, 4);
  assert.equal(canonical.messages[2].tool_calls[0].id, 'c1');
  assert.equal(canonical.messages[3].role, 'tool');
  assert.equal(canonical.tools.length, 1);
  assert.equal(canonical.max_tokens, 20);
  assert.equal(canonical.response_format.type, 'json_schema');
});

test('previous_response_id is rejected clearly', () => {
  assert.throws(() => responses.toCanonical({ input: 'hi', previous_response_id: 'resp_1' }), /previous_response_id/);
});

test('Responses stream emits function call events and completes', async () => {
  const events = await collect(responses.streamFromCanonical(chunks([
    { choices: [{ delta: { content: 'a' } }] },
    { choices: [{ delta: { tool_calls: [{ index: 0, id: 'c1', function: { name: 'f', arguments: '{}' } }] } }] },
    { choices: [{ delta: {}, finish_reason: 'tool_calls' }], usage: { prompt_tokens: 1, completion_tokens: 2 } },
  ]), 'm'));
  const types = events.map((event) => event.event);
  assert.ok(types.includes('response.function_call_arguments.done'));
  const done = events[events.length - 1];
  assert.equal(done.event, 'response.completed');
  assert.equal(done.data.response.output.length, 2);
  assert.equal(done.data.response.usage.output_tokens, 2);
  assert.deepEqual(events.map((event) => event.data.sequence_number), events.map((_, index) => index));
});
