'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const registry = require('../../server/services/processing/shield/registry');
const engine = require('../../server/services/processing/shield/engine');

const built = registry.get();

// Every rule and detector carries its own examples: each "match" text must
// produce a span (with the given value, if one is named), each "ignore" text
// must produce none.
for (const item of [...built.rules, ...built.detectors]) {
  if (item.id === 'ai_entities' || item.id === 'custom_terms') continue;
  test(`${item.id} finds its examples and nothing else`, () => {
    for (const example of item.tests.match) {
      const text = typeof example === 'string' ? example : example.text;
      const values = engine.scanOne(item, text).map((span) => span.value);
      assert.ok(values.length, `${item.id} should match: ${JSON.stringify(text)}`);
      if (typeof example !== 'string') assert.ok(values.includes(example.value), `${item.id} found ${JSON.stringify(values)} in ${JSON.stringify(text)}, expected ${JSON.stringify(example.value)}`);
    }
    for (const text of item.tests.ignore) {
      assert.deepEqual(engine.scanOne(item, text).map((span) => span.value), [], `${item.id} should ignore: ${JSON.stringify(text)}`);
    }
  });
}

test('the main packs are covered by tests', () => {
  const untested = built.rules.filter((rule) => rule.file !== 'secrets_gitleaks.json' && !rule.tests.match.length);
  assert.deepEqual(untested.map((rule) => rule.id), []);
});

function packDir(files) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'shield-'));
  for (const [name, content] of Object.entries(files)) fs.writeFileSync(path.join(directory, name), content);
  return directory;
}

test('broken packs fail at load time', () => {
  const detectorsDir = packDir({});
  const rule = (id) => `{ id: '${id}', label: 'X', category: 'email', pattern: /x/ }`;
  assert.throws(() => registry.build({ rulesDir: packDir({ 'a.js': `module.exports = [${rule('same')}, ${rule('same')}];` }), detectorsDir }), /Duplicate shield rule id same/);
  assert.throws(() => registry.build({ rulesDir: packDir({ 'a.js': "module.exports = [{ id: 'x', label: 'X', category: 'nope', pattern: /x/ }];" }), detectorsDir }), /Invalid shield rule/);
  assert.throws(() => registry.build({ rulesDir: packDir({ 'a.js': "module.exports = [{ id: 'x', label: 'X', category: 'email', pattern: /x/, validator: 'nope' }];" }), detectorsDir }), /Invalid shield rule/);
  assert.throws(() => registry.build({ rulesDir: packDir({ 'a.json': JSON.stringify({ rules: [{ id: 'x', label: 'X', category: 'email', pattern: '(' }] }) }), detectorsDir }), /invalid pattern/);
});
