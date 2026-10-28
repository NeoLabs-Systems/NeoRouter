'use strict';

const express = require('express');

// A fake OpenAI-compatible upstream. Behaviour is selected by model id:
//   ok-*      answers normally          fail-*   HTTP 500
//   rl-*      HTTP 429 with Retry-After  empty-*  200 with empty content
//   slow-*    answers after 300 ms       tools-*  returns a tool call
//   hang-*    answers after 5 s          think-*  reasoning only, no answer
//   trunc-*   spends max_tokens on reasoning (finish_reason length)
function createMockProvider({ models, prices = {}, port = 0 } = {}) {
  const app = express();
  app.use(express.json());
  const calls = [];
  app.get('/v1/models', (req, res) => {
    if (req.get('authorization') !== 'Bearer good-key') return res.status(401).json({ error: { message: 'Invalid API key' } });
    return res.json({
      object: 'list',
      data: models.map((id) => ({ id, object: 'model', context_length: 32768, ...(prices[id] ? { pricing: prices[id] } : {}) })),
    });
  });
  app.post('/v1/chat/completions', async (req, res) => {
    const { model, stream, tools } = req.body;
    calls.push({ model, stream: Boolean(stream), at: Date.now() });
    if (model.startsWith('fail-')) return res.status(500).json({ error: { message: 'upstream exploded' } });
    if (model.startsWith('rl-')) return res.status(429).set('retry-after', '30').json({ error: { message: 'Rate limit reached: limit: 30 requests per minute' } });
    if (model.startsWith('slow-')) await new Promise((resolve) => setTimeout(resolve, 300));
    if (model.startsWith('hang-')) await new Promise((resolve) => setTimeout(resolve, 5000));
    const toolCall = model.startsWith('tools-') || (tools && model.includes('tool'))
      ? [{ id: 'call_1', type: 'function', function: { name: tools?.[0]?.function?.name || 'lookup', arguments: '{"q":"x"}' } }]
      : null;
    const content = model.startsWith('empty-') || model.startsWith('think-') ? '' : `hello from ${model}`;
    if (model.startsWith('trunc-') && !stream) {
      return res.json({ id: 'cmpl-1', object: 'chat.completion', created: 1, model, choices: [{ index: 0, message: { role: 'assistant', content: '', reasoning: 'thinking and thinking' }, finish_reason: 'length' }], usage: { prompt_tokens: 5, completion_tokens: 50, total_tokens: 55 } });
    }
    if (model.startsWith('think-') && !stream) {
      return res.json({ id: 'cmpl-1', object: 'chat.completion', created: 1, model, choices: [{ index: 0, message: { role: 'assistant', content: '', reasoning: 'thinking about 7' }, finish_reason: 'stop' }], usage: { prompt_tokens: 5, completion_tokens: 5, total_tokens: 10 } });
    }
    res.set('x-ratelimit-limit-requests', '1000');
    res.set('x-ratelimit-remaining-requests', '990');
    res.set('x-ratelimit-reset-requests', '30s');
    // "cost-" models report a charge like OpenRouter's usage.cost.
    const usage = { prompt_tokens: 12, completion_tokens: 6, total_tokens: 18, ...(model.startsWith('cost-') ? { cost: 0.0004 } : {}) };
    if (!stream) {
      return res.json({
        id: 'cmpl-1', object: 'chat.completion', created: 1, model,
        choices: [{ index: 0, message: { role: 'assistant', content: toolCall ? null : content, ...(toolCall ? { tool_calls: toolCall } : {}) }, finish_reason: toolCall ? 'tool_calls' : 'stop' }],
        usage,
      });
    }
    res.set('content-type', 'text/event-stream');
    const send = (payload) => res.write(`data: ${JSON.stringify(payload)}\n\n`);
    const base = { id: 'cmpl-1', object: 'chat.completion.chunk', created: 1, model };
    send({ ...base, choices: [{ index: 0, delta: { role: 'assistant', content: '' }, finish_reason: null }] });
    if (toolCall) {
      send({ ...base, choices: [{ index: 0, delta: { tool_calls: [{ index: 0, id: 'call_1', type: 'function', function: { name: toolCall[0].function.name, arguments: '' } }] }, finish_reason: null }] });
      send({ ...base, choices: [{ index: 0, delta: { tool_calls: [{ index: 0, function: { arguments: '{"q":"x"}' } }] }, finish_reason: null }] });
      send({ ...base, choices: [{ index: 0, delta: {}, finish_reason: 'tool_calls' }] });
    } else if (content) {
      for (const word of content.split(' ')) send({ ...base, choices: [{ index: 0, delta: { content: `${word} ` }, finish_reason: null }] });
      send({ ...base, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] });
    } else {
      send({ ...base, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] });
    }
    send({ ...base, choices: [], usage });
    res.write('data: [DONE]\n\n');
    return res.end();
  });
  return new Promise((resolve) => {
    const server = app.listen(port, '127.0.0.1', () => {
      resolve({ url: `http://127.0.0.1:${server.address().port}/v1`, calls, close: () => new Promise((done) => server.close(done)) });
    });
  });
}

module.exports = { createMockProvider };
