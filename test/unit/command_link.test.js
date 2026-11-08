'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { binDir, ensureCommandLink, removeCommandLink, commandReachable, pathHint } = require('../../lib/cli/command_link');

// Every test gets its own home, so the real ~/.local/bin is never touched.
const homes = [];
function tempHome() {
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-home-'));
  homes.push(home);
  return home;
}
test.after(() => {
  for (const home of homes) fs.rmSync(home, { recursive: true, force: true });
});
const appDir = path.resolve(__dirname, '../..');
const posix = process.platform !== 'win32';

test('the launcher runs this checkout\'s CLI, and installing again changes nothing', { skip: !posix }, () => {
  const home = tempHome();
  const env = { PATH: `${binDir(home)}${path.delimiter}/usr/bin:/bin`, SHELL: '/bin/zsh' };
  const first = ensureCommandLink({ appDir, home, env });
  assert.equal(first.status, 'created');
  assert.equal(first.hint, null, 'on the PATH already: no hint');
  assert.equal(fs.statSync(first.path).mode & 0o111, 0o111, 'executable');
  const run = spawnSync(first.path, ['version'], { encoding: 'utf8', env: { ...process.env, ...env } });
  assert.equal(run.status, 0, run.stderr);
  assert.match(run.stdout, /\d+\.\d+\.\d+/);
  assert.equal(ensureCommandLink({ appDir, home, env }).status, 'unchanged');
  assert.equal(ensureCommandLink({ appDir: '/elsewhere/NeoRouter', home, env }).status, 'updated', 'a moved checkout is followed');
  assert.ok(commandReachable(env));
});

test('a folder that is not on the PATH comes with a line for the person\'s shell', { skip: !posix }, () => {
  const home = tempHome();
  const link = ensureCommandLink({ appDir, home, env: { PATH: '/usr/bin:/bin', SHELL: '/usr/bin/fish' } });
  assert.equal(link.hint, 'fish_add_path ~/.local/bin');
  assert.equal(pathHint(binDir(home), { shell: '/bin/zsh', home }), `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc`);
  assert.equal(pathHint(binDir(home), { shell: '/bin/bash', home, platform: 'linux' }), `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc`);
  assert.equal(pathHint(binDir(home), { shell: '/bin/bash', home, platform: 'darwin' }), `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bash_profile`);
  assert.equal(pathHint(binDir(home), { shell: '', home }), 'export PATH="$HOME/.local/bin:$PATH"');
  assert.equal(commandReachable({ PATH: '/usr/bin:/bin' }), false);
});

test('someone else\'s neorouter is left alone, and uninstall removes only ours', { skip: !posix }, () => {
  const home = tempHome();
  fs.mkdirSync(binDir(home), { recursive: true });
  const foreign = path.join(binDir(home), 'neorouter');
  fs.writeFileSync(foreign, '#!/bin/sh\necho mine\n', { mode: 0o755 });
  assert.equal(ensureCommandLink({ appDir, home, env: { PATH: '' } }).status, 'foreign');
  assert.equal(removeCommandLink({ home }), false);
  assert.equal(fs.readFileSync(foreign, 'utf8'), '#!/bin/sh\necho mine\n');
  fs.rmSync(foreign);
  ensureCommandLink({ appDir, home, env: { PATH: '' } });
  assert.equal(removeCommandLink({ home }), true);
  assert.equal(fs.existsSync(foreign), false);
});

test('Windows gets no launcher', () => {
  assert.equal(ensureCommandLink({ appDir, home: tempHome(), platform: 'win32' }).status, 'unsupported');
});
