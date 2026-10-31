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

const MODELS = ['ok-chat', 'ok-embed-small', 'fail-embed-large', 'flux-ok', 'tts-ok', 'whisper-ok', 'fail-whisper', 'llama-guard-ok', 'sora-ok', 'jev-1.13'];

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
  assert.equal(kinds.weather, undefined, 'kinds without models are left out by default');
  const all = await request(app).get('/api/v1/models/capabilities?all=1').set('Authorization', `Bearer ${session}`);
  assert.deepEqual(all.body.capabilities.find((item) => item.id === 'weather'), { id: 'weather', total: 0, free: 0, routable: true }, '?all=1 lists them too');
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

test('System One decision models answer typed questions', async () => {
  const response = await gateway('post', '/systemone').send({ state: { message: 'hi' }, questions: { reply_needed: { type: 'boolean' } } });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  assert.equal(response.body.model, 'mock/jev-1.13');
  assert.deepEqual(response.body.answers, { reply_needed: true });
  // Classifier checks never pick a decision model.
  const moderated = await gateway('post', '/moderations').send({ input: 'hello' });
  assert.equal(moderated.body.model, 'mock/llama-guard-ok');
  const listed = await gateway('get', '/models?type=moderation');
  assert.equal(listed.body.data.find((model) => model.id === 'mock/jev-1.13').neorouter.decision, true);
  assert.equal(listed.body.data.find((model) => model.id === 'mock/llama-guard-ok').neorouter.decision, false);
  assert.equal(listed.body.data.find((model) => model.id === 'auto').neorouter.decision, true, 'auto can answer /v1/systemone');
});

test('model lists say how many models the key is not allowed to use', async () => {
  const paidKey = await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`).send({ name: 'plans only', allowFree: false, allowPlan: true, allowPaid: false });
  const listed = await request(app).get('/v1/models?type=embedding').set('Authorization', `Bearer ${paidKey.body.secret}`);
  assert.equal(listed.body.data.filter((model) => !model.id.startsWith('auto')).length, 0);
  assert.ok(listed.body.neorouter.not_allowed_for_key.free >= 2, JSON.stringify(listed.body.neorouter));
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

test('media links serve only plain media types, never pages', async () => {
  const mediaStore = require('../../server/capabilities/media/media_store');
  const page = await request(app).get(`/v1/media/${mediaStore.put(Buffer.from('<script>alert(1)</script>'), 'text/html')}`);
  assert.equal(page.status, 200);
  assert.equal(page.headers['content-type'], 'application/octet-stream');
  assert.equal(page.headers['content-disposition'], 'attachment');
  assert.equal(page.headers['x-content-type-options'], 'nosniff');
  assert.match(page.headers['content-security-policy'], /sandbox/);
  const svg = await request(app).get(`/v1/media/${mediaStore.put(Buffer.from('<svg/>'), 'image/svg+xml')}`);
  assert.equal(svg.headers['content-type'], 'application/octet-stream');
  const png = await request(app).get(`/v1/media/${mediaStore.put(Buffer.from('png'), 'image/png')}`);
  assert.equal(png.headers['content-type'], 'image/png');
  assert.equal(png.headers['content-disposition'], undefined);
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
  const decision = await play('decision', { state: { text: 'Where is my parcel?' }, questions: { answer: { type: 'choice', instructions: 'What does the customer want?', criteria: { tracking: 'tracking', refund: 'refund' } } } });
  assert.equal(decision.status, 200, JSON.stringify(decision.body));
  assert.ok(decision.body.answers.answer);
  assert.equal((await play('nope', {})).status, 404);
  const voices = await request(app).get('/api/v1/playground/voices?model=auto').set('Authorization', `Bearer ${session}`);
  assert.ok(voices.body.voices.some((voice) => voice.id === 'alloy'), 'auto offers the portable OpenAI voices');
});

test('a request waits for a model that frees up in a moment, and fails fast when none will', async () => {
  const quota = require('../../server/services/quota/quota_service');
  const catalog = require('../../server/services/catalog/catalog_service');
  const usage = require('../../server/services/usage/usage_service');
  const userId = (await request(app).get('/api/v1/auth/me').set('Authorization', `Bearer ${session}`)).body.user.id;
  const images = catalog.listForUser(userId, { capability: 'image' });
  // Every image model at its per-minute limit for two more seconds: the request queues.
  for (const entry of images) quota.markExhausted(entry, 'rpm', Date.now() + 2000);
  const started = Date.now();
  const waited = await gateway('post', '/images/generations').send({ prompt: 'a lighthouse' });
  assert.equal(waited.status, 200, JSON.stringify(waited.body));
  assert.ok(Date.now() - started >= 1500, 'it waited for the limit to reset');
  assert.ok(usage.logs(userId, { limit: 1 })[0].queuedMs >= 1000, 'the wait is logged');
  // Out for the day: waiting cannot help, so the answer comes at once.
  for (const entry of images) quota.markExhausted(entry, 'rpd', null);
  const fast = Date.now();
  const failed = await gateway('post', '/images/generations').send({ prompt: 'a lighthouse' });
  assert.equal(failed.body.error.code, 'NO_MODEL_AVAILABLE');
  assert.ok(Date.now() - fast < 1500, 'no pointless wait');
  for (const entry of images) quota.markExhausted(entry, 'rpd', Date.now() - 1);
});

test('a key can be limited to kinds of model, with chosen models only narrowing their own kind', async () => {
  const keyWith = async (allowedModels) => (await request(app).post('/api/v1/keys').set('Authorization', `Bearer ${session}`)
    .send({ name: `kinds ${allowedModels.join(' ')}`, mode: 'free_only', allowedModels })).body.secret;
  const as = (key) => (method, path) => request(app)[method](`/v1${path}`).set('Authorization', `Bearer ${key}`);
  const chat = (call) => call('post', '/chat/completions').send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] });
  const speak = (call) => call('post', '/audio/speech').send({ model: 'auto', input: 'hello' });

  // No text models, but voice and System One.
  const voiceOnly = as(await keyWith(['kind:speech', 'kind:moderation']));
  assert.notEqual((await chat(voiceOnly)).status, 200, 'text models are off for this key');
  assert.equal((await speak(voiceOnly)).status, 200);
  assert.equal((await voiceOnly('post', '/moderations').send({ input: 'hello' })).status, 200);

  // One chosen chat model plus any speech model.
  const mixed = as(await keyWith(['kind:chat', 'kind:speech', 'mock/ok-chat']));
  const answered = await chat(mixed);
  assert.equal(answered.status, 200, JSON.stringify(answered.body));
  assert.equal(answered.headers['x-neorouter-model'], 'mock/ok-chat');
  assert.equal((await speak(mixed)).status, 200, 'the chat pick does not narrow speech');

  // Without kinds, model patterns still limit every kind, as before.
  const legacy = as(await keyWith(['mock/ok-chat']));
  assert.notEqual((await speak(legacy)).status, 200);
});
