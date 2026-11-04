'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const registry = require('../../server/providers/registry');
const { classify } = require('../../server/services/catalog/free_classifier');
const { normalizeModel } = require('../../server/providers/model_info');
const { classifyHttpError, KINDS, ProviderError } = require('../../server/providers/provider_error');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const accounts = require('../../server/services/providers/account_service');
const catalog = require('../../server/services/catalog/catalog_service');

// Entries as OpenCode lists them (https://opencode.ai/inference/v1/models and
// /inference/go/v1/models, 2026-11-04), without the fields NeoRouter does not read.
const LISTED = {"zen":[{"id":"big-pickle","name":"Big Pickle","type":"chat","modalities":{"input":["text"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true}},"limit":{"context":200000,"input":160000,"output":32000},"cost":{"text":{"input":{"price":0,"cache":{"read":0,"write":0}},"output":{"price":0}}}},{"id":"glm-5.3-flash-free","name":"GLM-5.3-Flash Free","type":"chat","modalities":{"input":["text","image/jpeg","image/png","video/mp4","video/x-matroska","video/quicktime"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true,"effort":["low","high","max"]}},"limit":{"context":200000,"output":32000},"cost":{"text":{"input":{"price":0,"cache":{"read":0}},"output":{"price":0}}}},{"id":"claude-haiku-5-5","name":"Claude Haiku 5.5","type":"chat","modalities":{"input":["text","image/jpeg","image/png","image/gif","image/webp","application/pdf"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true,"toggle":true,"effort":["low","medium","high","xhigh","max"]}},"limit":{"context":1000000,"output":128000},"cost":{"text":{"input":{"price":0.1,"cache":{"read":0.01,"write":0.125},"overrides":[{"when":{"context":{"gt":100000}},"price":0.5,"cache":{"read":0.05,"write":0.625}}]},"output":{"price":0.5,"overrides":[{"when":{"context":{"gt":100000}},"price":2.5}]}}}},{"id":"glm-5.3-flash","name":"GLM-5.3-Flash","type":"chat","modalities":{"input":["text","image/jpeg","image/png","video/mp4","video/x-matroska","video/quicktime"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true,"effort":["low","high","max"]}},"limit":{"context":1000000,"output":131072},"cost":{"text":{"input":{"price":0.15,"cache":{"read":0.03}},"output":{"price":0.5}}}}],"go":[{"id":"beam-free","name":"Beam Free","type":"chat","modalities":{"input":["text"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true,"effort":["low","medium","high","xhigh","max"]}},"limit":{"context":1048576,"output":131072},"cost":{"text":{"input":{"price":0,"cache":{"read":0}},"output":{"price":0}}}},{"id":"iceman-contributor-free","name":"Iceman Contributor Free","type":"chat","modalities":{"input":["text"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true}},"limit":{"context":262144,"output":131072},"cost":{"text":{"input":{"price":0,"cache":{"read":0}},"output":{"price":0}}}},{"id":"muse-spark-1.3-contributor","name":"Muse Spark 1.3 Contributor","type":"chat","modalities":{"input":["text","image/jpeg","image/png","image/gif","image/webp","video/mp4","application/pdf","audio/mpeg","audio/wav"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true,"effort":["minimal","low","medium","high","xhigh"]}},"limit":{"context":1048576,"output":131072},"cost":{"text":{"input":{"price":0.1,"cache":{"read":0.002}},"output":{"price":0.2}}}},{"id":"glm-5.3-flash","name":"GLM-5.3-Flash","type":"chat","modalities":{"input":["text","image/jpeg","image/png","video/mp4","video/x-matroska","video/quicktime"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true,"effort":["low","high","max"]}},"limit":{"context":1000000,"output":131072},"cost":{"text":{"input":{"price":0.15,"cache":{"read":0.03}},"output":{"price":0.5}}}},{"id":"claude-haiku-5-5","name":"Claude Haiku 5.5","type":"chat","modalities":{"input":["text","image/jpeg","image/png","image/gif","image/webp","application/pdf"],"output":["text"]},"capabilities":{"tools":{"supported":true},"reasoning":{"supported":true,"toggle":true,"effort":["low","medium","high","xhigh","max"]}},"limit":{"context":1000000,"output":128000},"cost":{"text":{"input":{"price":0.1,"cache":{"read":0.01,"write":0.125},"overrides":[{"when":{"context":{"gt":100000}},"price":0.5,"cache":{"read":0.05,"write":0.625}}]},"output":{"price":0.5,"overrides":[{"when":{"context":{"gt":100000}},"price":2.5}]}}}}]};
const adapterFor = (id, listing) => {
  const definition = registry.get(id);
  const adapter = registry.createAdapter(definition, { secret: 'key', baseUrl: definition.baseUrl, options: {} });
  adapter.http = async () => ({ data: { object: 'list', data: listing } });
  return { definition, adapter };
};

test('OpenCode listings: prices, free models, limits and capabilities are read as listed', () => {
  const [pickle, , haiku] = LISTED.zen.map((entry) => normalizeModel(entry));
  assert.deepEqual([pickle.priceIn, pickle.priceOut, pickle.priceKnown, pickle.freeHint], [0, 0, true, true]);
  assert.deepEqual([haiku.priceIn, haiku.priceOut, haiku.priceKnown, haiku.freeHint], [0.1, 0.5, true, false]);
  assert.equal(haiku.contextWindow, 1_000_000);
  assert.equal(haiku.maxOutput, 128_000);
  assert.equal(haiku.vision, true, 'image/jpeg and the like are images');
  assert.equal(haiku.tools, true);
  assert.equal(haiku.reasoning, true);
  assert.equal(haiku.kind, 'chat');
});

test('a listed price is per million tokens whatever its size, and a capability can say it is not supported', () => {
  const model = normalizeModel({ id: 'tiny', cost: { text: { input: { price: 0.005 }, output: { price: 0.008 } } }, capabilities: { tools: { supported: false }, reasoning: { supported: false } }, modalities: { input: ['text'] } });
  assert.deepEqual([model.priceIn, model.priceOut], [0.005, 0.008], 'not mistaken for a per-token price');
  assert.equal(model.tools, false);
  assert.equal(model.reasoning, false);
  assert.equal(model.vision, false);
  assert.equal(normalizeModel({ id: 'old', pricing: { prompt: '0.000001', completion: '0.000002' } }).priceIn, 1, 'other listings are read as before');
});

test('OpenCode Zen: models that only work inside OpenCode are not offered, paid ones keep their listed price', async () => {
  const { definition, adapter } = adapterFor('opencode', LISTED.zen);
  const models = await adapter.listModels();
  assert.deepEqual(models.map((model) => model.id), ['claude-haiku-5-5', 'glm-5.3-flash']);
  for (const model of models) {
    assert.equal(model.priceKnown, true);
    assert.equal(classify({ definition, billingMode: 'paid', modelId: model.id, listing: model }).isFree, false, model.id);
  }
  assert.equal(definition.baseUrl, 'https://opencode.ai/inference/v1');
  assert.equal(definition.headers['x-opencode-session'], 'neorouter');
});

test('OpenCode Go: a plan, free models count as free, training models start switched off', async () => {
  const { definition, adapter } = adapterFor('opencode_go', LISTED.go);
  assert.equal(definition.category, 'plan');
  assert.equal(definition.billing.defaultMode, 'plan');
  assert.equal(definition.baseUrl, 'https://opencode.ai/inference/go/v1');
  assert.equal(definition.headers['x-opencode-session'], 'neorouter', 'Go refuses a request without it');
  const models = await adapter.listModels();
  assert.equal(models.length, 5, 'free models are kept on Go: the subscription covers them');
  assert.deepEqual(models.filter((model) => model.optIn).map((model) => model.id), ['iceman-contributor-free', 'muse-spark-1.3-contributor']);
  const free = models.filter((model) => classify({ definition, billingMode: 'paid', modelId: model.id, listing: model }).isFree).map((model) => model.id);
  assert.deepEqual(free, ['beam-free', 'iceman-contributor-free']);
});

test('models that start switched off stay as the person set them across syncs', async () => {
  migrate();
  const db = getDatabase();
  db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u', 'u', 'u@x.test', 'x', 'admin')").run();
  db.prepare("INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode, prepaid) VALUES ('go', 'u', 'opencode_go', 'opencode-go', 'OpenCode Go', 'paid', 1)").run();
  const { adapter } = adapterFor('opencode_go', LISTED.go);
  const original = accounts.adapterFor;
  accounts.adapterFor = () => adapter;
  const enabled = () => Object.fromEntries(db.prepare("SELECT model_id, enabled FROM models WHERE account_id = 'go'").all().map((row) => [row.model_id, row.enabled]));
  try {
    await catalog.syncAccount('go');
    assert.deepEqual(enabled(), { 'beam-free': 1, 'iceman-contributor-free': 0, 'muse-spark-1.3-contributor': 0, 'glm-5.3-flash': 1, 'claude-haiku-5-5': 1 });
    db.prepare("UPDATE models SET enabled = 1 WHERE model_id = 'muse-spark-1.3-contributor'").run();
    db.prepare("UPDATE models SET enabled = 0 WHERE model_id = 'glm-5.3-flash'").run();
    await catalog.syncAccount('go');
    assert.equal(enabled()['muse-spark-1.3-contributor'], 1, 'switched on by the person');
    assert.equal(enabled()['glm-5.3-flash'], 0, 'switched off by the person');
  } finally {
    accounts.adapterFor = original;
  }
});

test('a 1-token request checks the key where /models is public, on a cheap model', async () => {
  const run = async (failure) => {
    const { adapter } = adapterFor('opencode', [...LISTED.zen, ...LISTED.go.filter((entry) => entry.id === 'claude-haiku-5-5')].slice(0, 4));
    const asked = [];
    adapter.chat = async (_request, { modelId }) => {
      asked.push(modelId);
      if (failure) throw failure;
      return {};
    };
    return { result: await adapter.validate(), asked };
  };
  const ok = await run(null);
  assert.deepEqual(ok.result, { status: 'healthy', message: null });
  assert.deepEqual(ok.asked, ['glm-5.3-flash'], 'the cheap model the definition names, not the first one listed');
  const broke = await run(new ProviderError(KINDS.PAYMENT, 'Upstream request failed: Insufficient account funds'));
  assert.equal(broke.result.status, 'unknown');
  assert.match(broke.result.message, /Insufficient account funds/);
  assert.equal((await run(new ProviderError(KINDS.AUTH, 'Missing API key.'))).result.status, 'invalid');
  assert.equal((await run(new ProviderError(KINDS.NOT_FOUND, 'This Go model requires Global regions.'))).result.status, 'healthy', 'the key works; that model is not open to it');
});

test("OpenCode's refusals are read by what they mean", () => {
  const kind = (status, message) => classifyHttpError(status, { error: { message } }).kind;
  assert.equal(kind(402, 'Upstream request failed: Insufficient account funds'), KINDS.PAYMENT);
  assert.equal(kind(403, 'Upstream request failed: An active OpenCode Go subscription is required to use Go models.'), KINDS.PAYMENT);
  assert.equal(kind(400, "Upstream request failed: This Go model requires Global regions. Select Global in your workspace's Privacy settings to use it."), KINDS.NOT_FOUND);
  assert.equal(kind(401, 'Missing API key.'), KINDS.AUTH, 'a missing key is still a key problem');
  assert.equal(kind(400, 'messages must not be empty'), KINDS.BAD_REQUEST, 'an ordinary bad request is still the caller\'s');
  assert.equal(kind(429, 'Free usage limit reached. Free models work in OpenCode'), KINDS.RATE_LIMIT);
});
