'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const registry = require('../../server/providers/registry');
const { sessionId, OpenCodeGoAdapter } = require('../../server/providers/adapters/opencode_go');
const { normalizeModel } = require('../../server/providers/model_info');
const { opencodeGoWindows } = require('../../server/providers/definitions/plans');

test('OpenCode Go is a plan provider that talks OpenAI Chat', () => {
  const definition = registry.get('opencode_go');
  assert.equal(definition.adapter, 'opencode_go');
  assert.equal(definition.category, 'plan');
  assert.equal(definition.billing.defaultMode, 'plan');
  assert.ok(definition.billing.plan);
  // Go refuses requests without a session id and asks clients to name themselves.
  assert.ok(definition.quirks.userAgent);
  assert.equal(definition.quota.probe.path, 'https://opencode.ai/inference/go/v1/usage');
});

test('the session id names a conversation, not a request', () => {
  const system = { role: 'system', content: 'You are a coding agent.' };
  const first = { role: 'user', content: 'Add a test.' };
  const turn1 = { messages: [system, first] };
  const turn2 = { messages: [system, first, { role: 'assistant', content: 'ok' }, { role: 'user', content: 'Now run it.' }] };
  assert.equal(sessionId(turn1), sessionId(turn2), 'later turns keep the same id');
  assert.match(sessionId(turn1), /^ses_[0-9a-f]{32}$/);
  assert.notEqual(sessionId(turn1), sessionId({ messages: [system, { role: 'user', content: 'Something else.' }] }));
  assert.equal(sessionId({ messages: [] }), null);
});

test('chat and stream both carry the session header, with nothing to configure', async () => {
  const definition = registry.get('opencode_go');
  const adapter = new OpenCodeGoAdapter(definition, { secret: 'sk-test', options: {} });
  const canonical = { messages: [{ role: 'system', content: 's' }, { role: 'user', content: 'hi' }] };
  const expected = sessionId(canonical);
  const seen = [];
  adapter.request = async (url, options) => {
    seen.push({ url, headers: options.headers });
    if (options.raw) {
      const body = { id: 'x', model: 'glm-5.3', choices: [{ index: 0, delta: { content: 'hi' }, finish_reason: 'stop' }] };
      const stream = new ReadableStream({ start(controller) { controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(body)}\n\ndata: [DONE]\n\n`)); controller.close(); } });
      return new Response(stream, { headers: { 'content-type': 'text/event-stream' } });
    }
    return { data: { id: 'x', choices: [{ index: 0, message: { role: 'assistant', content: 'hi' }, finish_reason: 'stop' }] }, headers: {} };
  };

  await adapter.chat(canonical, { modelId: 'glm-5.3' });
  const chunks = [];
  for await (const chunk of adapter.stream(canonical, { modelId: 'glm-5.3' })) chunks.push(chunk);

  assert.equal(seen.length, 2);
  assert.equal(seen[0].url, 'https://opencode.ai/inference/go/openai/v1/chat/completions');
  for (const call of seen) {
    assert.equal(call.headers['x-opencode-session'], expected);
    assert.equal(call.headers.Authorization, 'Bearer sk-test');
    assert.equal(call.headers['User-Agent'], definition.quirks.userAgent);
  }
  // The per-request header does not leak into the adapter's static headers.
  assert.equal(adapter.headers()['x-opencode-session'], undefined);
  assert.equal(chunks.length, 1);
});

test('the usage answer becomes the three plan windows', () => {
  const resets = ['2026-11-07T14:12:03.000Z', '2026-11-12T00:00:00.000Z', '2026-12-01T00:00:00.000Z'];
  const windows = opencodeGoWindows({ usage: {
    rolling: { status: 'ok', percent: 37, resetsAt: resets[0] },
    weekly: { status: 'ok', percent: 62, resetsAt: resets[1] },
    monthly: { status: 'ok', percent: 12, resetsAt: resets[2] },
  } });
  assert.deepEqual(windows, [
    { metric: 'plan_5h', limit: 100, remaining: 63, resetAt: Date.parse(resets[0]) },
    { metric: 'plan_week', limit: 100, remaining: 38, resetAt: Date.parse(resets[1]) },
    { metric: 'plan_month', limit: 100, remaining: 88, resetAt: Date.parse(resets[2]) },
  ]);
  // An account without a Go subscription answers without windows.
  assert.deepEqual(opencodeGoWindows({ usage: {} }), []);
  // A used-up window is 0 % left, never unknown-and-therefore-allowed.
  assert.deepEqual(opencodeGoWindows({ usage: { rolling: { percent: 100, resetsAt: resets[0] } } }),
    [{ metric: 'plan_5h', limit: 100, remaining: 0, resetAt: Date.parse(resets[0]) }]);
});

test('Go\'s model listing prices, limits and capabilities are read', () => {
  const model = normalizeModel({
    id: 'glm-5.3',
    name: 'GLM-5.3',
    type: 'chat',
    limit: { context: 1000000, output: 131072 },
    cost: { text: { input: { price: 1.4, cache: { read: 0.26 } }, output: { price: 4.4 } } },
    capabilities: { tools: { supported: true }, reasoning: { supported: true } },
    modalities: { input: ['text'], output: ['text'] },
  });
  assert.equal(model.kind, 'chat');
  assert.equal(model.contextWindow, 1000000);
  assert.equal(model.maxOutput, 131072);
  assert.equal(model.priceIn, 1.4);
  assert.equal(model.priceOut, 4.4);
  assert.equal(model.priceKnown, true);
  assert.equal(model.freeHint, false);
  assert.equal(model.tools, true);
  assert.equal(model.reasoning, true);
  assert.equal(model.vision, false);

  const free = normalizeModel({ id: 'beam-free', name: 'Beam Free', cost: { text: { input: { price: 0 }, output: { price: 0 } } } });
  assert.equal(free.freeHint, true);
  assert.equal(free.priceIn, 0);
});
