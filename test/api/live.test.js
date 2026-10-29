'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const WebSocket = require('ws');
const { createApp } = require('../../server/app');
const live = require('../../server/capabilities/live/gateway');
const { createMockProvider } = require('../support/mock_provider');

let app;
let server;
let base;
let mock;
let session;
let secret;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);

test.before(async () => {
  app = createApp();
  server = app.listen(0, '127.0.0.1');
  live.attach(server);
  await new Promise((resolve) => server.once('listening', resolve));
  base = `ws://127.0.0.1:${server.address().port}`;
  mock = await createMockProvider({ models: ['gpt-live-1', 'fail-realtime', 'ok-chat'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'live', email: 'live@example.test', password: 'correct-horse-battery' })).body.session.token;
  const account = await api('post', '/providers/accounts').send({ definitionId: 'custom_openai', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(account.status, 201, JSON.stringify(account.body));
  secret = (await api('post', '/keys').send({ name: 'live', mode: 'free_only' })).body.secret;
});

test.after(async () => {
  await mock.close();
  await new Promise((resolve) => server.close(resolve));
});

function open(path, headers = { Authorization: `Bearer ${secret}` }) {
  const socket = new WebSocket(`${base}${path}`, { headers });
  const events = [];
  const waiters = [];
  socket.on('message', (data) => {
    events.push(JSON.parse(String(data)));
    for (const waiter of waiters.splice(0)) waiter();
  });
  const next = (type) => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`no ${type} event; got ${JSON.stringify(events)}`)), 3000);
    const check = () => {
      const found = events.find((event) => event.type === type);
      if (found) {
        clearTimeout(timer);
        return resolve(found);
      }
      return waiters.push(check);
    };
    check();
  });
  const closed = new Promise((resolve) => socket.on('close', (code, reason) => resolve({ code, reason: String(reason) })));
  return { socket, next, closed, opened: new Promise((resolve, reject) => { socket.once('open', resolve); socket.once('error', reject); }) };
}

test('live models are their own kind in the catalog', async () => {
  const caps = (await api('get', '/models/capabilities')).body.capabilities;
  const liveCap = caps.find((item) => item.id === 'live');
  assert.ok(liveCap, JSON.stringify(caps));
  assert.equal(liveCap.routable, true);
  const listed = await request(app).get('/v1/models?type=live').set('Authorization', `Bearer ${secret}`);
  const model = listed.body.data.find((item) => item.id.endsWith('/gpt-live-1'));
  assert.deepEqual(model.neorouter.protocols, ['openai_live', 'openai_realtime']);
  const auto = listed.body.data.find((item) => item.id === 'auto');
  assert.deepEqual(auto.neorouter.protocols, ['openai_live', 'openai_realtime'], 'auto lists the protocols that reach its models');
});

test('a GPT-Live session is relayed with the model rewritten and usage logged', async () => {
  const client = open('/v1/live/sessions');
  await client.opened;
  client.socket.send(JSON.stringify({ type: 'session.start', session: { model: 'auto', instructions: 'hi' } }));
  const started = await client.next('session.started');
  assert.equal(started.session.model, 'gpt-live-1', 'auto picked the live model and the provider saw its own id');
  client.socket.send(JSON.stringify({ type: 'session.input_audio.append', audio: 'AAAA' }));
  const echoed = await client.next('echo');
  assert.equal(echoed.event.audio, 'AAAA');
  await client.next('response.done');
  client.socket.send(JSON.stringify({ type: 'usage.binary' }));
  await client.next('echo');
  await new Promise((resolve) => setTimeout(resolve, 50));
  client.socket.close();
  await client.closed;
  await new Promise((resolve) => setTimeout(resolve, 50));
  const logged = (await api('get', '/usage/logs?limit=5')).body;
  const row = JSON.stringify(logged);
  assert.match(row, /openai_live/, row.slice(0, 600));
  assert.match(row, /gpt-live-1/);
  const live = logged.logs.find((item) => item.capability === 'live' || /openai_live/.test(JSON.stringify(item)));
  assert.equal(live.inputTokens ?? live.input_tokens, 17, 'JSON and binary usage reports both count');
});

test('the realtime protocol takes the model from the URL', async () => {
  const client = open('/v1/realtime?model=gpt-live-1', { Authorization: `Bearer ${secret}`, 'OpenAI-Beta': 'realtime=v1' });
  await client.opened;
  const created = await client.next('session.created');
  assert.equal(created.session.model, 'gpt-live-1');
  client.socket.close();
});

test('a refused provider socket ends the session with an error event', async () => {
  const client = open('/v1/realtime?model=fail-realtime');
  await client.opened;
  const error = await client.next('error');
  assert.match(error.error.message, /attempt|No live model/);
  const closed = await client.closed;
  assert.equal(closed.code, 1011);
});

test('a missing or wrong key is refused before the upgrade', async () => {
  const client = open('/v1/live/sessions', { Authorization: 'Bearer nrt_wrong' });
  await assert.rejects(client.opened, /401/);
  const viaQuery = open(`/v1/live/sessions?key=${secret}`, {});
  await viaQuery.opened;
  viaQuery.socket.close();
});

test('a provider that hangs up before answering counts as a failed attempt', async () => {
  const client = open('/v1/live/sessions');
  await client.opened;
  client.socket.send(JSON.stringify({ type: 'session.start', session: { model: 'fail-realtime' } }));
  const error = await client.next('error');
  assert.match(error.error.message, /model failed/);
  assert.equal((await client.closed).code, 1011);
});
