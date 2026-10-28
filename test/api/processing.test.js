'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');

let app;
let mock;
let session;
let key;

const auth = (call) => call.set('Authorization', `Bearer ${session}`);

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['echo-one'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'priv', email: 'priv@example.test', password: 'correct-horse-battery' })).body.session.token;
  await auth(request(app).post('/api/v1/providers/accounts')).send({ definitionId: 'custom_openai', slug: 'mock', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  key = (await auth(request(app).post('/api/v1/keys')).send({ name: 'k', mode: 'free_only' })).body.secret;
  const { settings } = (await auth(request(app).get('/api/v1/processing/settings'))).body;
  settings.privacy.enabled = true;
  settings.privacy.customTerms = ['Acme Holding'];
  settings.compression.level = 'lossless';
  const saved = await auth(request(app).put('/api/v1/processing/settings')).send(settings);
  assert.equal(saved.status, 200, JSON.stringify(saved.body));
});

test.after(() => mock.close());

const message = 'Mail jana@example.org about Acme Holding, password: hunter2pw\n\n\n\nthanks';

test('providers only see placeholders and the client gets the real values back', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
    .send({ model: 'mock/echo-one', messages: [{ role: 'user', content: message }] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  const sent = mock.calls.at(-1).messages;
  const sentText = JSON.stringify(sent);
  assert.ok(!sentText.includes('jana@example.org') && !sentText.includes('Acme Holding') && !sentText.includes('hunter2pw'), sentText);
  assert.match(sent.at(-1).content, /\[EMAIL_1\].*\[PRIVATE_1\].*\[SECRET_1\]/s);
  assert.equal(sent[0].role, 'system', 'the model is told to keep placeholders');
  const answer = response.body.choices[0].message.content;
  assert.match(answer, /jana@example\.org/);
  assert.match(answer, /Acme Holding/);
  assert.match(answer, /\[SECRET_1\]/, 'secrets are hidden for good, not restored');
  assert.equal(response.headers['x-neorouter-redacted'], '3');
  assert.match(response.headers['x-neorouter-compression'], /^lossless; saved~=\d+/);
});

test('streamed answers are restored even when placeholders arrive in pieces', async () => {
  const response = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${key}`)
    .send({ model: 'mock/echo-one', stream: true, messages: [{ role: 'user', content: 'Write to jana@example.org now' }] });
  assert.equal(response.status, 200);
  const text = response.text.split('\n').filter((line) => line.startsWith('data: {'))
    .map((line) => JSON.parse(line.slice(6)).choices?.[0]?.delta?.content || '').join('');
  assert.equal(text, 'Write to jana@example.org now');
});

test('preview shows what would be sent, and settings are validated', async () => {
  const preview = await auth(request(app).post('/api/v1/processing/preview')).send({ text: 'Call +49 170 1234567' });
  assert.equal(preview.status, 200);
  assert.equal(preview.body.messages.at(-1).content, 'Call [PHONE_1]');
  assert.equal(preview.body.stats.redacted, 1);
  const invalid = await auth(request(app).put('/api/v1/processing/settings')).send({ privacy: { enabled: true }, compression: { level: 'ultra' } });
  assert.equal(invalid.status, 400);
  const stats = (await auth(request(app).get('/api/v1/processing/stats'))).body;
  assert.ok(stats.redacted >= 4 && stats.protectedRequests >= 2, JSON.stringify(stats));
});
