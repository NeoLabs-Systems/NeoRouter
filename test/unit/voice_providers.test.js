'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const express = require('express');
const registry = require('../../server/providers/registry');
const { cues } = require('../../server/providers/adapters/elevenlabs');
const { auraModel } = require('../../server/providers/adapters/deepgram');

let server;
let base;
const seen = [];

test.before(async () => {
  const app = express();
  app.post('/v1/speak', express.json(), (req, res) => {
    seen.push({ path: '/speak', query: req.query, auth: req.get('authorization'), body: req.body });
    res.set('content-type', 'audio/mpeg').send(Buffer.from('mp3-bytes'));
  });
  app.post('/v1/listen', express.raw({ type: '*/*' }), (req, res) => {
    seen.push({ path: '/listen', query: req.query, type: req.get('content-type'), bytes: req.body.length });
    res.json({
      metadata: { duration: 3.3 },
      results: {
        channels: [{ detected_language: 'en', alternatives: [{ transcript: 'Hello from NeoRouter.' }] }],
        utterances: [{ start: 0, end: 1.5, transcript: 'Hello from' }, { start: 1.5, end: 3.3, transcript: 'NeoRouter.' }],
      },
    });
  });
  app.post('/v1/text-to-speech/:voice', express.json(), (req, res) => {
    seen.push({ path: `/tts/${req.params.voice}`, query: req.query, key: req.get('xi-api-key'), body: req.body });
    res.set('content-type', 'audio/mpeg').send(Buffer.from('eleven-mp3'));
  });
  app.post('/v1/speech-to-text', express.raw({ type: 'multipart/form-data' }), async (req, res) => {
    const form = await new Request('http://x/', { method: 'POST', headers: { 'content-type': req.get('content-type') }, body: req.body }).formData();
    seen.push({ path: '/stt', model: form.get('model_id'), granularity: form.get('timestamps_granularity') });
    res.json({ language_code: 'eng', text: 'Hi there. Bye now.', words: [
      { text: 'Hi', start: 0, end: 0.3, type: 'word' }, { text: ' ', start: 0.3, end: 0.35, type: 'spacing' }, { text: 'there.', start: 0.35, end: 0.8, type: 'word' },
      { text: 'Bye', start: 2.5, end: 2.8, type: 'word' }, { text: 'now.', start: 2.8, end: 3.1, type: 'word' },
    ] });
  });
  await new Promise((resolve) => { server = app.listen(0, '127.0.0.1', resolve); });
  base = `http://127.0.0.1:${server.address().port}/v1`;
});

test.after(() => new Promise((resolve) => server.close(resolve)));

const ctx = (modelId) => ({ modelId, signal: AbortSignal.timeout(5000), timeoutMs: 5000 });
const adapter = (id) => registry.createAdapter(registry.get(id), { secret: 'secret-1', baseUrl: base, options: {} });

test('Deepgram speaks with the Aura voice for an OpenAI voice name and the right encoding', async () => {
  const result = await adapter('deepgram').speech({ input: 'Hi', voice: 'nova', format: 'wav' }, ctx('aura-2'));
  assert.equal(result.mime, 'audio/wav');
  const call = seen.find((item) => item.path === '/speak');
  assert.equal(call.auth, 'Token secret-1');
  assert.deepEqual(call.query, { model: 'aura-2-luna-en', encoding: 'linear16', container: 'wav' });
  assert.deepEqual(call.body, { text: 'Hi' });
  assert.equal(auraModel('aura-2', 'aura-2-julius-de'), 'aura-2-julius-de');
  assert.equal(auraModel('aura-2', 'unknown'), 'aura-2-thalia-en');
});

test('Deepgram transcribes raw audio into text, language and segments', async () => {
  const result = await adapter('deepgram').transcribe({ file: Buffer.alloc(64, 1), mime: 'audio/wav' }, ctx('nova-3'));
  assert.equal(result.text, 'Hello from NeoRouter.');
  assert.equal(result.language, 'en');
  assert.equal(result.duration, 3.3);
  assert.equal(result.segments.length, 2);
  const call = seen.find((item) => item.path === '/listen');
  assert.equal(call.type, 'audio/wav');
  assert.equal(call.bytes, 64);
  assert.equal(call.query.detect_language, 'true');
});

test('ElevenLabs maps voices and builds subtitle cues from word timestamps', async () => {
  const speech = await adapter('elevenlabs').speech({ input: 'Hi', voice: 'alloy', format: 'mp3' }, ctx('eleven_multilingual_v2'));
  assert.equal(speech.audio.toString(), 'eleven-mp3');
  const call = seen.find((item) => item.path.startsWith('/tts/'));
  assert.equal(call.path, '/tts/21m00Tcm4TlvDq8ikWAM');
  assert.equal(call.key, 'secret-1');
  assert.equal(call.query.output_format, 'mp3_44100_128');
  assert.equal(call.body.model_id, 'eleven_multilingual_v2');

  const transcript = await adapter('elevenlabs').transcribe({ file: Buffer.alloc(64, 1), filename: 'a.wav', mime: 'audio/wav' }, ctx('scribe_v2'));
  assert.equal(transcript.text, 'Hi there. Bye now.');
  assert.deepEqual(transcript.segments.map((item) => item.text), ['Hi there.', 'Bye now.']);
  assert.equal(transcript.duration, 3.1);
  assert.equal(seen.find((item) => item.path === '/stt').granularity, 'word');
  assert.equal(cues([]).length, 0);
});

test('neither voice provider is offered for speech translation', () => {
  assert.equal(adapter('deepgram').transcribeTranslate, false);
  assert.equal(adapter('elevenlabs').transcribeTranslate, false);
});
