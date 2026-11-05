'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const concurrency = require('../../server/services/routing/concurrency');

let app;
let provider;
let session;
let account;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);

test.before(async () => {
  app = createApp();
  provider = await createMockProvider({ models: ['steady'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'limits', email: 'limits@example.test', password: 'correct-horse-battery' })).body.session.token;
  const created = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', baseUrl: provider.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  account = created.body.account;
});

test.after(async () => provider.close());

const stated = async () => (await api('get', '/providers/accounts')).body.accounts.find((item) => item.id === account.id);

test('stated limits are saved, reported with their source, and cleared with null', async () => {
  const saved = await api('put', `/providers/accounts/${account.id}/limits`).send({ rpm: 20, concurrent: 4 });
  assert.equal(saved.status, 200, JSON.stringify(saved.body));
  assert.deepEqual(saved.body.account.limits, { rpm: 20, concurrent: 4 });
  assert.deepEqual(saved.body.account.concurrency, { limit: 4, source: 'manual' });

  // Absent fields stay; the shown limits carry the manual source.
  await api('put', `/providers/accounts/${account.id}/limits`).send({ rpd: 500 });
  assert.deepEqual((await stated()).limits, { rpm: 20, rpd: 500, concurrent: 4 });
  const quotas = (await api('get', '/usage/quotas')).body.accounts.find((item) => item.account.id === account.id);
  const rpm = quotas.limits.find((row) => row.metric === 'rpm');
  assert.deepEqual(rpm.shared, { limit: 20, source: 'manual' });

  await api('put', `/providers/accounts/${account.id}/limits`).send({ rpm: null, rpd: null, concurrent: null });
  const cleared = await stated();
  assert.deepEqual(cleared.limits, {});
  assert.equal(cleared.concurrency, null);
});

test('a stated concurrency limit is never learned over, and it holds after the learned ones fade', async () => {
  concurrency.resetForTests();
  await api('put', `/providers/accounts/${account.id}/limits`).send({ concurrent: 6 });
  const entry = { accountId: account.id, definitionId: 'custom_openai' };
  concurrency.onRateLimited(entry, { message: 'Too many concurrent requests' }, { running: 3 });
  assert.deepEqual(concurrency.limitFor(entry), { limit: 6, source: 'manual' });
  assert.deepEqual(concurrency.limitFor(entry, Date.now() + 10 * 3_600_000), { limit: 6, source: 'manual' }, 'does not fade');
  await api('put', `/providers/accounts/${account.id}/limits`).send({ concurrent: null });
  assert.equal(concurrency.limitFor(entry), null, 'cleared: back to learning, and nothing was learned meanwhile');
});

test('a stated requests-per-minute limit stops routing to the account once it is used', async () => {
  await api('put', `/providers/accounts/${account.id}/limits`).send({ rpm: 1 });
  const key = (await api('post', '/keys').send({ name: 'limits', mode: 'free_only' })).body.secret;
  const ask = () => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
    .send({ model: `${account.slug}/steady`, messages: [{ role: 'user', content: 'hi' }] });
  assert.equal((await ask()).status, 200);
  const second = await ask();
  assert.notEqual(second.status, 200, 'the one request a minute is spent');
  await api('put', `/providers/accounts/${account.id}/limits`).send({ rpm: null });
});

test('limits must be whole numbers of at least one, and only known fields are accepted', async () => {
  for (const body of [{ rpm: 0 }, { rpm: 1.5 }, { rpm: -3 }, { rpm: 'ten' }, { burst: 5 }]) {
    const response = await api('put', `/providers/accounts/${account.id}/limits`).send(body);
    assert.equal(response.status, 400, JSON.stringify(body));
  }
  assert.equal((await api('put', '/providers/accounts/nope/limits').send({ rpm: 5 })).status, 404);
});
