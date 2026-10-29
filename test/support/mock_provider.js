'use strict';

const express = require('express');

// A fake OpenAI-compatible upstream. Behaviour is selected by model id:
//   ok-*      answers normally          fail-*   HTTP 500
//   rl-*      HTTP 429 with Retry-After  empty-*  200 with empty content
//   slow-*    answers after 300 ms       tools-*  returns a tool call
//   hang-*    answers after 5 s          think-*  reasoning only, no answer
//   trunc-*   spends max_tokens on reasoning (finish_reason length)
//   echo-*    answers with the last user message (streams it in small pieces)
//   searcher-* calls the web_search tool once, then answers from its result
//   *guard*   answers like Llama Guard ("unsafe\nS9" when the text mentions a bomb)
// Media endpoints (embeddings, images, speech, transcriptions, videos) fail
// for models starting with fail-, like chat.
function createMockProvider({ models, prices = {}, port = 0, failSearch = false, maxConcurrent = null } = {}) {
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
  // maxConcurrent: answer 429 like a provider with a concurrency cap.
  let active = 0;
  app.post('/v1/chat/completions', async (req, res, next) => {
    if (maxConcurrent && active >= maxConcurrent) {
      calls.push({ model: req.body.model, rejected: true, at: Date.now() });
      return res.status(429).json({ error: { message: 'Too many concurrent requests for this account.' } });
    }
    active += 1;
    res.on('close', () => { active -= 1; });
    return next();
  }, async (req, res) => {
    const { model, stream, tools } = req.body;
    calls.push({ model, stream: Boolean(stream), at: Date.now(), messages: req.body.messages });
    if (model.startsWith('fail-')) return res.status(500).json({ error: { message: 'upstream exploded' } });
    if (model.startsWith('rl-')) return res.status(429).set('retry-after', '30').json({ error: { message: 'Rate limit reached: limit: 30 requests per minute' } });
    if (model.startsWith('slow-')) await new Promise((resolve) => setTimeout(resolve, 300));
    if (model.startsWith('hang-')) await new Promise((resolve) => setTimeout(resolve, 5000));
    const toolCall = model.startsWith('tools-') || (tools && model.includes('tool'))
      ? [{ id: 'call_1', type: 'function', function: { name: tools?.[0]?.function?.name || 'lookup', arguments: '{"q":"x"}' } }]
      : null;
    const lastUser = [...(req.body.messages || [])].reverse().find((message) => message.role === 'user');
    const guard = model.includes('guard') ? (/bomb/i.test(String(lastUser?.content)) ? 'unsafe\nS9' : 'safe') : null;
    if (model.startsWith('searcher-') && (tools || []).some((tool) => tool.function?.name === 'web_search')) {
      const toolMessage = (req.body.messages || []).find((message) => message.role === 'tool');
      if (!toolMessage) {
        return res.json({ id: 'cmpl-s', object: 'chat.completion', created: 1, model, choices: [{ index: 0, message: { role: 'assistant', content: null, tool_calls: [{ id: 'call_s1', type: 'function', function: { name: 'web_search', arguments: JSON.stringify({ query: 'neorouter release' }) } }] }, finish_reason: 'tool_calls' }], usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 } });
      }
    }
    const searchAnswer = model.startsWith('searcher-') ? `Answer from: ${String((req.body.messages || []).find((message) => message.role === 'tool')?.content || 'nothing').split('\n')[0]}` : null;
    const plain = searchAnswer ?? (model.startsWith('echo-') ? String(lastUser?.content || '') : model.startsWith('empty-') || model.startsWith('think-') ? '' : `hello from ${model}`);
    const content = guard ?? plain;
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
      // echo-* streams in 3-character pieces so placeholders arrive split.
      const pieces = model.startsWith('echo-') ? content.match(/[\s\S]{1,3}/g) || [] : content.split(' ').map((word) => `${word} `);
      for (const piece of pieces) send({ ...base, choices: [{ index: 0, delta: { content: piece }, finish_reason: null }] });
      send({ ...base, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] });
    } else {
      send({ ...base, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] });
    }
    send({ ...base, choices: [], usage });
    res.write('data: [DONE]\n\n');
    return res.end();
  });
  const media = (name) => (req, res, next) => {
    calls.push({ model: req.body?.model, kind: name, at: Date.now(), body: req.body });
    if (String(req.body?.model || '').startsWith('fail-')) return res.status(500).json({ error: { message: 'upstream exploded' } });
    return next();
  };
  app.post('/v1/embeddings', media('embed'), (req, res) => {
    const inputs = Array.isArray(req.body.input) ? req.body.input : [req.body.input];
    const size = req.body.dimensions || 8;
    res.json({ object: 'list', model: req.body.model, data: inputs.map((text, index) => ({ object: 'embedding', index, embedding: Array.from({ length: size }, (_, i) => (text.length + i) / 100) })), usage: { prompt_tokens: 3 * inputs.length, total_tokens: 3 * inputs.length } });
  });
  const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=';
  app.post('/v1/images/generations', media('image'), (req, res) => {
    res.json({ created: 1, data: Array.from({ length: req.body.n || 1 }, () => ({ b64_json: PNG })) });
  });
  app.post('/v1/audio/speech', media('speech'), (req, res) => {
    res.set('content-type', 'audio/mpeg').send(Buffer.from(`ID3fake-mp3:${req.body.voice}:${req.body.input}`));
  });
  app.post('/v1/audio/transcriptions', express.raw({ type: 'multipart/form-data', limit: '30mb' }), async (req, res) => {
    const form = await new Request('http://x/', { method: 'POST', headers: { 'content-type': req.get('content-type') }, body: req.body }).formData();
    const model = form.get('model');
    const file = form.get('file');
    calls.push({ model, kind: 'transcribe', at: Date.now(), bytes: file?.size, responseFormat: form.get('response_format') });
    if (String(model).startsWith('fail-')) return res.status(500).json({ error: { message: 'upstream exploded' } });
    return res.json({ task: 'transcribe', language: 'english', duration: 2.5, text: 'hello world', segments: [{ id: 0, start: 0, end: 1.2, text: ' hello' }, { id: 1, start: 1.2, end: 2.5, text: ' world' }] });
  });
  // SearXNG-style search: GET /v1/search?q=…&format=json. failSearch makes it fail.
  app.get('/v1/search', (req, res) => {
    calls.push({ kind: 'search', q: req.query.q, at: Date.now() });
    if (failSearch) return res.status(500).json({ error: 'search exploded' });
    return res.json({ results: [
      { title: `Result for ${req.query.q}`, url: 'https://example.com/a', content: 'First result text.' },
      { title: 'Second', url: 'https://www.other.org/b', content: 'Second result text.' },
    ] });
  });
  const videos = new Map();
  app.post('/v1/videos', media('video'), (req, res) => {
    const id = `vid_${videos.size + 1}`;
    videos.set(id, { polls: 0 });
    res.json({ id, object: 'video', status: 'queued', progress: 0 });
  });
  app.get('/v1/videos/:id', (req, res) => {
    const video = videos.get(req.params.id);
    if (!video) return res.status(404).json({ error: { message: 'no such video' } });
    video.polls += 1;
    return res.json({ id: req.params.id, status: video.polls >= 2 ? 'completed' : 'in_progress', progress: video.polls >= 2 ? 100 : 50 });
  });
  app.get('/v1/videos/:id/content', (req, res) => res.set('content-type', 'video/mp4').send(Buffer.from('fake-mp4')));
  return new Promise((resolve) => {
    const server = app.listen(port, '127.0.0.1', () => {
      resolve({ url: `http://127.0.0.1:${server.address().port}/v1`, calls, close: () => new Promise((done) => server.close(done)) });
    });
  });
}

module.exports = { createMockProvider };
