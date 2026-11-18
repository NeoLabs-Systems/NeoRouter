'use strict';

// Speech and image requests carry options (a voice, a size) that differ per
// provider. These tests stub the provider calls and check what is sent.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const registry = require('../../server/providers/registry');
const { ProviderError, KINDS } = require('../../server/providers/provider_error');
const { explainNoModel } = require('../../server/services/routing/router');

const adapterFor = (id, options = {}) => registry.createAdapter(registry.get(id), { secret: 'key', baseUrl: null, options });

test('Cloudflare Aura gets only a voice Aura has; another provider\'s voice falls back to its default', async () => {
  const adapter = adapterFor('cloudflare', { accountId: 'a'.repeat(32) });
  const sent = [];
  adapter.run = async (modelId, body) => {
    sent.push(body);
    return { bytes: Buffer.from('audio'), mime: 'audio/mpeg' };
  };
  await adapter.speech({ input: 'Hello there.', voice: 'Kore' }, { modelId: '@cf/deepgram/aura-2-en' });
  await adapter.speech({ input: 'Hello there.', voice: 'Luna' }, { modelId: '@cf/deepgram/aura-2-en' });
  await adapter.speech({ input: 'Hello there.', voice: 'selene' }, { modelId: '@cf/deepgram/aura-2-en' });
  await adapter.speech({ input: 'Hola.', voice: 'luna' }, { modelId: '@cf/deepgram/aura-2-es' });
  assert.deepEqual(sent.map((body) => body.speaker ?? null), [null, 'luna', null, null]);
  assert.equal(sent[0].text, 'Hello there.');
});

test('Cloudflare FLUX.2 images go out as form data, other image models as JSON', async () => {
  const adapter = adapterFor('cloudflare', { accountId: 'a'.repeat(32) });
  const sent = [];
  adapter.run = async (modelId, body) => {
    sent.push(body);
    return { result: { image: Buffer.from('png').toString('base64') } };
  };
  await adapter.generateImage({ prompt: 'a cat', size: '1024x1024' }, { modelId: '@cf/black-forest-labs/flux-2-klein-4b' });
  await adapter.generateImage({ prompt: 'a cat', size: '1024x1024' }, { modelId: '@cf/black-forest-labs/flux-1-schnell' });
  assert.ok(sent[0] instanceof FormData);
  assert.equal(sent[0].get('width'), '1024');
  assert.deepEqual(sent[1], { prompt: 'a cat', width: 1024, height: 1024 });
});

function elevenLabs({ refuse = () => false } = {}) {
  const adapter = adapterFor('elevenlabs');
  const spoken = [];
  adapter.http = async (path) => {
    if (path.includes('/v2/voices')) return { data: { voices: [{ voice_id: 'DefaultFemale0000001', name: 'Talia', category: 'premade', labels: { gender: 'female' } }, { voice_id: 'DefaultMale000000001', name: 'Eldrin', category: 'premade', labels: { gender: 'male' } }] } };
    return { data: { voices: [{ voice_id: 'LibraryVoice00000001', name: 'Narrator', category: 'professional' }, { voice_id: 'MyOwnClone0000000001', name: 'Me', category: 'cloned' }] } };
  };
  adapter.request = async (url) => {
    const voiceId = decodeURIComponent(/text-to-speech\/([^?]+)/.exec(url)[1]);
    spoken.push(voiceId);
    if (refuse(voiceId)) throw new ProviderError(KINDS.PAYMENT, 'Free users cannot use library voices via the API. Please upgrade your subscription to use this voice.');
    return { arrayBuffer: async () => new TextEncoder().encode('audio').buffer, headers: new Headers() };
  };
  return { adapter, spoken };
}

test('ElevenLabs picks voices the plan can use: defaults, OpenAI names by voice type, own voices', async () => {
  const { adapter } = elevenLabs();
  assert.equal(await adapter.voiceId(undefined), 'DefaultFemale0000001');
  assert.equal(await adapter.voiceId('onyx'), 'DefaultMale000000001', 'an OpenAI voice name gets a default voice of that type');
  assert.equal(await adapter.voiceId('Kore'), 'DefaultFemale0000001', 'an unknown name gets the default');
  assert.equal(await adapter.voiceId('Me'), 'MyOwnClone0000000001', 'the account\'s own voice by name');
});

test('ElevenLabs: a Voice Library voice refused on a free plan is retried with a default voice, never read as "needs payment"', async () => {
  const { adapter, spoken } = elevenLabs({ refuse: (voiceId) => voiceId === 'LibraryVoice00000001' });
  const result = await adapter.speech({ input: 'Hello there.', voice: 'LibraryVoice00000001' }, { modelId: 'eleven_flash_v2_5' });
  assert.ok(result.audio.length > 0);
  assert.deepEqual(spoken, ['LibraryVoice00000001', 'DefaultFemale0000001']);
  const stubborn = elevenLabs({ refuse: () => true });
  await assert.rejects(stubborn.adapter.speech({ input: 'Hi', voice: 'LibraryVoice00000001' }, { modelId: 'eleven_flash_v2_5' }), (error) => error.kind === KINDS.BAD_REQUEST);
});

test('Groq\'s Arabic Orpheus gets an Arabic voice, the English one an English voice', async () => {
  const adapter = adapterFor('groq');
  const voices = [];
  adapter.request = async (url, { body }) => {
    voices.push(body.voice);
    return { arrayBuffer: async () => new TextEncoder().encode('audio').buffer, headers: new Headers({ 'content-type': 'audio/wav' }) };
  };
  await adapter.speech({ input: 'Hi', voice: 'alloy' }, { modelId: 'canopylabs/orpheus-arabic-saudi' });
  await adapter.speech({ input: 'Hi', voice: 'alloy' }, { modelId: 'canopylabs/orpheus-v1-english' });
  await adapter.speech({ input: 'Hi', voice: 'noura' }, { modelId: 'canopylabs/orpheus-arabic-saudi' });
  assert.deepEqual(voices, ['fahad', 'troy', 'noura']);
});

test('a media request refused for its own options leaves the model without a cooldown', () => {
  const learning = require('../../server/services/routing/learning');
  const stats = require('../../server/services/routing/stats_store');
  const { getDatabase } = require('../../server/db/database');
  require('../../server/db/migrate').migrate();
  const db = getDatabase();
  db.prepare("INSERT OR IGNORE INTO users (id, username, email, password_hash, role) VALUES ('mv', 'mv', 'mv@x.test', 'x', 'admin')").run();
  db.prepare("INSERT OR IGNORE INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode) VALUES ('mva', 'mv', 'cloudflare', 'cf', 'CF', 'free_tier')").run();
  const ref = db.prepare("INSERT INTO models (account_id, model_id, canonical_id, capability) VALUES ('mva', 'aura', 'aura', 'speech')").run().lastInsertRowid;
  const entry = { ref, accountId: 'mva', id: 'cf/aura' };
  const refused = new ProviderError(KINDS.BAD_REQUEST, 'enum Kore not in amalthea, ...');
  for (let index = 0; index < 3; index += 1) learning.onFailure(entry, refused, { requestSpecific: true });
  assert.equal(stats.availability(ref).available, true);
});

test('the no-model message names models NeoRouter cannot call, missing ones and payment refusals apart', () => {
  assert.match(explainNoModel({ unsupported: 1 }, 'speech'), /cannot be used for this through NeoRouter yet/);
  assert.match(explainNoModel({ not_available: 1 }, 'speech'), /listed but not served/);
  assert.match(explainNoModel({ payment_required: 2 }, 'speech'), /payment or plan reasons/);
  assert.match(explainNoModel({ price_unknown: 3 }, 'speech'), /no known price/);
  assert.doesNotMatch(explainNoModel({ price_unknown: 3 }, 'speech'), /not allowed for this key/);
});

test('OVH speaks with its Riva models: one host per model, a voice derived from the model language', async () => {
  const adapter = adapterFor('ovh');
  assert.equal(adapter.supports('speech'), true);
  assert.equal(adapter.supports('transcribe'), true);
  assert.equal(require('../../server/core/capability_registry').methodFor('speech'), 'speech');
  assert.deepEqual((await adapter.voices({ modelId: 'nvr-tts-en-us' })).map((voice) => voice.id), ['English-US.Female-1', 'English-US.Male-1']);
  const calls = [];
  adapter.request = async (url, { body }) => {
    calls.push({ url, body });
    // German has no female voice on OVHcloud: Riva names the voice it does not know.
    if (body.voice_name === 'German-DE-Female-1') throw new ProviderError(KINDS.BAD_REQUEST, 'Model is not available on server: Voice German-DE-Female-1 for language de-DE not found.');
    return { arrayBuffer: async () => new TextEncoder().encode('RIFF....WAVE').buffer, headers: new Headers({ 'content-type': 'application/octet-stream' }) };
  };
  const first = await adapter.speech({ input: 'Hallo.', voice: 'alloy' }, { modelId: 'nvr-tts-de-de' });
  assert.equal(first.mime, 'audio/wav');
  assert.equal(calls[0].url, 'https://nvr-tts-de-de.endpoints.kepler.ai.cloud.ovh.net/api/v1/tts/text_to_audio');
  assert.deepEqual(calls[0].body, { encoding: 1, language_code: 'de-DE', sample_rate_hz: 16000, text: 'Hallo.', voice_name: 'German-DE-Female-1' });
  assert.equal(calls[1].body.voice_name, 'German-DE-Male-1');
  await adapter.speech({ input: 'Noch einmal.' }, { modelId: 'nvr-tts-de-de' });
  assert.equal(calls[2].body.voice_name, 'German-DE-Male-1', 'the voice that worked is remembered');
  await adapter.speech({ input: 'Hi.', voice: 'English-US.Male-1' }, { modelId: 'nvr-tts-en-us' });
  assert.equal(calls[3].body.voice_name, 'English-US.Male-1', 'a Riva voice name is passed on');
});

const { parseWav, wavHeader } = require('../../server/providers/adapters/wav');
const wav = (samples) => Buffer.concat([wavHeader({ channels: 1, sampleRate: 24000, bitsPerSample: 16 }, samples.length), samples]);

test('Groq Orpheus: text over 200 characters is spoken in sentence-sized parts joined into one WAV', async () => {
  const adapter = adapterFor('groq');
  const inputs = [];
  adapter.request = async (url, { body }) => {
    inputs.push(body.input);
    return { arrayBuffer: async () => { const bytes = wav(Buffer.from([inputs.length, inputs.length])); return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.length); }, headers: new Headers({ 'content-type': 'audio/wav' }) };
  };
  const text = `${'This sentence has a few words in it. '.repeat(8)}${'x'.repeat(250)}.`;
  const result = await adapter.speech({ input: text, voice: 'troy' }, { modelId: 'canopylabs/orpheus-v1-english' });
  assert.ok(inputs.length > 2);
  assert.ok(inputs.every((piece) => piece.length <= 200), 'every part fits the limit');
  assert.equal(inputs.join(' ').replace(/\s+/g, ''), text.replace(/\s+/g, ''), 'no text is lost');
  const joined = parseWav(result.audio);
  assert.equal(result.mime, 'audio/wav');
  assert.equal(joined.fmt.sampleRate, 24000);
  assert.deepEqual([...joined.data], inputs.flatMap((_piece, index) => [index + 1, index + 1]));
  assert.equal(result.audio.readUInt32LE(4), result.audio.length - 8, 'the RIFF size covers the joined file');
  // Short text stays one request.
  inputs.length = 0;
  await adapter.speech({ input: 'Hi.', voice: 'troy' }, { modelId: 'canopylabs/orpheus-v1-english' });
  assert.deepEqual(inputs, ['Hi.']);
});

function openRouter(refuse = () => false) {
  // A base URL of its own, so the cached voice listing is this test's.
  const adapter = registry.createAdapter(registry.get('openrouter'), { secret: 'key', baseUrl: `https://or-${Math.random()}.test/api/v1`, options: {} });
  const sent = [];
  adapter.http = async (path) => {
    assert.equal(path, '/models?output_modalities=speech');
    return { data: { data: [
      { id: 'heygen/heygen-voice-1-turbo:free', supported_voices: ['nadine', 'tabitha'] },
      { id: 'google/gemini-3.8-flash-tts', supported_voices: ['Zephyr', 'Kore'] },
      { id: 'fish-audio/s2.1-pro-free:free', supported_voices: null },
    ] } };
  };
  adapter.request = async (url, { body }) => {
    sent.push(body);
    if (refuse(body)) throw new ProviderError(KINDS.BAD_REQUEST, 'This model only returns pcm.');
    const type = body.response_format === 'pcm' ? 'audio/pcm' : 'audio/mpeg';
    return { arrayBuffer: async () => new Uint8Array([1, 0, 2, 0]).buffer, headers: new Headers({ 'content-type': type }) };
  };
  return { adapter, sent };
}

test('OpenRouter speech takes each model\'s voices from its listing; unknown voices get the first one', async () => {
  const { adapter, sent } = openRouter();
  await adapter.speech({ input: 'Hi', voice: 'alloy', format: 'pcm' }, { modelId: 'heygen/heygen-voice-1-turbo:free' });
  await adapter.speech({ input: 'Hi', voice: 'kore', format: 'pcm' }, { modelId: 'google/gemini-3.8-flash-tts' });
  await adapter.speech({ input: 'Hi', format: 'pcm' }, { modelId: 'fish-audio/s2.1-pro-free:free' });
  assert.deepEqual(sent.map((body) => body.voice), ['nadine', 'Kore', undefined]);
  assert.ok(!('voice' in sent[2]), 'no OpenAI voice name for a model without a voice list');
  assert.deepEqual((await adapter.voices({ modelId: 'google/gemini-3.8-flash-tts' })).map((voice) => voice.id), ['Zephyr', 'Kore']);
});

test('OpenRouter PCM comes back as WAV, and a format the model refuses is retried with the other one', async () => {
  const { adapter, sent } = openRouter((body) => body.response_format === 'mp3');
  const result = await adapter.speech({ input: 'Hi', voice: 'nadine', format: 'mp3' }, { modelId: 'heygen/heygen-voice-1-turbo:free' });
  assert.deepEqual(sent.map((body) => body.response_format), ['mp3', 'pcm']);
  assert.equal(result.mime, 'audio/wav');
  const parsed = parseWav(result.audio);
  assert.equal(parsed.fmt.sampleRate, 24000);
  assert.deepEqual([...parsed.data], [1, 0, 2, 0]);
  const raw = await adapter.speech({ input: 'Hi', voice: 'nadine', format: 'pcm' }, { modelId: 'heygen/heygen-voice-1-turbo:free' });
  assert.equal(raw.mime, 'audio/pcm', 'PCM asked for is passed on as it is');
  const wavRequest = await openRouter().adapter.speech({ input: 'Hi', voice: 'nadine', format: 'wav' }, { modelId: 'heygen/heygen-voice-1-turbo:free' });
  assert.equal(wavRequest.mime, 'audio/wav');
});

test('Pollinations lists chat, image and embedding models, with paid_only ones paid', async () => {
  const adapter = adapterFor('pollinations');
  assert.equal(adapter.supports('generateImage'), true);
  assert.equal(adapter.supports('embed'), true);
  adapter.http = async (path) => {
    if (path === '/models') return { data: { data: [{ id: 'openai/gpt-5.4-nano', category: 'text', output_modalities: ['text'], paid_only: false }, { id: 'tongyi-mai/z-image-turbo', category: 'image', paid_only: false }] } };
    if (path.endsWith('/image/models')) return { data: [{ name: 'tongyi-mai/z-image-turbo', category: 'image', paid_only: false }, { name: 'lykon/dreamshaper-8-lcm', category: 'image', paid_only: false }, { name: 'openai/gpt-image-2', category: 'image', paid_only: true }, { name: 'alibaba/wan-2.7', category: 'video', paid_only: true }] };
    if (path.endsWith('/embeddings/models')) return { data: [{ name: 'openai/text-embedding-3-small', category: 'embedding', paid_only: false }, { name: 'qwen/qwen3-embedding-8b', category: 'embedding', paid_only: true }] };
    throw new Error(`unexpected ${path}`);
  };
  const models = Object.fromEntries((await adapter.listModels()).map((model) => [model.id, model]));
  assert.equal(models['openai/gpt-5.4-nano'].kind, 'chat');
  assert.equal(models['tongyi-mai/z-image-turbo'].kind, 'image');
  assert.equal(models['lykon/dreamshaper-8-lcm'].kind, 'image', 'the listing\'s category decides, not the name');
  assert.equal(models['alibaba/wan-2.7'].kind, 'video');
  assert.equal(models['openai/text-embedding-3-small'].kind, 'embedding');
  assert.equal(models['openai/gpt-image-2'].paidOnly, true);
  assert.equal(models['qwen/qwen3-embedding-8b'].paidOnly, true);
  assert.equal(models['openai/text-embedding-3-small'].paidOnly, false);
});

test('Pollinations images and embeddings use its OpenAI-compatible endpoints', async () => {
  const adapter = adapterFor('pollinations');
  const calls = [];
  adapter.http = async (path, { body }) => {
    calls.push({ path, body });
    if (path === '/images/generations') return { data: { data: [{ b64_json: 'aW1n' }] } };
    return { data: { data: [{ index: 0, embedding: [0.1, 0.2] }], usage: { prompt_tokens: 2 } } };
  };
  const image = await adapter.generateImage({ prompt: 'a cat', size: '512x512' }, { modelId: 'tongyi-mai/z-image-turbo' });
  assert.equal(image.images[0].b64, 'aW1n');
  assert.deepEqual(calls[0].body, { model: 'tongyi-mai/z-image-turbo', prompt: 'a cat', n: 1, size: '512x512', response_format: 'b64_json' });
  const vectors = await adapter.embed({ input: ['hi'] }, { modelId: 'openai/text-embedding-3-small' });
  assert.equal(calls[1].path, '/embeddings');
  assert.deepEqual(vectors.vectors, [[0.1, 0.2]]);
});

test('Cloudflare Nova-3 gets the raw audio with its content type and returns the Deepgram transcript', async () => {
  const adapter = adapterFor('cloudflare', { accountId: 'a'.repeat(32) });
  const sent = [];
  adapter.request = async (url, { body, headers }) => {
    sent.push({ url, body, headers });
    return new Response(JSON.stringify({ success: true, result: { results: { channels: [{ alternatives: [{ transcript: 'Hello there.', words: [] }] }] } } }), { headers: { 'content-type': 'application/json' } });
  };
  const result = await adapter.transcribe({ file: Buffer.from('mp3-bytes'), mime: 'audio/mpeg', filename: 'a.mp3' }, { modelId: '@cf/deepgram/nova-3' });
  assert.equal(result.text, 'Hello there.');
  assert.match(sent[0].url, /\/ai\/run\/@cf\/deepgram\/nova-3$/);
  assert.ok(sent[0].body instanceof Uint8Array);
  assert.equal(Buffer.from(sent[0].body).toString(), 'mp3-bytes');
  assert.equal(sent[0].headers['Content-Type'], 'audio/mpeg');
});
