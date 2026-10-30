'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { detect, Vault, redactRequest, restoreResponse, StreamRestorer, DEFAULT_MODES } = require('../../server/services/processing/redaction');
const { compress } = require('../../server/services/processing/compression');

test.before(() => require('../../server/db/migrate').migrate());

const modes = { ...DEFAULT_MODES };
const categories = (text, options = {}) => detect(text, { modes, ...options }).map((span) => `${span.category}:${span.value}`);

test('detectors find real sensitive values and validate them', () => {
  assert.deepEqual(categories('Mail anna.schmidt@example.de now'), ['email:anna.schmidt@example.de']);
  assert.deepEqual(categories('IBAN DE89 3704 0044 0532 0130 00 ok'), ['iban:DE89 3704 0044 0532 0130 00']);
  assert.deepEqual(categories('IBAN DE89 3704 0044 0532 0130 01'), [], 'a wrong checksum is not an IBAN');
  assert.deepEqual(categories('card 4111 1111 1111 1111'), ['card:4111 1111 1111 1111']);
  assert.deepEqual(categories('order 4111 1111 1111 1112'), [], 'fails the Luhn check');
  assert.deepEqual(categories('call +49 170 1234567 today'), ['phone:+49 170 1234567']);
  assert.deepEqual(categories('in 2026 we sold 1500 units'), []);
  assert.deepEqual(categories('key sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789'), ['secret:sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789']);
  assert.deepEqual(categories('password: hunter2pw'), ['secret:hunter2pw']);
  assert.deepEqual(categories('das Server-Passwort: hunter2pw.'), ['secret:hunter2pw'], 'German keywords count too');
  assert.deepEqual(categories('Mein Kennwort lautet Sommer2026'), ['secret:Sommer2026']);
  assert.deepEqual(categories('the pin is 4711'), ['secret:4711']);
  assert.deepEqual(categories('the topic is passwords in general'), [], 'the word alone is not a secret');
  assert.deepEqual(categories('postgres://app:s3cr3tpass@db.local/x'), ['secret:s3cr3tpass']);
  assert.deepEqual(categories('commit 9fceb02d0ae598e95dc970b74767f19372d61af8'), [], 'git hashes are not secrets');
  assert.deepEqual(categories('server 10.0.4.17 down'), ['ip:10.0.4.17']);
  assert.deepEqual(categories('Grüße von Acme Holding', { customTerms: ['Acme Holding'] }), ['custom:Acme Holding']);
});

test('the same value gets the same placeholder and restores; hidden values stay hidden', () => {
  const vault = new Vault(modes);
  const text = vault.redactText('a@b.de and again a@b.de, card 4111111111111111');
  assert.equal(text, '[EMAIL_1] and again [EMAIL_1], card [CARD_1]');
  assert.equal(vault.restoreText('Write to [EMAIL_1] or [ email 1 ]. Card [CARD_1].'), 'Write to a@b.de or a@b.de. Card [CARD_1].');
});

test('requests get an instruction and responses are restored', () => {
  const result = redactRequest({ messages: [{ role: 'user', content: 'My mail is x@y.org' }] }, { modes });
  assert.equal(result.canonical.messages[0].role, 'system');
  assert.match(result.canonical.messages[0].content, /placeholders/);
  assert.equal(result.canonical.messages[1].content, 'My mail is [EMAIL_1]');
  const restored = restoreResponse({ choices: [{ message: { role: 'assistant', content: 'Sent to [EMAIL_1]' } }] }, result.vault);
  assert.equal(restored.choices[0].message.content, 'Sent to x@y.org');
  assert.equal(redactRequest({ messages: [{ role: 'user', content: 'nothing private' }] }, { modes }), null);
});

test('placeholders split across stream chunks are restored', () => {
  const vault = new Vault(modes);
  vault.redactText('x@y.org');
  const restorer = new StreamRestorer(vault);
  const pieces = ['Hi [EM', 'AIL', '_1', '] and [', 'unrelated] text'];
  let out = '';
  for (const piece of pieces) out += restorer.process({ choices: [{ index: 0, delta: { content: piece } }] }).choices[0].delta.content;
  out += restorer.process({ choices: [{ index: 0, delta: {}, finish_reason: 'stop' }] }).choices[0].delta.content || '';
  assert.equal(out, 'Hi x@y.org and [unrelated] text');
});

test('the privacy filter adds what it finds, only once an admin turned it on', async () => {
  const privacyModel = require('../../server/services/processing/privacy_model');
  const processing = require('../../server/services/processing/processing_service');
  const fake = Object.assign(async (text) => (text.includes('Johanna Krause') ? [{ entity_group: 'private_person', word: ' Johanna Krause', score: 0.98 }, { entity_group: 'private_person', word: 'Made Up', score: 0.9 }] : []), { dispose: async () => {} });
  privacyModel.setLibraryForTests(async () => ({ env: {}, pipeline: async () => fake }));
  const settings = { ...processing.DEFAULT_SETTINGS, privacy: { ...processing.DEFAULT_SETTINGS.privacy, enabled: true, ai: { enabled: true } } };
  const canonical = { messages: [{ role: 'user', content: 'Lunch with Johanna Krause tomorrow, okay?' }] };
  const off = await processing.apply('u1', canonical, { settings });
  assert.equal(off.stats.aiDetection, false, 'nothing runs until an admin turns it on');
  privacyModel.setEnabled(true);
  try {
    const on = await processing.apply('u1', canonical, { settings });
    assert.equal(on.stats.aiDetection, true);
    const sent = on.canonical.messages.at(-1).content;
    assert.doesNotMatch(sent, /Johanna Krause/);
    assert.equal(on.vault.restoreText(sent), canonical.messages[0].content, 'placeholders are put back');
  } finally {
    privacyModel.setEnabled(false);
  }
});

const longTool = (seed) => Array.from({ length: 400 }, (_, i) => `${seed} line ${i} ${'x'.repeat(40)}`).join('\n');

test('lossless compression keeps meaning and leaves short chats alone', () => {
  const canonical = { messages: [
    { role: 'system', content: 'Be brief.' },
    { role: 'tool', content: JSON.stringify({ a: 1, b: [1, 2, 3] }, null, 2) },
    { role: 'user', content: 'Hello   \n\n\n\nworld\n```\ncode   \n\n\n\n```' },
  ] };
  const result = compress(canonical, { level: 'lossless' });
  assert.equal(result.canonical.messages[1].content, '{"a":1,"b":[1,2,3]}');
  assert.equal(result.canonical.messages[2].content, 'Hello\n\nworld\n```\ncode   \n\n\n\n```', 'code blocks are untouched');
  assert.equal(compress(canonical, { level: 'off' }).canonical, canonical);
});

test('balanced and aggressive compression trim old tool output but protect the latest message', () => {
  const messages = [{ role: 'system', content: 'S' }];
  for (let i = 0; i < 5; i += 1) messages.push({ role: 'assistant', content: `step ${i}` }, { role: 'tool', content: longTool(`run${i}`) });
  messages.push({ role: 'user', content: longTool('question') });
  const balanced = compress({ messages }, { level: 'balanced', minTokens: 100 });
  assert.ok(balanced.stats.savedTokens > 0);
  assert.match(balanced.canonical.messages[2].content, /characters omitted/);
  assert.equal(balanced.canonical.messages.at(-1).content, messages.at(-1).content, 'the latest user message is kept whole');
  const aggressive = compress({ messages }, { level: 'aggressive', minTokens: 100 });
  assert.match(aggressive.canonical.messages[2].content, /Older tool output omitted/);
  assert.ok(aggressive.stats.savedTokens > balanced.stats.savedTokens);
  const short = compress({ messages: messages.slice(0, 3) }, { level: 'aggressive', minTokens: 1_000_000 });
  assert.equal(short.stats.engines.maskOldToolOutput, undefined, 'lossy engines wait for the size threshold');
  assert.equal(compress({ messages }, { level: 'aggressive', minTokens: 100 }, { headerLevel: 'lossless' }).stats.level, 'lossless', 'a header can only lower the level');
});
