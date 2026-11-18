'use strict';

// Your own account: export the data, export the saved keys (needs 2FA),
// delete the account. Each step asks for the password again.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { generateSync } = require('otplib');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { TOTP_OPTIONS } = require('../../server/services/auth/two_factor_service');

const PASSWORD = 'correct-horse-battery';
let app;
let mock;
const people = {};

const as = (who, method, path, body) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${people[who].token}`).send(body);

async function register(name) {
  const response = await request(app).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: PASSWORD });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  people[name] = { id: response.body.user.id, token: response.body.session.token };
}

async function enableTwoFactor(who) {
  const setup = await as(who, 'post', '/auth/2fa/setup', { currentPassword: PASSWORD });
  people[who].secret = setup.body.manualKey;
  const enabled = await as(who, 'post', '/auth/2fa/enable', { code: code(who) });
  assert.equal(enabled.status, 200);
}

const code = (who) => generateSync({ ...TOTP_OPTIONS, secret: people[who].secret });

test.before(async () => {
  mock = await createMockProvider({ models: ['ok-alpha'] });
  app = createApp();
  await register('admin');
  await as('admin', 'patch', '/admin/settings', { registrationOpen: true });
  await register('ada');
  await register('bob');
  const created = await as('ada', 'post', '/providers/accounts', { definitionId: 'custom_openai', label: 'My mock', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  await as('ada', 'post', '/keys', { name: 'laptop' });
});

test.after(() => mock.close());

test('the data export needs the password and holds no secrets', async () => {
  assert.equal((await as('ada', 'post', '/auth/account/export', { currentPassword: 'wrong-password-1' })).status, 401);
  const response = await as('ada', 'post', '/auth/account/export', { currentPassword: PASSWORD });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.headers['cache-control'], 'no-store');
  const data = response.body;
  assert.equal(data.kind, 'neorouter-account');
  assert.equal(data.profile.username, 'ada');
  assert.equal(data.connections.length, 1);
  assert.equal(data.connections[0].label, 'My mock');
  assert.equal(data.routerKeys[0].name, 'laptop');
  const text = JSON.stringify(data);
  for (const secret of ['good-key', 'password_hash', 'key_hash', 'secret_encrypted', 'token_hash']) assert.ok(!text.includes(secret), secret);
});

test('the key export needs two-factor, the password and a code', async () => {
  const without = await as('ada', 'post', '/auth/account/export-keys', { currentPassword: PASSWORD });
  assert.equal(without.status, 403);
  assert.equal(without.body.error.code, 'TWO_FACTOR_REQUIRED');
  await enableTwoFactor('ada');
  assert.equal((await as('ada', 'post', '/auth/account/export-keys', { currentPassword: PASSWORD })).status, 401, 'no code');
  assert.equal((await as('ada', 'post', '/auth/account/export-keys', { currentPassword: PASSWORD, code: '000000' })).status, 401, 'wrong code');
  assert.equal((await as('ada', 'post', '/auth/account/export-keys', { currentPassword: 'wrong-password-1', code: code('ada') })).status, 401, 'wrong password');
  const response = await as('ada', 'post', '/auth/account/export-keys', { currentPassword: PASSWORD, code: code('ada') });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.kind, 'neorouter-keys');
  assert.deepEqual(response.body.keys.map((key) => [key.label, key.secret]), [['My mock', 'good-key']]);
  assert.equal(response.body.keys[0].billingMode, 'free_tier');
  assert.equal(response.body.keys[0].baseUrl, mock.url);
  people.ada.keyFile = response.body;
});

test('a key file adds the connections to another account, once', async () => {
  const file = people.ada.keyFile;
  const first = await as('bob', 'post', '/providers/accounts/import', file);
  assert.equal(first.status, 200, JSON.stringify(first.body));
  assert.deepEqual(first.body.imported.map((item) => item.label), ['My mock']);
  const connections = (await as('bob', 'get', '/providers/accounts')).body.accounts;
  assert.equal(connections.length, 1);
  assert.equal(connections[0].billingMode, 'free_tier');
  const again = await as('bob', 'post', '/providers/accounts/import', file);
  assert.deepEqual(again.body.skipped.map((item) => item.label), ['My mock'], 'the same key is not added twice');
  assert.equal(again.body.imported.length, 0);
  const odd = await as('bob', 'post', '/providers/accounts/import', { kind: 'neorouter-keys', keys: [
    { definitionId: 'not-a-provider', label: 'Nope', secret: 'x' },
    { definitionId: 'neorouter_peer', label: 'Partner', secret: 'x' },
    { definitionId: 'custom_openai', label: 'Bad key', baseUrl: mock.url, secret: 'bad-key' },
  ] });
  assert.deepEqual(odd.body.failed.map((item) => [item.label, item.code]), [['Nope', 'UNKNOWN_PROVIDER'], ['Partner', 'UNKNOWN_PROVIDER'], ['Bad key', 'KEY_REJECTED']]);
  assert.equal((await as('bob', 'post', '/providers/accounts/import', { kind: 'something-else', keys: [] })).status, 400);
});

test('with two-factor on, the data export needs a code too', async () => {
  assert.equal((await as('ada', 'post', '/auth/account/export', { currentPassword: PASSWORD })).status, 401);
  assert.equal((await as('ada', 'post', '/auth/account/export', { currentPassword: PASSWORD, code: code('ada') })).status, 200);
});

test('the only admin cannot delete their account', async () => {
  const response = await as('admin', 'post', '/auth/account/delete', { currentPassword: PASSWORD });
  assert.equal(response.status, 409);
  assert.equal(response.body.error.code, 'LAST_ADMIN');
});

test('deleting the account needs the password and code, then removes everything', async () => {
  assert.equal((await as('ada', 'post', '/auth/account/delete', { currentPassword: PASSWORD })).status, 401);
  assert.equal((await as('ada', 'post', '/auth/account/delete', { currentPassword: PASSWORD, code: code('ada') })).status, 204);
  assert.equal((await as('ada', 'get', '/auth/me')).status, 401, 'the session ended with the account');
  const login = await request(app).post('/api/v1/auth/login').send({ account: 'ada', password: PASSWORD });
  assert.equal(login.status, 401);
  const users = (await as('admin', 'get', '/admin/users')).body.users;
  assert.ok(!users.some((user) => user.username === 'ada'));
});
