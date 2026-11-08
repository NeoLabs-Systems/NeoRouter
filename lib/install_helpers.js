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

const PREBUILT_ARCHIVE = 'neorouter-web.tar.gz';
const releaseTag = (channel) => `web-${channel === 'beta' ? 'beta' : 'main'}`;
const PREBUILT_URL = (channel) => `https://github.com/NeoLabs-Systems/NeoRouter/releases/download/${releaseTag(channel)}/${PREBUILT_ARCHIVE}`;
const RELEASE_API_URL = (channel) => `https://api.github.com/repos/NeoLabs-Systems/NeoRouter/releases/tags/${releaseTag(channel)}`;
const FLUTTER_REPO = 'https://github.com/flutter/flutter.git';

// GitHub's download link sometimes answers 504 for minutes at a time; its API
// serves the same file, so it is the fallback.
function fetchPrebuiltArchive({ channel, archive, runCommand }) {
  const env = withInstallEnv();
  if (runCommand('curl', ['-fsSL', '--retry', '2', '-o', archive, PREBUILT_URL(channel)], { env }).status === 0) return true;
  const release = runCommand('curl', ['-fsSL', '--retry', '2', '-H', 'Accept: application/vnd.github+json', RELEASE_API_URL(channel)], { env });
  let asset = null;
  try {
    asset = JSON.parse(String(release.stdout || '')).assets?.find((item) => item.name === PREBUILT_ARCHIVE)?.url || null;
  } catch {
    asset = null;
  }
  if (release.status !== 0 || !asset) return false;
  return runCommand('curl', ['-fsSL', '--retry', '3', '-H', 'Accept: application/octet-stream', '-o', archive, asset], { env }).status === 0;
}

// The web client GitHub builds for every push (.github/workflows/web-client.yml),
// so a machine without Flutter still gets the current UI.
function downloadPrebuiltWebClient({ webClientDir, channel, runCommand }) {
  const nextDir = `${webClientDir}-next`;
  const archive = `${webClientDir}-download.tar.gz`;
  fs.rmSync(nextDir, { recursive: true, force: true });
  fs.mkdirSync(path.dirname(webClientDir), { recursive: true });
  if (!fetchPrebuiltArchive({ channel, archive, runCommand })) {
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

// The web client only works on iOS 26 Safari from this Flutter on. Older ones
// still build it, so a failed upgrade doesn't stop the build.
const REQUIRED_FLUTTER_VERSION = '3.47.9';

function isOlderVersion(version, minimum) {
  const parts = (value) => value.split('-')[0].split('.').map(Number);
  const [left, right] = [parts(version), parts(minimum)];
  for (let index = 0; index < right.length; index += 1) {
    if (left[index] !== right[index]) return left[index] < right[index];
  }
  return false;
}

// Shallow clones in the tools folder report no version; those are left alone.
function upgradeOlderFlutter({ flutter, runCommand, onFlutterUpgradeStart }) {
  const result = runCommand(flutter, ['--version', '--machine'], { env: withInstallEnv() });
  // First runs can print banners before the JSON.
  const start = String(result.stdout || '').indexOf('{');
  if (result.status !== 0 || start < 0) return;
  let version = '';
  try {
    version = JSON.parse(result.stdout.slice(start)).frameworkVersion || '';
  } catch {
    return;
  }
  if (!/^[1-9]\d*\.\d+\.\d+/.test(version) || !isOlderVersion(version, REQUIRED_FLUTTER_VERSION)) return;
  onFlutterUpgradeStart?.(REQUIRED_FLUTTER_VERSION);
  runCommand(flutter, ['upgrade'], { env: withInstallEnv() });
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
  onFlutterUpgradeStart,
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

  upgradeOlderFlutter({ flutter, runCommand, onFlutterUpgradeStart });

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
