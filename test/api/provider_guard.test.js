'use strict';

require('../support/env');
const http = require('node:http');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { isPrivateAddress } = require('../../server/providers/net_guard');

let app;
let mock;
let catcher;
let accountId;
const seen = [];
const users = {};

function api(who, method, path, body, space) {
  let call = request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${users[who].token}`);
  if (space) call = call.set('X-NeoRouter-Space', users[space].id);
  return body ? call.send(body) : call;
}

async function register(name) {
  const response = await request(app).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: 'correct-horse-battery' });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  users[name] = { id: response.body.user.id, token: response.body.session.token };
}

async function joinWith(permissions) {
  const link = (await api('owner', 'post', '/teams/invites', { label: 'test', permissions })).body.link;
  const joined = await api('member', 'post', '/teams/invites/redeem', { link });
  assert.equal(joined.status, 200, JSON.stringify(joined.body));
}

function setPermissions(permissions) {
  return api('owner', 'put', `/teams/members/${users.member.id}/permissions`, { permissions });
}

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['echo-one'] });
  // Stands in for a host a member controls: records every key sent to it.
  catcher = http.createServer((req, res) => {
    seen.push(req.headers.authorization || null);
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ data: [] }));
  });
  await new Promise((resolve) => catcher.listen(0, '127.0.0.1', resolve));
  await register('owner');
  await api('owner', 'patch', '/admin/settings', { registrationOpen: true });
  await register('member');
  await register('user');
  await api('owner', 'patch', '/admin/settings', { registrationOpen: false });
  const added = await api('owner', 'post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'mock', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(added.status, 201, JSON.stringify(added.body));
  accountId = added.body.account.id;
  await joinWith(['providers.manage']);
});

test.after(async () => {
  process.env.NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS = 'true';
  await mock.close();
  await new Promise((resolve) => catcher.close(resolve));
});

const catcherUrl = () => `http://127.0.0.1:${catcher.address().port}`;

test('a member cannot move the owner’s key to another host', async () => {
  const moved = await api('member', 'patch', `/providers/accounts/${accountId}`, { baseUrl: catcherUrl() }, 'owner');
  assert.equal(moved.status, 400, JSON.stringify(moved.body));
  assert.equal(moved.body.error.code, 'SECRET_REQUIRED');
  const pathed = await api('member', 'patch', `/providers/accounts/${accountId}`, { options: { modelsPath: `${catcherUrl()}/steal` } }, 'owner');
  assert.equal(pathed.status, 400, 'a models path must stay on the base URL');
  const synced = await api('member', 'post', `/providers/accounts/${accountId}/sync`, null, 'owner');
  assert.equal(synced.status, 200, JSON.stringify(synced.body));
  assert.ok(!seen.includes('Bearer good-key'), 'the stored key never reached the other host');
});

test('the owner can still change the base URL, and a member who types the key again too', async () => {
  const owner = await api('owner', 'patch', `/providers/accounts/${accountId}`, { baseUrl: mock.url });
  assert.equal(owner.status, 200, JSON.stringify(owner.body));
  const member = await api('member', 'patch', `/providers/accounts/${accountId}`, { baseUrl: mock.url, secret: 'good-key' }, 'owner');
  assert.equal(member.status, 200, JSON.stringify(member.body));
});

test('only the instance admin can use local and private addresses', async () => {
  process.env.NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS = 'false';
  const body = { definitionId: 'custom_openai', secret: 'good-key', baseUrl: mock.url };
  const user = await api('user', 'post', '/providers/probe', body);
  assert.equal(user.status, 400, JSON.stringify(user.body));
  assert.equal(user.body.error.code, 'PRIVATE_ADDRESS');
  const created = await api('user', 'post', '/providers/accounts', { ...body, slug: 'local' });
  assert.equal(created.body.error.code, 'PRIVATE_ADDRESS');
  await setPermissions(['providers.manage']);
  const member = await api('member', 'patch', `/providers/accounts/${accountId}`, { baseUrl: mock.url, secret: 'good-key' }, 'owner');
  assert.equal(member.body.error.code, 'PRIVATE_ADDRESS', 'a member of the admin’s team cannot either');
  const admin = await api('owner', 'post', '/providers/probe', body);
  assert.equal(admin.status, 200, JSON.stringify(admin.body));
});

test('a non-admin’s existing local account stops reaching the local network', async () => {
  process.env.NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS = 'true';
  const added = await api('user', 'post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'local', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(added.status, 201, JSON.stringify(added.body));
  const key = (await api('user', 'post', '/keys', { name: 'k', mode: 'free_only' })).body.secret;
  process.env.NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS = 'false';
  const before = mock.calls.length;
  const chat = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
    .send({ model: 'local/echo-one', messages: [{ role: 'user', content: 'hi' }] });
  assert.notEqual(chat.status, 200, JSON.stringify(chat.body));
  assert.equal(mock.calls.length, before, 'the request never left for the loopback host');
});

test('private address ranges are recognised', () => {
  for (const address of ['127.0.0.1', '10.1.2.3', '172.20.0.1', '192.168.1.1', '169.254.169.254', '100.64.0.1', '0.0.0.0', '::1', 'fd00::1', 'fe80::1', '::ffff:127.0.0.1']) {
    assert.equal(isPrivateAddress(address), true, address);
  }
  for (const address of ['8.8.8.8', '1.1.1.1', '2606:4700::1111']) assert.equal(isPrivateAddress(address), false, address);
});
