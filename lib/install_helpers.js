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

const PREBUILT_URL = (channel) => `https://github.com/NeoLabs-Systems/NeoRouter/releases/download/web-${channel === 'beta' ? 'beta' : 'main'}/neorouter-web.tar.gz`;
const FLUTTER_REPO = 'https://github.com/flutter/flutter.git';

// The web client GitHub builds for every push (.github/workflows/web-client.yml),
// so a machine without Flutter still gets the current UI.
function downloadPrebuiltWebClient({ webClientDir, channel, runCommand }) {
  const nextDir = `${webClientDir}-next`;
  const archive = `${webClientDir}-download.tar.gz`;
  fs.rmSync(nextDir, { recursive: true, force: true });
  fs.mkdirSync(path.dirname(webClientDir), { recursive: true });
  const fetched = runCommand('curl', ['-fsSL', '--retry', '2', '-o', archive, PREBUILT_URL(channel)], { env: withInstallEnv() });
  if (fetched.status !== 0) {
    fs.rmSync(archive, { force: true });
    return false;
  }
  fs.mkdirSync(nextDir, { recursive: true });
  const unpacked = runCommand('tar', ['-xzf', archive, '-C', nextDir], { env: withInstallEnv() });
  fs.rmSync(archive, { force: true });
  if (unpacked.status !== 0 || !hasBundledWebClient(nextDir)) {
    fs.rmSync(nextDir, { recursive: true, force: true });
    return false;
  }
  swapIn(nextDir, webClientDir);
  return true;
}

// Flutter in NeoRouter's own tools folder, installed once when there is no
// other way to get the web client (no Flutter, no prebuilt download).
function localFlutter(toolsDir) {
  const bin = path.join(toolsDir, 'flutter', 'bin', process.platform === 'win32' ? 'flutter.bat' : 'flutter');
  return fs.existsSync(bin) ? bin : null;
}

function installFlutter({ toolsDir, runCommand }) {
  fs.mkdirSync(toolsDir, { recursive: true });
  const target = path.join(toolsDir, 'flutter');
  fs.rmSync(target, { recursive: true, force: true });
  const cloned = runCommand('git', ['clone', '--depth', '1', '--branch', 'stable', FLUTTER_REPO, target], { env: withInstallEnv() });
  if (cloned.status !== 0) return null;
  const flutter = localFlutter(toolsDir);
  if (!flutter) return null;
  // The first run fetches the Dart SDK and the web engine.
  runCommand(flutter, ['config', '--no-analytics'], { env: withInstallEnv() });
  return runCommand(flutter, ['precache', '--web'], { env: withInstallEnv() }).status === 0 ? flutter : null;
}

// Builds with Flutter when it is there. Without it: the prebuilt client from
// GitHub, and only when that is not available either, Flutter is installed
// into the tools folder and the client built with it.
function buildBundledWebClientIfPossible({
  flutterAppDir,
  webClientDir,
  runCommand,
  commandExistsFn,
  channel = 'stable',
  toolsDir = null,
  onMissingSources,
  onUsingBundledClient,
  onMissingFlutter,
  onDownloaded,
  onInstallingFlutter,
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
    // An npm install carries no app sources to build from: the prebuilt client.
    if (downloadPrebuiltWebClient({ webClientDir, channel, runCommand })) {
      onDownloaded?.();
      return true;
    }
    if (required) fail(`Missing Flutter app sources at ${flutterAppDir}, and the prebuilt web client could not be downloaded.`);
    onMissingSources?.();
    return false;
  }

  let flutter = commandExistsFn('flutter') ? 'flutter' : toolsDir && localFlutter(toolsDir);
  if (!flutter) {
    onMissingFlutter?.();
    if (downloadPrebuiltWebClient({ webClientDir, channel, runCommand })) {
      onDownloaded?.();
      return true;
    }
    if (!toolsDir) {
      if (hasBundledWebClient(webClientDir)) return false;
      fail('Flutter SDK is required to build the web client because no bundled or prebuilt client was found.');
    }
    onInstallingFlutter?.();
    flutter = installFlutter({ toolsDir, runCommand });
    if (!flutter) {
      if (hasBundledWebClient(webClientDir)) return false;
      fail('The web client could not be downloaded and Flutter could not be installed. Install Flutter (https://docs.flutter.dev/get-started/install) and run `neorouter rebuild-web`.');
    }
  }

  // The running server keeps serving the current build while the new one is
  // made next to it, so a page opened during an update never loads a missing
  // main.dart.js. `flutter clean` would delete the live build, so only the
  // build cache it exists for is cleared.
  fs.rmSync(path.join(flutterAppDir, '.dart_tool', 'flutter_build'), { recursive: true, force: true });
  const pubGet = runCommand(flutter, ['pub', 'get'], {
    cwd: flutterAppDir,
    env: withInstallEnv(),
  });
  if (pubGet.status !== 0) fail('Flutter pub get failed before web rebuild.');

  const nextDir = `${webClientDir}-next`;
  fs.rmSync(nextDir, { recursive: true, force: true });
  onBuildStart?.();
  const build = runCommand(
    flutter,
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
    // A failed local build can still fall back to the prebuilt client.
    if (downloadPrebuiltWebClient({ webClientDir, channel, runCommand })) {
      onDownloaded?.();
      return true;
    }
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
  downloadPrebuiltWebClient,
  PREBUILT_URL,
  commandExists,
  hasBundledWebClient,
  buildBundledWebClientIfPossible,
};
