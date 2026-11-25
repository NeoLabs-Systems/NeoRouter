#!/usr/bin/env node
'use strict';

// Builds the Go port's testdata from the JavaScript oracle: the rule and
// detector examples, the spans the JS engine finds in a corpus of texts, and
// the redaction expectations of test/unit/shield_corpus.test.js and
// test/unit/redaction_restore.test.js.
//
//   node internal/processing/gen/gen_shield_testdata.js     (from the repo root)
//
// The Go tests read these files, so a difference between the two engines shows
// up as a failed test instead of a transcription mistake in a hand-written
// fixture.
const fs = require('node:fs');
const path = require('node:path');

const ROOT = path.join(__dirname, '..', '..', '..');
const OUT = path.join(ROOT, 'internal', 'processing', 'testdata');
const registry = require(path.join(ROOT, 'server', 'services', 'processing', 'shield', 'registry'));
const engine = require(path.join(ROOT, 'server', 'services', 'processing', 'shield', 'engine'));
const redaction = require(path.join(ROOT, 'server', 'services', 'processing', 'redaction'));

const built = registry.get();
const modes = { ...redaction.DEFAULT_MODES };

const spansOf = (text, options = {}) => engine.scan(text, { modes, ...options })
  .map((span) => ({ category: span.category, value: span.value, ruleId: span.ruleId }));

// --- 1. every rule's and detector's own examples --------------------------
const items = [...built.rules, ...built.detectors].map((item) => ({
  id: item.id,
  kind: item.kind,
  file: item.file,
  match: item.tests.match.map((example) => (typeof example === 'string' ? { text: example } : { text: example.text, value: example.value })),
  ignore: item.tests.ignore,
}));

// --- 2. the JS corpus test's own texts ------------------------------------
const corpusSource = fs.readFileSync(path.join(ROOT, 'test', 'unit', 'shield_corpus.test.js'), 'utf8');
const benign = eval(corpusSource.match(/const BENIGN = (\[[\s\S]*?\n\]);/)[1]);
const letter = eval(corpusSource.match(/const letter = (\[[\s\S]*?\]\.join\('\\n'\));/)[1]);

// Boundary and real-traffic texts: where a Go rewrite of a regex (RE2 has no
// lookbehind) or a greedy-vs-backtracking difference would show up.
const extras = [
  'call +49 170 1234567 today',
  'call me on 0170/1234567 or 0170-1234567',
  '(030) 1234 5678',
  'Tel. 0049 30 12345678, Fax 0049 30 12345679',
  'order 1234 5678 9012 3456 shipped',
  'Kontonummer 1234567890, BLZ 370 400 44',
  'Steuer-ID: 86 095 742 719 and 86095742719',
  'Ausweisnummer T220001293, Reisepass C01X00T47',
  'L898902C36UTO7408122F1204159ZE184226B<<<<<10',
  'P<UTOERIKSSON<<ANNA<MARIA<<<<<<<<<<<<<<<<<<<',
  'wifi 3C:22:FB:9A:10:7E and 3c-22-fb-9a-10-7e and 0023.ac4f.1b2c',
  'host 2a00:1450:4001:82a::200e answered, meet at 12:30:45, std::vector',
  'listen on 127.0.0.1, 10.0.4.17 and 0.0.0.0',
  'commit 9fceb02d0ae598e95dc970b74767f19372d61af8 and sha256 e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
  'request id 3f2b8c1e-9d4a-4f6b-8e2a-1c7d5b9a0e3f took 120 ms',
  'Total: 1.234,56 EUR, paid 99,90 and 15 % discount, version 2.14.3, 2026-12-01',
  'Am 12.03.1985 geboren, Termin am 12.03.2026, released 2026-10-29',
  'geb. 12.03.1985, Date of birth: March 12, 1985, geboren am 3. Mai 1990',
  'Kennzeichen: M-AB 1234, Auto B XY 99E, Typ M-AB 1234',
  'VIN 1HGCM82633A004352 / WVWZZZ1JZXW000001',
  'Standort: 52.520008, 13.404954; 52.5200° N, 13.4050° E; 52°31\'12" N 13°24\'18" E',
  'https://maps.example/@48.13743,11.57549,15z',
  'meet at 9F4MGC22+22, C++ code',
  'BIC: COBADEFFXXX, IBAN DE89 3704 0044 0532 0130 00, GB82WEST12345698765432',
  'IBAN DE89 3704 0044 0532 0130 01 is wrong',
  'card 4111 1111 1111 1111, 5500-0000-0000-0004, CVV: 123, valid thru 09/28',
  'order 4111 1111 1111 1112, room 123, ratio 09/28',
  'US SSN 078-05-1120, ITIN 912-70-1234, routing number 021000021, ticket 021000021',
  'NI number AB 12 34 56 C, NHS number 943 476 5919, call 943 476 5919',
  'SV-Nummer 1237 010180, AHV 756.1234.5678.97, BSN 111222333, PESEL 44051401359',
  'NIR 1 84 12 76 451 089 46, DNI 12345678Z, NIE X1234567L, CF RSSMRA85T10A562S',
  'USt-IdNr. DE136695976, VAT: FR40303265045, P.IVA IT12345678901, UID ATU12345678',
  '85.07.30-033.28 and NL123456789B01 and CHE-123.456.789',
  'Führerschein B072RRE2I55, Versichertennummer A123456780, RV-Nr. 12 150385 M 01 4',
  'Reisepass C01X00T47, Steuernummer 21/815/08150, personalausweis T220001293',
  'send to 1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa or bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq',
  'wallet 0x742d35cc6634c0532925a3b844bc454e4438f44e and hash 0x742d35cc6634c0532925a3b844bc454e4438f44e',
  '44AFFq5kSiGBoZ4NMDwYtN18obc8AemS33DBLWs3H7otXft3XjrpDtQGv7SqSsaBYBb98uNbr2VBBEt7f2wfn3RVGQBEP3A',
  'Solana wallet 7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV and 7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV',
  'Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456',
  'Authorization: Basic YWxhZGRpbjpvcGVuc2VzYW1l',
  'X-Api-Key: 9f8e7d6c5b4a39281706, Cookie: session=abc123def456; theme=dark',
  'postgres://app:s3cr3tpass@db.local/x and https://example.org/a:b',
  'https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX',
  'https://discord.com/api/webhooks/123456789/abcDEF-ghi',
  'https://bucket.s3.amazonaws.com/f.pdf?X-Amz-Signature=8f2a9c1d7e3b&sv=2022&sig=AbC%2Fdef123',
  'https://example.org/?page=2&sort=newest',
  'Your verification code is 482913. TAN: 553201. error code 5003 occurred, in 2026',
  'The topic is passwords in general. Set API_KEY=${API_KEY} and TOKEN=changeme in .env.example.',
  'DB_PASSWORD=Zx9!kq2Lm and export STRIPE_SECRET_KEY="rk_abc12345"',
  'DefaultEndpointsProtocol=https;AccountName=x;AccountKey=abc123+/def==;',
  'Server=db;User Id=sa;Password=Pa55word!;',
  '{"password": "hunter2pw", "pin": 4711, "token": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYzEXAMPLEKEY9"}',
  'password: hunter2pw\nKennwort lautet Sommer2026\nthe pin is 4711',
  'the temporary password from the onboarding email was RheinTech!2026#Reset.',
  'Das neue Passwort für den Router ist Sommer2026!',
  'the password is required for every login, my password was changed yesterday',
  'Max Bescheid geben, dass die Tabelle morgen fertig ist.',
  'Hallo, ich bin Anna Schmidt. Termin mit Frau Schneider morgen.',
  'Dear Mr. Thompson, Viele Grüße\nMaria Müller-Lüdenscheid',
  'Gespräch mit Richard von Weizsäcker, Teilnehmer: Schmidt, Anna',
  'hallo, ich bin anna schmidt, grüße\njonas becker, herr müller hat angerufen',
  'NAME: ANNA SCHMIDT, ICH BIN DA, will you do it so',
  'Ich wohne in der Hauptstraße 5, 10115 Berlin.',
  'Lieferung an Berliner Straße 12a, Adresse: Am Markt 3',
  'Ship to 1600 Pennsylvania Avenue, Washington, DC 20500',
  'Karl-Marx-Allee 34\n10178 Berlin',
  'Im Jahr 2026, Der Platz 3 ging an uns, 10115 Berlin ist schön, Kapitel 3',
  'my seed: abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about',
  'Please review the budget and approve the final plan for the next quarter today.',
  '1. legal 2. winner 3. thank 4. year 5. wave 6. sausage 7. worth 8. useful 9. legal 10. winner 11. thank 12. yellow',
  'Sehr geehrte Frau Schneider,\nmein Name ist Jonas Becker, geb. 14.07.1988, wohnhaft Lindenstraße 12, 50674 Köln.\nMeine Steuer-ID ist 86 095 742 719, die USt-IdNr. DE136695976.\nRufen Sie mich unter +49 170 1234567 an.\nÜberweisen Sie bitte auf DE89 3704 0044 0532 0130 00.\nMein Auto (Kennzeichen K-JB 1234) steht bei 50.9375, 6.9603 laut GPS.\nViele Grüße\nJonas Becker',
  'Mail jana@example.org about Acme Holding, password: hunter2pw',
  'Hi, this is Mike Thompson (SSN 078-05-1120). Passport: L898902C36UTO7408122F1204159ZE184226B<<<<<10\nSend the refund to bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq.',
  'deploy key sk-or-v1-abcdefghijklmnopqrstuvwxyz0123456789 for jana@example.org',
  corpusSource.includes('nothing private') ? 'nothing private' : 'nothing private',
  'function add(a, b) {\n  return a + b;\n}',
  '```json\n{"a": 1, "b": [1, 2, 3]}\n```',
  '',
  ' ',
  'abc',
  '1234567890123456789012345678901234567890',
  'a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8',
  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
];

const spanCases = [];
const seen = new Set();
for (const text of [...benign, letter, ...extras]) {
  if (seen.has(text)) continue;
  seen.add(text);
  spanCases.push({ text, expected: spansOf(text) });
}

// Options the JS corpus test exercises.
const optionCases = [
  { text: 'Write to support@acme.example or call +49 170 1234567', allowTerms: ['@acme.example'] },
  { text: 'Write to support@acme.example or call +49 170 1234567', allowTerms: ['+49 170 1234567'] },
  { text: 'Termin mit Lena Wojtowicz', entities: [{ text: 'Lena Wojtowicz', category: 'person' }] },
];
for (const item of optionCases) {
  spanCases.push({ text: item.text, allowTerms: item.allowTerms || [], entities: item.entities || [], expected: spansOf(item.text, item) });
}

// --- 3. redaction expectations of the corpus test --------------------------
const letterRedaction = redaction.redactRequest({ messages: [{ role: 'user', content: letter }] }, { modes });
const hidden = letterRedaction.canonical.messages[letterRedaction.canonical.messages.length - 1].content;
const findings = letterRedaction.vault.findings();

const corpus = {
  benign,
  letter: {
    text: letter,
    redacted: hidden,
    counts: Object.keys(letterRedaction.vault.counts).sort(),
    findings: findings.map((finding) => ({ token: finding.token, category: finding.category, label: finding.label })),
  },
};

// --- 4. the vault fixtures of the restore test -----------------------------
const restoreSource = fs.readFileSync(path.join(ROOT, 'test', 'unit', 'redaction_restore.test.js'), 'utf8');
const key = eval(restoreSource.match(/const KEY = ('(?:[^'\\]|\\.)*');/)[1]);
const pem = eval(restoreSource.match(/const PEM = ('(?:[^'\\]|\\.)*');/)[1]);
const write = (name, value) => fs.writeFileSync(path.join(OUT, name), `${JSON.stringify(value, null, 1)}\n`);

write('shield_rules.json', items);
write('span_cases.json', spanCases);
write('corpus.json', corpus);
write('vault.json', { key, pem });

console.log(`shield_rules.json: ${items.length} rules and detectors`);
console.log(`span_cases.json: ${spanCases.length} texts`);
console.log(`corpus.json: ${benign.length} benign texts, letter redaction ${hidden.length} characters, ${findings.length} findings`);
console.log('vault.json: restore fixtures');