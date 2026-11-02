'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { statusChecks } = require('../../lib/cli/status');
const { createUpdateGit } = require('../../lib/cli/updates');
const ui = require('../../lib/cli/ui');

const CLI = path.join(__dirname, '../../bin/neorouter.js');

function neorouter(t, ...args) {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-cli-'));
  t.after(() => fs.rmSync(home, { recursive: true, force: true }));
  return spawnSync(process.execPath, [CLI, ...args], {
    encoding: 'utf8',
    env: { ...process.env, NEOROUTER_HOME: home, NEOROUTER_PORT: '1' },
  });
}

test('help groups everyday commands and --all lists the rest', (t) => {
  const everyday = neorouter(t, 'help').stdout;
  assert.match(everyday, /When something's wrong/);
  assert.match(everyday, /neorouter doctor/);
  assert.doesNotMatch(everyday, /reset-password/);
  assert.match(neorouter(t, 'help', '--all').stdout, /neorouter reset-password/);
});

test('output for scripts stays bare', (t) => {
  assert.equal(neorouter(t, 'env', 'get', 'NEOROUTER_HOST').stdout.trim().split('\n').length, 1);
  assert.equal(neorouter(t, 'version').stdout.trim().split('\n').length, 1);
});

test('a mistyped command points at help and fails', (t) => {
  const result = neorouter(t, 'nope');
  assert.equal(result.status, 1);
  assert.match(result.stderr, /no neorouter nope command/);
  assert.match(result.stderr, /Try neorouter help/);
  assert.doesNotMatch(result.stderr, /Technical details/);
});

test('status says what to run for each problem', () => {
  const facts = {
    running: true,
    url: 'http://localhost:4600/app',
    version: '1.2.0',
    runningVersion: '1.1.0',
    serviceActive: false,
    configPresent: true,
    webClientPresent: true,
    update: { version: '1.3.0' },
  };
  const fixes = statusChecks(facts).map((check) => check.fix).filter(Boolean);
  assert.deepEqual(fixes, ['neorouter restart', 'neorouter install', 'neorouter update']);
  const inContainer = statusChecks({ ...facts, inContainer: true });
  assert.ok(!inContainer.some((check) => check.fix === 'neorouter install'));
});

test('a long line is cut to the window, never wrapped', () => {
  assert.equal(ui.visibleLength(ui.truncate('x'.repeat(100), 20)), 20);
  assert.equal(ui.truncate('short', 20), 'short');
});

test('an update is only offered when the branch has something newer', () => {
  const answers = (ahead, remote) => (cmd, args) => {
    const joined = args.join(' ');
    let stdout = '';
    if (joined.startsWith('rev-list')) stdout = String(ahead);
    else if (joined === 'rev-parse --abbrev-ref HEAD') stdout = 'beta';
    else if (joined === 'show origin/beta:package.json') stdout = JSON.stringify({ version: remote });
    else if (joined === 'show HEAD:package.json') stdout = JSON.stringify({ version: '1.0.0' });
    return { status: 0, stdout };
  };
  assert.equal(createUpdateGit(answers(0, '1.0.0')).pendingUpdate('beta'), null);
  assert.equal(createUpdateGit(answers(2, '0.9.0')).pendingUpdate('beta'), null);
  assert.equal(createUpdateGit(answers(2, '0.9.0')).pendingUpdate('beta', { allowOlder: true }).version, '0.9.0');
  assert.equal(createUpdateGit(answers(2, '1.0.0')).pendingUpdate('beta').version, '1.0.0');
});
