'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { getDatabase } = require('../../server/db/database');

let app;
let llm;
let engine;
let broken;
let session;
let secret;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const gateway = (method, path) => request(app)[method](`/v1${path}`).set('Authorization', `Bearer ${secret}`);

test.before(async () => {
  app = createApp();
  llm = await createMockProvider({ models: ['searcher-one'] });
  engine = await createMockProvider({ models: [] });
  broken = await createMockProvider({ models: [], failSearch: true });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'searcher', email: 's@example.test', password: 'correct-horse-battery' })).body.session.token;
  assert.equal((await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', slug: 'llm', baseUrl: llm.url, secret: 'good-key', billingMode: 'free_tier' })).status, 201);
  secret = (await api('post', '/keys').send({ name: 'search', mode: 'free_only' })).body.secret;
});

test.after(async () => {
  await llm.close();
  await engine.close();
  await broken.close();
});

test('without a search engine, built-in web search is ignored like before', async () => {
  const response = await gateway('post', '/chat/completions').send({ model: 'auto', web_search_options: {}, messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.headers['x-neorouter-web-searches'], undefined);
  assert.equal((await gateway('post', '/search').send({ query: 'x' })).status, 503);
});

test('search engines are providers: listed, routed and falling back', async () => {
  const add = (slug, url) => api('post', '/providers/accounts').send({ definitionId: 'searxng', slug, baseUrl: url, billingMode: 'free_tier' });
  const first = await add('broken', broken.url);
  assert.equal(first.status, 201, JSON.stringify(first.body));
  assert.equal((await add('searx', engine.url)).status, 201);

  const kinds = (await api('get', '/models/capabilities')).body.capabilities;
  assert.ok(kinds.some((kind) => kind.id === 'search' && kind.routable && kind.free === 2));

  // Whichever engine routing tries first, the broken one never answers.
  for (let index = 0; index < 3; index += 1) {
    const response = await gateway('post', '/search').send({ query: 'neorouter', max_results: 1 });
    assert.equal(response.status, 200, JSON.stringify(response.body));
    assert.equal(response.body.model, 'searx/searxng');
    assert.equal(response.body.results.length, 1);
    assert.equal(response.body.results[0].url, 'https://example.com/a');
  }
  const filtered = await gateway('post', '/search').send({ query: 'neorouter', exclude_domains: ['example.com'] });
  assert.deepEqual(filtered.body.results.map((item) => item.url), ['https://www.other.org/b']);
  const sent = engine.calls.filter((call) => call.kind === 'search').at(-1);
  assert.match(sent.q, /-site:example\.com/);

  const row = getDatabase().prepare("SELECT units, is_free FROM requests WHERE capability = 'search' AND status = 'success' LIMIT 1").get();
  assert.deepEqual(row, { units: 1, is_free: 1 });
});

test('chat with web_search_options searches and answers from the results', async () => {
  const response = await gateway('post', '/chat/completions').send({ model: 'auto', web_search_options: {}, messages: [{ role: 'user', content: 'What is new in NeoRouter?' }] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.headers['x-neorouter-web-searches'], '1');
  assert.equal(response.body.choices[0].message.content, 'Answer from: [1] Result for neorouter release');
  assert.ok(!response.body.choices[0].message.tool_calls, 'the internal search call never reaches the client');
});

test('Anthropic web_search tools and OpenRouter :online work the same way, streams included', async () => {
  const anthropic = await gateway('post', '/messages').set('anthropic-version', '2023-06-01').send({
    model: 'auto', max_tokens: 200, tools: [{ type: 'web_search_20250305', name: 'web_search', max_uses: 2, allowed_domains: ['example.com'] }],
    messages: [{ role: 'user', content: 'Search please' }],
  });
  assert.equal(anthropic.status, 200, JSON.stringify(anthropic.body));
  assert.equal(anthropic.headers['x-neorouter-web-searches'], '1');
  assert.match(anthropic.body.content[0].text, /^Answer from: \[1\]/);

  const streamed = await gateway('post', '/chat/completions').send({ model: 'llm/searcher-one:online', stream: true, messages: [{ role: 'user', content: 'news?' }] });
  assert.equal(streamed.status, 200);
  const text = [...streamed.text.matchAll(/"content":"([^"]*)"/g)].map((match) => match[1]).join('');
  assert.match(text, /^Answer from: \[1\] Result for neorouter release/);
  assert.equal(streamed.headers['x-neorouter-web-searches'], '1');
  assert.match(streamed.text, /\[DONE\]/);
});
