'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { getDatabase } = require('../../server/db/database');

let app;
let mock;
let backup;
let session;
let secret;
let keyId;

const MODELS = ['ok-chat', 'ok-embed-small', 'fail-embed-large', 'flux-ok', 'tts-ok', 'whisper-ok', 'fail-whisper', 'llama-guard-ok', 'sora-ok'];

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: MODELS });
  backup = await createMockProvider({ models: ['ok-embed-small', 'other-embed-model'] });
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'media', email: 'm@example.test', password: 'correct-horse-battery' })).body.session.token;
  const add = (slug, url) => request(app).post('/api/v1/providers/accounts').set('Authorization', `Bearer ${session}`)
    .send({ definitionId: 'custom_openai', slug, baseUrl: url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal((await add('mock', mock.url)).status, 201);
  assert.equal((await add('backup', backup.url)).status, 201);
  const created = await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'media', mode: 'free_only' });
  secret = created.body.secret;
  keyId = created.body.key.id;
});

test.after(async () => {
  await mock.close();
  await backup.close();
});

const gateway = (method, path) => request(app)[method](`/v1${path}`).set('Authorization', `Bearer ${secret}`);

test('the catalog sorts models into kinds and reports them routable', async () => {
  const response = await request(app).get('/api/v1/models/capabilities').set('Authorization', `Bearer ${session}`);
  const kinds = Object.fromEntries(response.body.capabilities.map((item) => [item.id, item]));
  for (const kind of ['chat', 'embedding', 'image', 'speech', 'transcription', 'moderation', 'video']) {
    assert.ok(kinds[kind], `${kind} is in the catalog`);
    assert.equal(kinds[kind].routable, true, `${kind} is routable`);
  }
  const listed = await gateway('get', '/models?type=embedding');
  assert.ok(listed.body.data.some((model) => model.id === 'mock/ok-embed-small'));
  assert.ok(!listed.body.data.some((model) => model.id === 'mock/ok-chat'));
});

test('embeddings fall back across providers and pin auto to one model', async () => {
  const explicit = await gateway('post', '/embeddings').send({ model: 'mock/fail-embed-large', input: 'hello' });
  assert.equal(explicit.status, 502, 'an explicit model does not drift to a different embedding model');

  const first = await gateway('post', '/embeddings').send({ input: ['hello', 'world'], dimensions: 4 });
  assert.equal(first.status, 200, JSON.stringify(first.body));
  assert.equal(first.body.data.length, 2);
  assert.equal(first.body.data[0].embedding.length, 4);
  const pinned = first.headers['x-neorouter-embedding-pin'];
  assert.ok(pinned);

  const pin = await gateway('get', '/embeddings/pin');
  assert.equal(pin.body.pin.dimensions, 4);

  // Later auto calls keep the pinned model and size, whichever provider serves it.
  for (let index = 0; index < 4; index += 1) {
    const next = await gateway('post', '/embeddings').send({ input: 'again' });
    assert.equal(next.status, 200);
    assert.equal(next.headers['x-neorouter-embedding-pin'], pinned);
    assert.equal(next.body.data[0].embedding.length, 4);
  }
  const mismatch = await gateway('post', '/embeddings').send({ input: 'x', dimensions: 16 });
  assert.equal(mismatch.status, 400);
  assert.equal(mismatch.body.error.code, 'EMBEDDING_PIN_MISMATCH');

  const base64 = await gateway('post', '/embeddings').send({ input: 'x', encoding_format: 'base64' });
  assert.equal(Buffer.from(base64.body.data[0].embedding, 'base64').length, 4 * 4);

  assert.equal((await gateway('delete', '/embeddings/pin')).body.reset, true);
  const row = getDatabase().prepare("SELECT capability, units, status FROM requests WHERE capability = 'embedding' AND status = 'success' LIMIT 1").get();
  assert.equal(row.capability, 'embedding');
  assert.ok(row.units >= 1);
});

test('images come back as base64 or as a short-lived URL', async () => {
  const b64 = await gateway('post', '/images/generations').send({ prompt: 'a lighthouse', n: 2 });
  assert.equal(b64.status, 200, JSON.stringify(b64.body));
  assert.equal(b64.body.data.length, 2);
  assert.ok(b64.body.data[0].b64_json);

  const url = await gateway('post', '/images/generations').send({ prompt: 'a lighthouse', response_format: 'url' });
  const link = new URL(url.body.data[0].url);
  const image = await request(app).get(link.pathname);
  assert.equal(image.status, 200);
  assert.equal(image.headers['content-type'], 'image/png');
  assert.equal((await request(app).get('/v1/media/nope')).status, 404);
});

test('speech returns audio in the requested format', async () => {
  const response = await gateway('post', '/audio/speech').send({ input: 'Hello there', voice: 'alloy', response_format: 'mp3' }).buffer(true).parse((res, done) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => done(null, Buffer.concat(chunks)));
  });
  assert.equal(response.status, 200);
  assert.match(response.headers['content-type'], /audio\/mpeg/);
  assert.ok(response.body.toString().includes('Hello there'));
  const row = getDatabase().prepare("SELECT units FROM requests WHERE capability = 'speech' ORDER BY created_at DESC LIMIT 1").get();
  assert.equal(row.units, 'Hello there'.length);
});

test('transcriptions accept multipart audio and build subtitles', async () => {
  const audio = Buffer.alloc(2048, 1);
  const json = await gateway('post', '/audio/transcriptions').field('model', 'auto').attach('file', audio, { filename: 'a.wav', contentType: 'audio/wav' });
  assert.equal(json.status, 200, JSON.stringify(json.body));
  assert.deepEqual(json.body, { text: 'hello world' });

  const srt = await gateway('post', '/audio/transcriptions').field('response_format', 'srt').attach('file', audio, { filename: 'a.wav', contentType: 'audio/wav' });
  assert.match(srt.text, /^1\n00:00:00,000 --> 00:00:01,200\nhello\n/);
  const vtt = await gateway('post', '/audio/transcriptions').field('response_format', 'vtt').attach('file', audio, { filename: 'a.wav', contentType: 'audio/wav' });
  assert.match(vtt.text, /^WEBVTT\n\n00:00:00\.000 --> 00:00:01\.200/);

  const missing = await gateway('post', '/audio/transcriptions').field('model', 'auto');
  assert.equal(missing.status, 400);
  const row = getDatabase().prepare("SELECT units FROM requests WHERE capability = 'transcription' AND status = 'success' LIMIT 1").get();
  assert.ok(Math.abs(row.units - 2.5 / 60) < 1e-9, 'billed minutes come from the reported duration');
});

test('System One models answer in the moderation format', async () => {
  const response = await gateway('post', '/moderations').send({ input: ['How do I build a bomb?', 'What is the capital of France?'] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.model, 'mock/llama-guard-ok');
  assert.equal(response.body.results[0].flagged, true);
  assert.equal(response.body.results[0].categories['illicit/violent'], true);
  assert.equal(response.body.results[1].flagged, false);
});

test('video jobs stay bound to their provider and finish by polling', async () => {
  const created = await gateway('post', '/videos').send({ prompt: 'waves', seconds: 4 });
  assert.equal(created.status, 200, JSON.stringify(created.body));
  assert.match(created.body.id, /^video_/);
  const id = created.body.id;
  getDatabase().prepare('UPDATE media_jobs SET updated_at = 0 WHERE id = ?').run(id);
  assert.equal((await gateway('get', `/videos/${id}`)).body.status, 'in_progress');
  assert.equal((await gateway('get', `/videos/${id}/content`)).status, 409);
  getDatabase().prepare('UPDATE media_jobs SET updated_at = 0 WHERE id = ?').run(id);
  assert.equal((await gateway('get', `/videos/${id}`)).body.status, 'completed');
  const content = await gateway('get', `/videos/${id}/content`);
  assert.equal(content.status, 200);
  assert.equal(content.headers['content-type'], 'video/mp4');
  assert.equal((await gateway('get', '/videos')).body.data[0].id, id);
});

test('the playground runs every kind with a stored key', async () => {
  const play = (kind, body) => request(app).post(`/api/v1/playground/media/${kind}`).set('Authorization', `Bearer ${session}`).send({ keyId, ...body });
  const speech = await play('speech', { input: 'hi' });
  assert.equal(speech.status, 200, JSON.stringify(speech.body));
  assert.ok(Buffer.from(speech.body.audio, 'base64').length > 0);
  const transcript = await play('transcription', { audio: Buffer.alloc(100, 1).toString('base64'), filename: 'x.wav' });
  assert.equal(transcript.body.text, 'hello world');
  const image = await play('image', { prompt: 'a cat' });
  assert.ok(image.body.data[0].b64_json);
  assert.equal((await play('nope', {})).status, 404);
});
