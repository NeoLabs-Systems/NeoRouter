'use strict';

// Settings → Connect your apps: catalog, addresses by where an app runs, the
// live check of a key, model name suggestions and the network switch.
require('../support/env');
const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { ENV_FILE } = require('../../runtime/paths');
const { detectContainer } = require('../../runtime/container');

let app;
let mock;
const people = {};
const as = (who, method, path, body) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${people[who]}`).send(body);

async function register(name) {
  people[name] = (await request(app).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: 'correct-horse-battery' })).body.session.token;
}

test.before(async () => {
  mock = await createMockProvider({ models: ['qwen3.8-flash-next', 'ok-beta'] });
  app = createApp();
  await register('admin');
  await as('admin', 'patch', '/admin/settings', { registrationOpen: true });
  await register('user');
  const created = await as('user', 'post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'spark', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
});

test.after(() => mock.close());

test('the catalog and an address for every place an app can run', async () => {
  const response = await as('user', 'get', '/apps');
  assert.equal(response.status, 200);
  const ids = response.body.apps.map((item) => item.id);
  for (const id of ['pi-dev', 'cline', 'open-webui', 'claude-code', 'other-openai']) assert.ok(ids.includes(id), id);
  const places = Object.fromEntries(response.body.places.map((place) => [place.id, place]));
  assert.match(places.local.address, /^http:\/\/127\.0\.0\.1:\d+\/v1$/);
  assert.equal(places.local.reachable, true);
  assert.match(places.docker.address, /^http:\/\/host\.docker\.internal:\d+\/v1$/);
  assert.equal(places.internet.reachable, false, 'no public URL in tests');
  assert.equal(places.internet.fix, 'public_url');
  assert.ok(['local', 'network'].includes(response.body.network.scope));
});

test('the live check sees a request arrive, and why it failed', async () => {
  const key = (await as('user', 'post', '/keys', { name: 'Pi Dev' })).body;
  const before = (await as('user', 'get', `/apps/keys/${key.key.id}/activity`)).body;
  assert.equal(before.lastUsedAt, null);
  assert.equal(before.request, null);
  // A shortened model name: refused, with the full name suggested.
  const wrong = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key.secret}`)
    .send({ model: 'spark/qwen3.8', messages: [{ role: 'user', content: 'Hi' }] });
  assert.equal(wrong.status, 404);
  assert.match(wrong.body.error.message, /Did you mean "spark\/qwen3\.8-flash-next"\?/);
  assert.deepEqual(wrong.body.error.details.suggestions, ['spark/qwen3.8-flash-next']);
  const failed = (await as('user', 'get', `/apps/keys/${key.key.id}/activity`)).body;
  assert.ok(failed.lastUsedAt);
  assert.equal(failed.request.ok, false);
  assert.equal(failed.request.errorCode, 'MODEL_NOT_FOUND');
  assert.equal(failed.request.requested, 'spark/qwen3.8');
  // The right name answers, and the check turns green.
  const right = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key.secret}`)
    .send({ model: 'spark/qwen3.8-flash-next', messages: [{ role: 'user', content: 'Hi' }] });
  assert.equal(right.status, 200, JSON.stringify(right.body));
  const ok = (await as('user', 'get', `/apps/keys/${key.key.id}/activity`)).body;
  assert.equal(ok.request.ok, true);
  assert.ok(ok.request.model);
  // Someone else's key is not found.
  assert.equal((await as('admin', 'get', `/apps/keys/${key.key.id}/activity`)).status, 404);
});

test('only an admin opens NeoRouter to containers and the network', async () => {
  assert.equal((await as('user', 'put', '/apps/network', { scope: 'network' })).status, 403);
  const opened = await as('admin', 'put', '/apps/network', { scope: 'network' });
  assert.equal(opened.status, 200, JSON.stringify(opened.body));
  assert.equal(opened.body.restarting, false, 'tests do not run under launchd or systemd');
  if (!detectContainer()) {
    assert.equal(opened.body.restartNeeded, true);
    assert.match(fs.readFileSync(ENV_FILE, 'utf8'), /^NEOROUTER_HOST=0\.0\.0\.0$/m);
    await as('admin', 'put', '/apps/network', { scope: 'local' });
    assert.match(fs.readFileSync(ENV_FILE, 'utf8'), /^NEOROUTER_HOST=127\.0\.0\.1$/m);
  }
});

test('on its own domain, apps get that one address and no local choices', () => {
  const { places } = require('../../server/services/apps/places');
  const hosted = places('https://router.example.com');
  assert.deepEqual(hosted.map((place) => [place.id, place.address, place.reachable]), [['internet', 'https://router.example.com/v1', true]]);
  // At home or on this computer, every place is offered.
  for (const origin of ['http://127.0.0.1:4600', 'https://neorouter.local', 'https://192.168.1.20', 'http://router.example.com']) {
    assert.deepEqual(places(origin).map((place) => place.id), ['local', 'docker', 'network', 'internet'], origin);
  }
});
