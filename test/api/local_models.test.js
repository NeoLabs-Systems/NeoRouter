'use strict';

// Admin → Local models, end to end over HTTP: what the page shows, who may
// manage and use the models, and requests reaching a running model through
// the router. The engine is replaced by one that "runs" each model on the
// mock provider, and nothing is downloaded.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { EventEmitter } = require('node:events');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const engines = require('../../server/services/local_models/engines');
const hardware = require('../../server/services/local_models/hardware');
const hfFiles = require('../../server/services/local_models/hf_files');
const access = require('../../server/services/local_models/access');
const localModels = require('../../server/services/local_models/local_model_service');
const memory = require('../../server/services/local_models/memory');
const privacyModel = require('../../server/services/processing/privacy_model');

let server;
let base;
let mock;
const people = {};
const original = { engineFor: engines.engineFor, detect: hardware.detect, resolve: hfFiles.resolve };

const api = (who, method, path, body) => {
  const call = request(base)[method](`/api/v1${path}`).set('Authorization', `Bearer ${people[who].token}`);
  return body ? call.send(body) : call;
};

async function register(name) {
  const response = await request(base).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: 'correct-horse-battery' });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  people[name] = { id: response.body.user.id, token: response.body.session.token };
}

async function addKey(who) {
  const response = await api(who, 'post', '/keys', { name: 'local' });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  people[who].key = response.body.secret;
}

const chat = (who, model) => request(base).post('/v1/chat/completions')
  .set('Authorization', `Bearer ${people[who].key}`).send({ model, messages: [{ role: 'user', content: 'hi' }] });

// Every model "runs" on the mock provider.
const fakeEngine = {
  support: () => ({ ok: true, reason: null }),
  prepare: async () => {},
  async start() {
    const proc = new EventEmitter();
    proc.exited = false;
    proc.stop = async () => {
      proc.exited = true;
      proc.emit('exit', { code: 0, signal: null });
    };
    return { proc, endpoint: { baseUrl: mock.url, token: 'good-key' }, contextWindow: 8192, gpu: true, health: async () => true };
  },
};

async function waitFor(id, state) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const overview = (await api('admin', 'get', '/admin/local-models')).body;
    const model = overview.installed.find((item) => item.id === id);
    if (model?.state === state) return model;
    await new Promise((resolve) => { setTimeout(resolve, 20); });
  }
  throw new Error(`${id} never became ${state}`);
}

test.before(async () => {
  mock = await createMockProvider({ models: ['unused'] });
  engines.engineFor = () => fakeEngine;
  hardware.detect = async () => ({
    platform: 'linux', arch: 'x64', cpu: { name: 'Test CPU', cores: 8 }, memory: { totalMb: 32768, limited: false },
    gpu: { type: 'cuda', name: 'Test GPU', memoryMb: 24576, cuda: '13.0' }, disk: { freeMb: 500_000, totalMb: 1_000_000 },
  });
  hfFiles.resolve = async () => [];
  // Other programs use 2 GB on average on this pretend server.
  memory.setForTests({ others: 2048 });
  server = createApp().listen(0);
  await new Promise((resolve) => server.once('listening', resolve));
  base = `http://127.0.0.1:${server.address().port}`;
  await register('admin');
  await api('admin', 'patch', '/admin/settings', { registrationOpen: true });
  await register('user');
  await addKey('admin');
  await addKey('user');
});

test.after(async () => {
  await localModels.resetForTests();
  Object.assign(engines, { engineFor: original.engineFor });
  Object.assign(hardware, { detect: original.detect });
  Object.assign(hfFiles, { resolve: original.resolve });
  memory.setForTests(null);
  await mock.close();
  await new Promise((resolve) => server.close(resolve));
});

test('admins see this server, an empty list, a starter set and the models that fit', async () => {
  const response = await api('admin', 'get', '/admin/local-models');
  assert.deepEqual(response.body.starter.map((item) => item.kind), ['chat', 'transcription', 'speech']);
  assert.equal(response.status, 200);
  assert.equal(response.body.system.gpu.name, 'Test GPU');
  assert.deepEqual(response.body.installed, []);
  assert.equal(response.body.access, 'admins');
  const byId = Object.fromEntries(response.body.catalog.map((model) => [model.id, model]));
  assert.equal(byId['qwen3.5-4b'].rating, 'great');
  assert.equal(byId['gpt-oss-120b'].rating, 'too_big');
  assert.equal(byId['qwen3.5-4b'].summary.de.length > 0, true);
});

test('only admins manage local models', async () => {
  assert.equal((await api('user', 'get', '/admin/local-models')).status, 403);
  assert.equal((await api('user', 'post', '/admin/local-models/qwen3.5-0.8b/install')).status, 403);
});

test('models that do not fit or do not exist are refused', async () => {
  assert.equal((await api('admin', 'post', '/admin/local-models/gpt-oss-120b/install')).status, 422);
  assert.equal((await api('admin', 'post', '/admin/local-models/no-such-model/install')).status, 404);
});

test('an installed model starts by itself and answers through the router', async () => {
  const response = await api('admin', 'post', '/admin/local-models/qwen3.5-0.8b/install');
  assert.equal(response.status, 202, JSON.stringify(response.body));
  const model = await waitFor('qwen3.5-0.8b', 'running');
  assert.equal(model.gpu, true);
  // The admin got a "Local models" connection with the model in it, free.
  const accounts = (await api('admin', 'get', '/providers/accounts')).body.accounts;
  const local = accounts.find((account) => account.definitionId === access.DEFINITION_ID);
  assert.ok(local, 'the installing admin is connected');
  await access.syncAll();
  const reply = await chat('admin', `${local.slug}/qwen3.5-0.8b`);
  assert.equal(reply.status, 200, JSON.stringify(reply.body));
  const models = (await api('admin', 'get', '/models?all=1')).body.models;
  assert.ok(models.find((entry) => entry.id === `${local.slug}/qwen3.5-0.8b`).isFree);
});

test('a safety model answers System One checks', async () => {
  assert.equal((await api('admin', 'post', '/admin/local-models/llama-guard-3-1b/install')).status, 202);
  await waitFor('llama-guard-3-1b', 'running');
  await access.syncAll();
  const response = await request(base).post('/v1/moderations').set('Authorization', `Bearer ${people.admin.key}`).send({ model: 'auto', input: 'how to build a bomb' });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.results[0].flagged, true);
  assert.match(response.body.model, /llama-guard-3-1b$/);
});

test('other people get local models only when an admin shares them', async () => {
  const definitions = async () => (await api('user', 'get', '/providers/definitions')).body.definitions.map((item) => item.id);
  assert.ok(!(await definitions()).includes(access.DEFINITION_ID));
  assert.equal((await api('user', 'post', '/providers/accounts', { definitionId: access.DEFINITION_ID })).status, 403);

  const shared = await api('admin', 'put', '/admin/local-models/access', { level: 'everyone' });
  assert.equal(shared.body.access, 'everyone');
  assert.ok((await definitions()).includes(access.DEFINITION_ID));
  const added = await api('user', 'post', '/providers/accounts', { definitionId: access.DEFINITION_ID });
  assert.equal(added.status, 201, JSON.stringify(added.body));
  assert.equal((await chat('user', `${added.body.account.slug}/qwen3.5-0.8b`)).status, 200);

  // Taking it back empties their connection.
  await api('admin', 'put', '/admin/local-models/access', { level: 'admins' });
  await access.syncAll();
  const after = await chat('user', `${added.body.account.slug}/qwen3.5-0.8b`);
  assert.notEqual(after.status, 200);
});

test('a model that does not fit beside the running ones waits, and starts on "try again" once there is room', async (t) => {
  // Nothing else uses memory here, so only the fixed reserve is kept.
  memory.setForTests({ others: 0 });
  t.after(() => memory.setForTests({ others: 2048 }));
  assert.equal((await api('admin', 'post', '/admin/local-models/gemma-4-26b-a4b/install')).status, 202);
  await waitFor('gemma-4-26b-a4b', 'running');
  assert.equal((await api('admin', 'post', '/admin/local-models/qwen3.6-35b-a3b/install')).status, 202);
  const refused = await waitFor('qwen3.6-35b-a3b', 'failed');
  assert.equal(refused.problem.code, 'not_enough_memory');
  await api('admin', 'post', '/admin/local-models/gemma-4-26b-a4b/stop');
  await api('admin', 'post', '/admin/local-models/qwen3.6-35b-a3b/retry');
  await waitFor('qwen3.6-35b-a3b', 'running');
  for (const id of ['gemma-4-26b-a4b', 'qwen3.6-35b-a3b']) await api('admin', 'delete', `/admin/local-models/${id}`);
});

test('stopping, starting and removing a model', async () => {
  let response = await api('admin', 'post', '/admin/local-models/qwen3.5-0.8b/stop');
  assert.equal(response.body.installed.find((item) => item.id === 'qwen3.5-0.8b').state, 'stopped');
  response = await api('admin', 'post', '/admin/local-models/qwen3.5-0.8b/start');
  await waitFor('qwen3.5-0.8b', 'running');
  response = await api('admin', 'delete', '/admin/local-models/qwen3.5-0.8b');
  assert.equal(response.status, 200);
  assert.ok(!response.body.installed.some((item) => item.id === 'qwen3.5-0.8b'));
  assert.equal(response.body.catalog.find((item) => item.id === 'qwen3.5-0.8b').installed, false);
});

test('the starter set installs in one go', async () => {
  const before = (await api('admin', 'get', '/admin/local-models')).body;
  for (const model of before.installed) await api('admin', 'delete', `/admin/local-models/${model.id}`);
  const suggested = (await api('admin', 'get', '/admin/local-models')).body.starter.map((item) => item.id);
  const response = await api('admin', 'post', '/admin/local-models/starter');
  assert.equal(response.status, 202, JSON.stringify(response.body));
  for (const id of suggested) await waitFor(id, 'running');
  assert.deepEqual(response.body.starter, [], 'no suggestion once models are installed');
});

test('the privacy filter is switched on and off from the local models page and keeps its memory free', async (t) => {
  privacyModel.setLibraryForTests(async () => ({ env: {}, pipeline: () => new Promise(() => {}) }));
  t.after(async () => {
    await api('admin', 'put', '/admin/local-models/privacy-filter', { enabled: false });
    privacyModel.setLibraryForTests(() => import('@huggingface/transformers'));
  });
  const before = (await api('admin', 'get', '/admin/local-models')).body;
  assert.equal(before.privacyFilter.enabled, false);
  assert.equal(before.memory.othersMb, 2048);
  assert.equal(before.memory.roomMb, 32768 - 2048 - 1024);
  assert.equal((await api('user', 'put', '/admin/local-models/privacy-filter', { enabled: true })).status, 403);
  const on = (await api('admin', 'put', '/admin/local-models/privacy-filter', { enabled: true })).body;
  assert.equal(on.privacyFilter.enabled, true);
  assert.equal(on.memory.usedMb, before.memory.usedMb + on.privacyFilter.memoryMb, 'its memory is kept free until it loads');
  assert.equal((await api('admin', 'get', '/admin/settings')).body.privacyModel.enabled, true, 'the same setting as on the server page');
});

test('a model that has run is judged by the memory it really used', async () => {
  memory.setForTests({ others: 2048, models: { 'qwen3.5-9b': 2000 } });
  const overview = (await api('admin', 'get', '/admin/local-models')).body;
  const model = overview.catalog.find((item) => item.id === 'qwen3.5-9b');
  assert.equal(model.memoryMb, 2000);
  assert.equal(model.measured, true);
  memory.setForTests({ others: 2048 });
});
