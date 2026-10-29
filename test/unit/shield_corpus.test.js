'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { detect, DEFAULT_MODES, redactRequest } = require('../../server/services/processing/redaction');
const engine = require('../../server/services/processing/shield/engine');

const modes = { ...DEFAULT_MODES };
const found = (text) => detect(text, { modes }).map((span) => `${span.category}:${span.value}`);

// Everyday text that holds nothing private. Every hit here is a false alarm.
const BENIGN = [
  'function add(a, b) {\n  return a + b;\n}\nconst total = items.reduce((sum, item) => sum + item.price, 0);',
  'commit 9fceb02d0ae598e95dc970b74767f19372d61af8\nAuthor: build bot\nDate: Tue Oct 28 14:02:11 2026 +0100\n\n    Fix the release script',
  'request id 3f2b8c1e-9d4a-4f6b-8e2a-1c7d5b9a0e3f took 120 ms',
  'sha256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
  'The meeting is on 12.03.2026 at 14:30, room 4.12.',
  'Upgrade from version 2.14.3 to 3.0.1 before 2026-12-01.',
  'Total: 1.234,56 EUR, paid 99,90 € and 15 % discount. Order 4711-0815.',
  'Colors: #ff8800, #00ccaa and rgb(12, 34, 56).',
  'Max Bescheid geben, dass die Tabelle morgen fertig ist.',
  'Ich habe heute Paul getroffen und wir haben über den Urlaub gesprochen.',
  'Hallo Anna, wie geht es dir? Liebe Grüße',
  'Will you mark the rose as done? Grace period ends in May.',
  'Im Jahr 2026 haben wir 1500 Einheiten verkauft, Kapitel 3 erklärt warum.',
  'Der Platz 3 ging an Team Blau, Platz 4 an Team Rot.',
  'Error code 5003 occurred on line 42, port 8080.',
  'The topic is passwords in general and how to store them safely.',
  'Set API_KEY=${API_KEY} and TOKEN=<your-token> in .env.example.',
  'Weights 0.1234, 0.5678 and 0.9012 sum up.',
  'Listen on 127.0.0.1:3000 and 0.0.0.0.',
  'std::vector<int> values; values.push_back(10);',
  'Bitte die Rechnung bis Freitag bezahlen und den Vertrag unterschreiben.',
];

test('everyday text gives no false alarms', () => {
  for (const text of BENIGN) assert.deepEqual(found(text), [], text);
});

test('a German letter: first names stay, everything identifying is hidden', () => {
  const letter = [
    'Sehr geehrte Frau Schneider,',
    'mein Name ist Jonas Becker, geb. 14.07.1988, wohnhaft Lindenstraße 12, 50674 Köln.',
    'Meine Steuer-ID ist 86 095 742 719, die USt-IdNr. DE136695976.',
    'Rufen Sie mich unter +49 170 1234567 an.',
    'Überweisen Sie bitte auf DE89 3704 0044 0532 0130 00.',
    'Mein Auto (Kennzeichen K-JB 1234) steht bei 50.9375, 6.9603 laut GPS.',
    'Viele Grüße',
    'Jonas Becker',
  ].join('\n');
  const result = redactRequest({ messages: [{ role: 'user', content: letter }] }, { modes });
  const sent = result.canonical.messages.at(-1).content;
  for (const hidden of ['Schneider', 'Becker', '+49 170', '14.07.1988', 'Lindenstraße 12', '86 095 742 719', 'DE136695976', 'DE89 3704', 'K-JB 1234', '50.9375']) {
    assert.ok(!sent.includes(hidden), `${hidden} should be hidden:\n${sent}`);
  }
  assert.match(sent, /^Sehr geehrte Frau \[LASTNAME_1\],/);
  assert.match(sent, /mein Name ist Jonas \[LASTNAME_2\]/, 'the first name stays');
  assert.match(sent, /\nJonas \[LASTNAME_2\]$/, 'the same name gets the same placeholder');
  assert.deepEqual(Object.keys(result.vault.counts).sort(), ['address', 'birthdate', 'iban', 'id', 'location', 'person', 'phone', 'vehicle']);
});

test('an English message with IDs, wallets and secrets', () => {
  const text = [
    'Hi, this is Mike Thompson (SSN 078-05-1120).',
    'Passport: L898902C36UTO7408122F1204159ZE184226B<<<<<10',
    'Send the refund to bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq.',
    'Recovery: abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about',
    'VIN 1HGCM82633A004352, we meet at 52.5200° N, 13.4050° E.',
    'DB_PASSWORD=Zx9!kq2Lm',
  ].join('\n');
  const categories = new Set(detect(text, { modes }).map((span) => span.category));
  for (const category of ['person', 'id', 'crypto', 'secret', 'vehicle', 'location']) assert.ok(categories.has(category), `${category} in ${[...categories]}`);
  assert.ok(!found(text).includes('person:Mike'), 'first names stay');
});

test('allow terms keep values in the text', () => {
  const text = 'Write to support@acme.example or call +49 170 1234567';
  assert.deepEqual(detect(text, { modes, allowTerms: ['@acme.example'] }).map((span) => span.category), ['phone']);
  assert.deepEqual(detect(text, { modes, allowTerms: ['+49 170 1234567'] }).map((span) => span.category), ['email']);
});

test('AI names keep their first name too', () => {
  const spans = detect('Termin mit Lena Wojtowicz', { modes, entities: [{ text: 'Lena Wojtowicz', category: 'person' }] });
  assert.deepEqual(spans.map((span) => span.value), ['Wojtowicz']);
});

test('a new message scans in a few milliseconds and repeats come from the cache', () => {
  const block = `${BENIGN.join('\n')}\nMail jana@example.org, IBAN DE89 3704 0044 0532 0130 00, Herr Becker.\n`;
  const text = block.repeat(Math.ceil(20_000 / block.length)).slice(0, 20_000);
  engine.clearCache();
  detect(`${text}warm-up`, { modes });
  let started = process.hrtime.bigint();
  detect(text, { modes });
  const cold = Number(process.hrtime.bigint() - started) / 1e6;
  started = process.hrtime.bigint();
  detect(text, { modes });
  const cached = Number(process.hrtime.bigint() - started) / 1e6;
  console.log(`20k characters: ${cold.toFixed(1)} ms new, ${cached.toFixed(2)} ms cached`);
  assert.ok(cold < 250, `scan took ${cold} ms`);
  assert.ok(cached < cold / 5, `cached scan took ${cached} ms`);
});
