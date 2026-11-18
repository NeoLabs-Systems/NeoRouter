'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { redactRequest, restoreResponse, StreamRestorer, Vault, DEFAULT_MODES } = require('../../server/services/processing/redaction');

const modes = { ...DEFAULT_MODES, secret: 'restore' };
const KEY = 'sk-proj-abcdefghijabcdefghij0001';
const PEM = '-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\nyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy\n-----END RSA PRIVATE KEY-----';

function vaultWith(...secrets) {
  const redacted = redactRequest({ messages: [{ role: 'user', content: secrets.join('\n') }] }, { modes });
  assert.equal(redacted.vault.size, secrets.length);
  return redacted.vault;
}

test('a placeholder is found however a model writes it', () => {
  const vault = vaultWith(KEY);
  for (const written of ['[SECRET_1]', '[Secret_1]', '[ SECRET 1 ]', '[SECRET-1]', '\\[SECRET_1\\]', '[SECRET\\_1]', '<SECRET_1>', '{SECRET_1}', '【SECRET_1】', 'SECRET_1', '`[SECRET_1]`', '**[SECRET_1]**']) {
    assert.ok(vault.restoreText(`use ${written}.`).includes(KEY), `${written} is restored`);
    assert.ok(!/SECRET/i.test(vault.restoreText(`use ${written}.`)), `${written} leaves nothing behind`);
  }
});

test('only the vault\'s own tokens are restored, never look-alikes', () => {
  const vault = vaultWith(KEY);
  const text = 'SECRET_2 and MY_SECRET_1 and SECRET_10 and SECRET_1X stay';
  assert.equal(vault.restoreText(text), text);
});

test('values hidden by choice stay hidden', () => {
  const redacted = redactRequest({ messages: [{ role: 'user', content: KEY }] }, { modes: { ...DEFAULT_MODES, secret: 'hide' } });
  assert.equal(redacted.vault.restoreText('[SECRET_1]'), '[SECRET_1]');
});

test('a multi-line secret keeps tool-call arguments valid JSON', () => {
  const vault = vaultWith(KEY, PEM);
  const token = [...vault.byToken].find(([, entry]) => entry.value.includes('PRIVATE'))[0];
  const answer = { choices: [{ message: { role: 'assistant', content: `see [${token}]`, tool_calls: [{ function: { arguments: JSON.stringify({ command: `echo "[${token}]" > key.pem`, key: '[SECRET_1]' }) } }] } }] };
  const restored = restoreResponse(answer, vault).choices[0].message;
  const args = JSON.parse(restored.tool_calls[0].function.arguments);
  assert.equal(args.command, `echo "${PEM}" > key.pem`, 'the private key arrives as written');
  assert.equal(args.key, KEY);
  assert.ok(restored.content.includes(PEM));
});

test('reasoning is restored in a whole answer too', () => {
  const vault = vaultWith(KEY);
  const message = restoreResponse({ choices: [{ message: { content: 'a', reasoning: 'think [SECRET_1]', reasoning_content: 'rc [SECRET_1]' } }] }, vault).choices[0].message;
  assert.equal(message.reasoning, `think ${KEY}`);
  assert.equal(message.reasoning_content, `rc ${KEY}`);
});

test('a placeholder split across stream chunks is restored, in text and in tool arguments', () => {
  const vault = vaultWith(KEY, PEM);
  const token = [...vault.byToken].find(([, entry]) => entry.value.includes('PRIVATE'))[0];
  const chunk = (delta, finish = null) => ({ id: 'x', choices: [{ index: 0, delta, finish_reason: finish }] });
  const characters = (text) => [...text];

  const textRestorer = new StreamRestorer(vault);
  const out = [...characters('Use [SECRET_1] and \\[SECRET\\_1\\] and SECRET_1 now'), ''].map((piece, index, all) => textRestorer.process(chunk({ content: piece }, index === all.length - 1 ? 'stop' : null)));
  assert.equal(out.map((item) => item.choices[0].delta.content || '').join(''), `Use ${KEY} and ${KEY} and ${KEY} now`);

  const argsRestorer = new StreamRestorer(vault);
  const text = JSON.stringify({ c: `[${token}]` });
  const pieces = characters(text).map((piece) => argsRestorer.process(chunk({ tool_calls: [{ index: 0, function: { arguments: piece } }] })));
  pieces.push(argsRestorer.process(chunk({}, 'tool_calls')));
  const joined = pieces.map((item) => (item.choices[0].delta.tool_calls || []).map((call) => call.function.arguments || '').join('')).join('');
  assert.equal(JSON.parse(joined).c, PEM);
});

test('a secret inside tool-call arguments of the conversation is hidden as the person wrote it', () => {
  const message = { role: 'assistant', content: null, tool_calls: [{ id: 'c1', type: 'function', function: { name: 'write', arguments: JSON.stringify({ text: PEM }) } }] };
  const redacted = redactRequest({ messages: [{ role: 'user', content: 'go' }, message] }, { modes });
  assert.ok(redacted, 'found inside the JSON string');
  const sent = redacted.canonical.messages.find((item) => item.role === 'assistant').tool_calls[0].function.arguments;
  assert.ok(!sent.includes('PRIVATE KEY'), 'the key does not leave');
  assert.equal(JSON.parse(sent).text.startsWith('[SECRET_'), true);
});

test('the same value gets the same placeholder in text and in arguments', () => {
  const vault = new Vault(modes);
  assert.equal(vault.tokenFor('secret', KEY), vault.tokenFor('secret', KEY));
});
