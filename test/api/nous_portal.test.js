'use strict';

const express = require('express');
const http = require('node:http');

// A fake Nous Portal (device flow, token, account) and inference API on one
// port; the env must be set before the app (and its provider definitions) load.
const seen = { chats: [], refreshes: 0, accountCalls: 0 };
const fake = express();
fake.use(express.json());
fake.use(express.urlencoded({ extended: false }));
const jwt = (claims) => `x.${Buffer.from(JSON.stringify(claims)).toString('base64url')}.y`;
const tokens = (suffix, expiresIn = 3600) => ({
  access_token: jwt({ sub: 'user-1', email: 'portal@example.test', account_tier: 'paid', exp: Math.floor(Date.now() / 1000) + expiresIn }),
  // Portal rotates the refresh token on every refresh.
  refresh_token: `refresh-${suffix}`,
  expires_in: expiresIn,
  scope: 'inference:invoke',
});
let polls = 0;
let issueShortLived = true;
let declineNext = false;
fake.post('/api/oauth/device/code', (req, res) => {
  if (req.body.client_id !== 'hermes-cli') return res.status(400).json({ error: 'invalid_client', error_description: `Unsupported OAuth client_id: ${req.body.client_id}` });
  return res.json({
    device_code: 'dc-1', user_code: 'ABCD-1234', expires_in: 600, interval: 1,
    verification_uri: 'https://portal.test/manage-subscription',
    verification_uri_complete: 'https://portal.test/manage-subscription?user_code=ABCD-1234',
  });
});
fake.post('/api/oauth/token', (req, res) => {
  if (req.body.grant_type === 'urn:ietf:params:oauth:grant-type:device_code') {
    if (declineNext) return res.status(400).json({ error: 'access_denied' });
    if (req.body.client_id !== 'hermes-cli' || req.body.device_code !== 'dc-1') return res.status(400).json({ error: 'invalid_grant', error_description: 'Invalid device code' });
    polls += 1;
    if (polls < 2) return res.status(400).json({ error: 'authorization_pending' });
    // The first tokens expire at once, so the first request has to refresh them.
    return res.json(tokens('one', issueShortLived ? 1 : 3600));
  }
  if (req.body.grant_type === 'refresh_token') {
    seen.refreshes += 1;
    // The refresh token goes in the header, and only the current one works.
    if (req.headers['x-nous-refresh-token'] !== 'refresh-one') return res.status(400).json({ error: 'invalid_grant', error_description: 'Invalid refresh token' });
    return res.json(tokens('two'));
  }
  return res.status(400).json({ error: 'unsupported_grant_type' });
});
fake.get('/api/oauth/account', (req, res) => {
  seen.accountCalls += 1;
  res.json({
    user: { email: 'portal@example.test' },
    organisation: { id: 'org-1', slug: 'org', name: 'Org' },
    paid_service_access: { allowed: true, total_usable_credits: 42.5 },
  });
});
fake.get('/v1/models', (req, res) => res.json({
  data: [
    { id: 'hermes-5', context_length: 131072, pricing: { prompt: '0.000001', completion: '0.000002' } },
    { id: 'tencent/hy4:free', context_length: 65536, pricing: { prompt: '0', completion: '0' } },
  ],
}));
fake.post('/v1/chat/completions', (req, res) => {
  seen.chats.push({ headers: req.headers, body: req.body });
  if (req.body.stream) {
    res.set('content-type', 'text/event-stream');
    res.write(`data: ${JSON.stringify({ id: 'c1', object: 'chat.completion.chunk', created: 1, model: req.body.model, choices: [{ index: 0, delta: { role: 'assistant', content: 'Hi ' } }] })}\n\n`);
    res.write(`data: ${JSON.stringify({ id: 'c1', object: 'chat.completion.chunk', created: 1, model: req.body.model, choices: [{ index: 0, delta: { content: 'there' }, finish_reason: 'stop' }], usage: { prompt_tokens: 3, completion_tokens: 2, total_tokens: 5 } })}\n\n`);
    res.write('data: [DONE]\n\n');
    return res.end();
  }
  return res.json({
    id: 'c1', object: 'chat.completion', created: 1, model: req.body.model,
    choices: [{ index: 0, message: { role: 'assistant', content: 'Hello from Nous' }, finish_reason: 'stop' }],
    usage: { prompt_tokens: 7, completion_tokens: 3, total_tokens: 10 },
  });
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
  process.env.NEOROUTER_NOUS_PORTAL_URL = base;
  process.env.NEOROUTER_NOUS_BASE_URL = `${base}/v1`;
  require('../support/env');
  const { createApp } = require('../../server/app');
  app = createApp();
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'nous', email: 'nous@example.test', password: 'correct-horse-battery' })).body.session.token;
  secret = (await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'k', mode: 'free_only' })).body.secret;
});

test.after(() => new Promise((resolve) => server.close(resolve)));

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);

test('Nous Portal connects with a device code and lists its models', async () => {
  const start = await api('post', '/providers/nous/login');
  assert.equal(start.status, 200, JSON.stringify(start.body));
  assert.equal(start.body.userCode, 'ABCD-1234');
  assert.match(start.body.verificationUrl, /user_code=ABCD-1234$/);
  const pending = await api('post', `/providers/nous/login/${start.body.loginId}`);
  assert.equal(pending.status, 200, JSON.stringify(pending.body));
  assert.equal(pending.body.status, 'pending');
  const done = await api('post', `/providers/nous/login/${start.body.loginId}`);
  assert.equal(done.status, 200, JSON.stringify(done.body));
  assert.equal(done.body.status, 'done');
  assert.equal(done.body.email, 'portal@example.test');
  assert.equal(done.body.account.billingMode, 'paid');
  accountId = done.body.account.id;
  const models = (await api('get', '/models')).body.models.filter((model) => model.accountId === accountId);
  assert.deepEqual(models.map((model) => model.modelId).sort(), ['hermes-5', 'tencent/hy4:free']);
});

test('a chat carries the signed-in bearer, refreshed from the rotated token', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${secret}`)
    .send({ model: 'auto', messages: [{ role: 'user', content: 'Hi' }] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.choices[0].message.content, 'Hello from Nous');
  const sent = seen.chats.at(-1);
  // The token from the login expired at once, so the rotated one is in use.
  assert.match(sent.headers.authorization, /^Bearer x\./);
  assert.equal(sent.body.model !== undefined, true);
  assert.equal(seen.refreshes >= 1, true);
});

test('the Portal account credits show up as the connection balance', async () => {
  const probe = await api('post', `/providers/accounts/${accountId}/quota`);
  assert.equal(probe.status, 200, JSON.stringify(probe.body));
  const quotas = (await api('get', '/usage/quotas')).body.accounts.find((group) => group.account.id === accountId);
  const balance = quotas.probes.find((item) => item.metric === 'balance');
  assert.equal(balance.remaining, 42.5);
  assert.equal(seen.accountCalls >= 1, true);
});

test('a pasted Portal API key is sent as it is, without a refresh', async () => {
  const before = seen.refreshes;
  const created = await api('post', '/providers/accounts').send({ definitionId: 'nous', secret: 'portalapikeyvalue', billingMode: 'paid' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  assert.equal(created.body.validation.status, 'healthy');
  // Two Nous connections now serve the same models, so the adapter is checked
  // directly instead of hoping the router picks this one.
  const registry = require('../../server/providers/registry');
  const adapter = registry.createAdapter(registry.get('nous'), { secret: 'portalapikeyvalue', baseUrl: null, options: {} });
  assert.deepEqual(adapter.authHeaders(), {}, 'the bearer is prepared per call');
  await adapter.prepare();
  assert.deepEqual(adapter.authHeaders(), { Authorization: 'Bearer portalapikeyvalue' });
  assert.equal(await adapter.fetchQuota(), null, 'an API key has no Portal account to read credits from');
  assert.equal(seen.refreshes, before, 'an API key is never refreshed');
});

test('a login nobody started is refused instead of polled', async () => {
  const unknown = await api('post', '/providers/nous/login/dc-unknown');
  assert.equal(unknown.status, 404);
});

test('a sign-in declined in the Portal says so instead of an internal error', async () => {
  declineNext = true;
  const start = await api('post', '/providers/nous/login');
  const poll = await api('post', `/providers/nous/login/${start.body.loginId}`);
  declineNext = false;
  assert.equal(poll.status, 400, JSON.stringify(poll.body));
  assert.equal(poll.body.error.code, 'NOUS_SIGN_IN_FAILED');
  assert.match(poll.body.error.message, /declined/);
  assert.equal((await api('post', `/providers/nous/login/${start.body.loginId}`)).status, 404, 'a declined sign-in is gone');
});
