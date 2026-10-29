'use strict';

const express = require('express');
const http = require('node:http');

// A fake OpenAI auth server and Codex backend on one port; the env must be
// set before the app (and its provider definitions) load.
const seen = { responses: [], refreshes: 0 };
const fake = express();
fake.use(express.json());
fake.use(express.urlencoded({ extended: false }));
const jwt = (claims) => `x.${Buffer.from(JSON.stringify(claims)).toString('base64url')}.y`;
const tokens = (suffix, expiresIn = 3600) => ({
  access_token: `access-${suffix}`, refresh_token: `refresh-${suffix}`, expires_in: expiresIn,
  id_token: jwt({ 'https://api.openai.com/auth': { chatgpt_account_id: 'acct-123', chatgpt_plan_type: 'plus' } }),
});
let polls = 0;
let issueShortLived = true;
fake.post('/api/accounts/deviceauth/usercode', (req, res) => res.json({ device_auth_id: 'dev-1', user_code: 'ABCD-1234', interval: 1 }));
fake.post('/api/accounts/deviceauth/token', (req, res) => {
  polls += 1;
  if (polls < 2) return res.status(403).json({ error: 'authorization_pending' });
  return res.json({ authorization_code: 'code-1', code_verifier: 'verifier-1', code_challenge: 'challenge-1' });
});
fake.post('/oauth/token', (req, res) => {
  if (req.body.grant_type === 'authorization_code') {
    if (req.body.redirect_uri !== `${process.env.NEOROUTER_CODEX_ISSUER}/deviceauth/callback` || req.body.code_verifier !== 'verifier-1') return res.status(400).json({ error: 'bad_exchange' });
    // The first tokens expire at once, so the first request has to refresh them.
    return res.json(tokens('one', issueShortLived ? 1 : 3600));
  }
  seen.refreshes += 1;
  return res.json(tokens('two'));
});
fake.get('/backend-api/codex/models', (req, res) => res.json({ models: [{ slug: 'gpt-test', display_name: 'GPT Test', visibility: 'list' }, { slug: 'gpt-hidden', visibility: 'hide' }] }));
fake.post('/backend-api/codex/responses', (req, res) => {
  seen.responses.push({ headers: req.headers, body: req.body });
  res.set('content-type', 'text/event-stream');
  res.set('x-codex-primary-used-percent', '25');
  res.set('x-codex-primary-window-minutes', '300');
  res.set('x-codex-secondary-used-percent', '60');
  res.set('x-codex-secondary-window-minutes', '10080');
  const send = (event) => res.write(`data: ${JSON.stringify(event)}\n\n`);
  const wantsTool = (req.body.tools || []).length && !req.body.input.some((item) => item.type === 'function_call_output');
  if (wantsTool) {
    send({ type: 'response.output_item.added', item: { id: 'fc_1', type: 'function_call', call_id: 'call_1', name: req.body.tools[0].name } });
    send({ type: 'response.function_call_arguments.delta', item_id: 'fc_1', delta: '{"city":' });
    send({ type: 'response.function_call_arguments.delta', item_id: 'fc_1', delta: '"Berlin"}' });
  } else {
    send({ type: 'response.output_text.delta', delta: 'Hello ' });
    send({ type: 'response.output_text.delta', delta: 'from ChatGPT' });
  }
  send({ type: 'response.completed', response: { usage: { input_tokens: 11, output_tokens: 4 } } });
  res.end();
});
const server = http.createServer(fake);

const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');

let app;
let session;
let secret;
let accountId;

test.before(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  process.env.NEOROUTER_CODEX_ISSUER = base;
  process.env.NEOROUTER_CODEX_BASE_URL = `${base}/backend-api/codex`;
  require('../support/env');
  const { createApp } = require('../../server/app');
  app = createApp();
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'gpt', email: 'gpt@example.test', password: 'correct-horse-battery' })).body.session.token;
  secret = (await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'k', mode: 'free_only' })).body.secret;
});

test.after(() => new Promise((resolve) => server.close(resolve)));

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);

test('ChatGPT connects with a device code and lists its models', async () => {
  const start = await api('post', '/providers/chatgpt/login');
  assert.equal(start.status, 200, JSON.stringify(start.body));
  assert.equal(start.body.userCode, 'ABCD-1234');
  assert.match(start.body.verificationUrl, /\/codex\/device$/);
  const pending = await api('post', `/providers/chatgpt/login/${start.body.loginId}`);
  assert.equal(pending.body.status, 'pending');
  const done = await api('post', `/providers/chatgpt/login/${start.body.loginId}`);
  assert.equal(done.status, 200, JSON.stringify(done.body));
  assert.equal(done.body.status, 'done');
  assert.equal(done.body.plan, 'plus');
  assert.equal(done.body.account.billingMode, 'plan');
  accountId = done.body.account.id;
  const models = (await api('get', '/models')).body.models.filter((model) => model.accountId === accountId);
  assert.deepEqual(models.map((model) => model.modelId), ['gpt-test'], 'hidden models are left out');
  assert.equal(models[0].isPlan, true);
});

test('a chat goes out in the Responses format and comes back as a chat answer', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`)
    .send({ model: 'auto', messages: [{ role: 'system', content: 'Be brief.' }, { role: 'user', content: 'Hi' }] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.choices[0].message.content, 'Hello from ChatGPT');
  assert.equal(response.body.usage.prompt_tokens, 11);
  const sent = seen.responses.at(-1);
  assert.equal(sent.headers['chatgpt-account-id'], 'acct-123');
  assert.equal(sent.headers.authorization, 'Bearer access-two', 'the expired token was refreshed first');
  assert.equal(seen.refreshes, 1);
  assert.equal(sent.body.store, false);
  assert.equal(sent.body.stream, true);
  assert.deepEqual(sent.body.input.map((item) => item.role), ['developer', 'user']);
});

test('tool calls translate both ways', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`).send({
    model: 'auto',
    messages: [{ role: 'user', content: 'Weather?' }],
    tools: [{ type: 'function', function: { name: 'weather', parameters: { type: 'object', properties: { city: { type: 'string' } } } } }],
  });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  const call = response.body.choices[0].message.tool_calls[0];
  assert.equal(call.function.name, 'weather');
  assert.equal(call.function.arguments, '{"city":"Berlin"}');
  assert.equal(response.body.choices[0].finish_reason, 'tool_calls');
});

test('the plan windows ChatGPT reports show up as plan usage', async () => {
  const quotas = (await api('get', '/usage/quotas')).body.accounts.find((group) => group.account.id === accountId);
  const windows = Object.fromEntries(quotas.probes.map((item) => [item.metric, item.remaining]));
  assert.equal(windows.plan_5h, 75);
  assert.equal(windows.plan_week, 40);
});
