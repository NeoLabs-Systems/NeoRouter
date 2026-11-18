'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { buildBundledWebClientIfPossible } = require('../../lib/install_helpers');

function workspace() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'nr-install-'));
  const appDir = path.join(root, 'flutter_app');
  fs.mkdirSync(appDir);
  fs.writeFileSync(path.join(appDir, 'pubspec.yaml'), 'name: x\n');
  return { root, appDir, webDir: path.join(appDir, 'build', 'web'), toolsDir: path.join(root, 'tools') };
}

function writeClient(dir, marker) {
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, 'index.html'), marker);
  fs.writeFileSync(path.join(dir, 'main.dart.js'), '//');
}

// Commands as a real machine would answer them, minus the network.
// directDown: GitHub's download link answers 504; its API still serves the file.
function commands({ root, prebuilt, directDown = false }) {
  const calls = [];
  const run = (command, args, options = {}) => {
    calls.push([path.basename(command), ...args]);
    if (command === 'curl') {
      if (!prebuilt) return { status: 22 };
      if (directDown && args.at(-1).includes('/releases/download/')) return { status: 22 };
      if (args.at(-1).includes('/releases/tags/')) return { status: 0, stdout: JSON.stringify({ assets: [{ name: 'neorouter-web.tar.gz', url: 'https://api.github.com/repos/x/y/releases/assets/1' }] }) };
      const source = path.join(root, 'prebuilt');
      writeClient(source, 'prebuilt');
      return spawnSync('tar', ['-czf', args[args.indexOf('-o') + 1], '-C', source, '.']);
    }
    if (command === 'tar') return spawnSync('tar', args);
    if (command === 'git') {
      const target = args.at(-1);
      fs.mkdirSync(path.join(target, 'bin'), { recursive: true });
      fs.writeFileSync(path.join(target, 'bin', 'flutter'), '');
      return { status: 0 };
    }
    if (path.basename(command) === 'flutter') {
      if (args[0] === 'build') writeClient(args[args.indexOf('--output') + 1], 'built');
      return { status: 0 };
    }
    return { status: 1 };
  };
  return { run, calls };
}

const fail = (message) => { throw new Error(message); };

test('without Flutter the prebuilt web client is downloaded', () => {
  const ws = workspace();
  const { run, calls } = commands({ root: ws.root, prebuilt: true });
  const built = buildBundledWebClientIfPossible({ flutterAppDir: ws.appDir, webClientDir: ws.webDir, runCommand: run, commandExistsFn: () => false, channel: 'beta', toolsDir: ws.toolsDir, fail });
  assert.equal(built, true);
  assert.equal(fs.readFileSync(path.join(ws.webDir, 'index.html'), 'utf8'), 'prebuilt');
  assert.ok(calls.some((call) => call[0] === 'curl' && call.at(-1).endsWith('/web-beta/neorouter-web.tar.gz')));
  assert.ok(!calls.some((call) => call[0] === 'git'), 'Flutter is not installed when the download works');
});

test('without Flutter or a prebuilt client, Flutter is installed into the tools folder and builds it', () => {
  const ws = workspace();
  const { run, calls } = commands({ root: ws.root, prebuilt: false });
  const built = buildBundledWebClientIfPossible({ flutterAppDir: ws.appDir, webClientDir: ws.webDir, runCommand: run, commandExistsFn: () => false, toolsDir: ws.toolsDir, fail });
  assert.equal(built, true);
  assert.equal(fs.readFileSync(path.join(ws.webDir, 'index.html'), 'utf8'), 'built');
  assert.ok(calls.some((call) => call[0] === 'git' && call.includes('stable')));
  assert.ok(fs.existsSync(path.join(ws.toolsDir, 'flutter', 'bin', 'flutter')), 'kept for the next update');
});

test('with Flutter installed the client is built locally, nothing is downloaded', () => {
  const ws = workspace();
  const { run, calls } = commands({ root: ws.root, prebuilt: true });
  const built = buildBundledWebClientIfPossible({ flutterAppDir: ws.appDir, webClientDir: ws.webDir, runCommand: run, commandExistsFn: (cmd) => cmd === 'flutter', toolsDir: ws.toolsDir, fail });
  assert.equal(built, true);
  assert.equal(fs.readFileSync(path.join(ws.webDir, 'index.html'), 'utf8'), 'built');
  assert.ok(!calls.some((call) => call[0] === 'curl'));
});

test('an install without the app sources (npm) downloads the prebuilt web client', () => {
  const ws = workspace();
  fs.rmSync(path.join(ws.appDir, 'pubspec.yaml'));
  const { run } = commands({ root: ws.root, prebuilt: true });
  const built = buildBundledWebClientIfPossible({ flutterAppDir: ws.appDir, webClientDir: ws.webDir, runCommand: run, commandExistsFn: () => false, toolsDir: ws.toolsDir, fail, required: true });
  assert.equal(built, true);
  assert.equal(fs.readFileSync(path.join(ws.webDir, 'index.html'), 'utf8'), 'prebuilt');
});

function buildWith(version) {
  const ws = workspace();
  const { run, calls } = commands({ root: ws.root, prebuilt: false });
  const upgrades = [];
  buildBundledWebClientIfPossible({
    flutterAppDir: ws.appDir,
    webClientDir: ws.webDir,
    runCommand: (command, args, options) => (args[0] === '--version'
      ? { status: 0, stdout: `Welcome\n{"frameworkVersion":"${version}"}` }
      : run(command, args, options)),
    commandExistsFn: (cmd) => cmd === 'flutter',
    onFlutterUpgradeStart: (target) => upgrades.push(target),
    fail,
  });
  return { commands: calls.map((call) => call.slice(1).join(' ')), upgrades };
}

test('an older Flutter is upgraded before the build', () => {
  const { commands: ran, upgrades } = buildWith('3.35.5');
  assert.equal(upgrades.length, 1);
  assert.ok(ran.indexOf('upgrade') < ran.indexOf('pub get'));
});

test('a new enough Flutter is left alone', () => {
  assert.ok(!buildWith('3.47.9').commands.includes('upgrade'));
});

test('a Flutter that reports no version is left alone', () => {
  assert.ok(!buildWith('0.0.0-unknown').commands.includes('upgrade'));
});

test('when GitHub\'s download link fails, the same archive comes through its API', () => {
  const ws = workspace();
  const { run, calls } = commands({ root: ws.root, prebuilt: true, directDown: true });
  const built = buildBundledWebClientIfPossible({ flutterAppDir: ws.appDir, webClientDir: ws.webDir, runCommand: run, commandExistsFn: () => false, channel: 'beta', toolsDir: ws.toolsDir, fail });
  assert.equal(built, true);
  assert.ok(calls.some((call) => call[0] === 'curl' && call.includes('Accept: application/octet-stream') && call.at(-1).endsWith('/releases/assets/1')));
  assert.equal(fs.readFileSync(path.join(ws.webDir, 'index.html'), 'utf8'), 'prebuilt');
});
