'use strict';

const fs = require('node:fs');
const path = require('node:path');

function withInstallEnv(extraEnv = {}) {
  return {
    ...process.env,
    ...extraEnv,
  };
}

function commandExists(runCommand, cmd) {
  const result = runCommand(process.platform === 'win32' ? 'where' : 'which', [cmd]);
  return result.status === 0;
}

function hasBundledWebClient(webClientDir) {
  return fs.existsSync(path.join(webClientDir, 'index.html'))
    && (
      fs.existsSync(path.join(webClientDir, 'main.dart.js'))
      || fs.existsSync(path.join(webClientDir, 'flutter_bootstrap.js'))
      || fs.existsSync(path.join(webClientDir, 'flutter.js'))
    );
}

// Two renames: the old build is out of the way for only a moment.
function swapIn(nextDir, liveDir) {
  const oldDir = `${liveDir}-old`;
  fs.rmSync(oldDir, { recursive: true, force: true });
  if (fs.existsSync(liveDir)) fs.renameSync(liveDir, oldDir);
  fs.renameSync(nextDir, liveDir);
  fs.rmSync(oldDir, { recursive: true, force: true });
}

function buildBundledWebClientIfPossible({
  flutterAppDir,
  webClientDir,
  runCommand,
  commandExistsFn,
  onMissingSources,
  onUsingBundledClient,
  onMissingFlutter,
  onBuildStart,
  onBuildSuccess,
  onBuildFailed,
  fail,
  required = false,
}) {
  if (!fs.existsSync(path.join(flutterAppDir, 'pubspec.yaml'))) {
    if (hasBundledWebClient(webClientDir)) {
      onUsingBundledClient?.();
      return false;
    }
    if (required) fail(`Missing Flutter app sources at ${flutterAppDir}`);
    onMissingSources?.();
    return false;
  }

  if (!commandExistsFn('flutter')) {
    if (hasBundledWebClient(webClientDir)) {
      onMissingFlutter?.();
      return false;
    }
    fail('Flutter SDK is required to build the web client because no bundled client was found.');
  }

  // The running server keeps serving the current build while the new one is
  // made next to it, so a page opened during an update never loads a missing
  // main.dart.js. `flutter clean` would delete the live build, so only the
  // build cache it exists for is cleared.
  fs.rmSync(path.join(flutterAppDir, '.dart_tool', 'flutter_build'), { recursive: true, force: true });
  const pubGet = runCommand('flutter', ['pub', 'get'], {
    cwd: flutterAppDir,
    env: withInstallEnv(),
  });
  if (pubGet.status !== 0) fail('Flutter pub get failed before web rebuild.');

  const nextDir = `${webClientDir}-next`;
  fs.rmSync(nextDir, { recursive: true, force: true });
  onBuildStart?.();
  const build = runCommand(
    'flutter',
    // The full icon font keeps its glyphs stable across releases, so a cached
    // font from an older build never shows blank icons.
    ['build', 'web', '--release', '--no-tree-shake-icons', '--base-href', '/app/', '--output', nextDir],
    {
      cwd: flutterAppDir,
      env: withInstallEnv(),
    },
  );
  if (build.status !== 0 || !hasBundledWebClient(nextDir)) {
    fs.rmSync(nextDir, { recursive: true, force: true });
    if (hasBundledWebClient(webClientDir)) {
      onBuildFailed?.();
      return false;
    }
    fail('Flutter web build failed and no bundled web client is available.');
  }
  swapIn(nextDir, webClientDir);
  onBuildSuccess?.();
  return true;
}

module.exports = {
  withInstallEnv,
  commandExists,
  hasBundledWebClient,
  buildBundledWebClientIfPossible,
};
