'use strict';

const fs = require('node:fs');
const path = require('node:path');
const net = require('node:net');
const { spawn, spawnSync } = require('node:child_process');
const {
  commandExists: sharedCommandExists,
  withInstallEnv,
  hasBundledWebClient,
  buildBundledWebClientIfPossible: sharedBuildWebClient,
} = require('./install_helpers');
const {
  APP_DIR,
  ENV_FILE,
  FLUTTER_APP_DIR,
  LOG_DIR,
  PID_FILE,
  RUNTIME_HOME,
  SERVER_PLIST_DST,
  SERVER_SERVICE_LABEL,
  SERVER_SYSTEMD_UNIT,
  WEB_CLIENT_DIR,
  ensureRuntimeDirs,
} = require('../runtime/paths');
const {
  loadEnvironment,
  parseEnv,
  readEnvFileRaw,
  removeEnvValue,
  upsertEnvValue,
} = require('../runtime/env');
const {
  describeReleaseChannelPolicy,
  parseReleaseChannel,
  readConfiguredReleaseChannel,
  writeReleaseChannelToEnvFile,
} = require('../runtime/release_channel');
const { createGitHelpers } = require('../runtime/git_helpers');
const { detectContainer } = require('../runtime/container');
const ui = require('./cli/ui');
const prompt = require('./cli/prompt');
const { inputError } = require('./cli/errors');
const { printHelp } = require('./cli/help');
const { printHomeHeader, printSetupFinished, printSetupProgress } = require('./cli/screens');
const { needsAttention, printChecks, printStatus, statusChecks } = require('./cli/status');
const { closeStepList, createStepList, formatDuration, noteActiveStep } = require('./cli/steps');
const { createUpdateGit } = require('./cli/updates');
const { checkForUpdate: checkChannelForUpdate, resolvePreferredBranch } = require('../runtime/update_check');

const APP_NAME = 'NeoRouter';
const GITHUB_REPO = 'NeoLabs-Systems/NeoRouter';
const DEFAULT_PORT = 4600;

// Inside a container the image is the unit of deployment: no launchd or user
// systemd to install, no checkout to pull, and the process must stay in the
// foreground so the container runtime can supervise it.
const IN_CONTAINER = detectContainer();

loadEnvironment();

// spawnSync's 1 MB default is too small for npm and Flutter build output.
const QUIET_OUTPUT_BUFFER = 64 * 1024 * 1024;

function runQuiet(cmd, args, options = {}) {
  return spawnSync(cmd, args, {
    encoding: 'utf8',
    cwd: APP_DIR,
    env: process.env,
    stdio: ['ignore', 'pipe', 'pipe'],
    maxBuffer: QUIET_OUTPUT_BUFFER,
    ...options,
  });
}

// The last lines a failed quiet command printed, for its error message.
function outputTail(result, lines = 12) {
  const text = String(result.stderr || result.stdout || result.error?.message || '').trim();
  return text.split('\n').slice(-lines).join('\n');
}

// Runs a command without echoing its output; a failure carries its last lines.
function runOrThrow(cmd, args, options = {}) {
  const result = runQuiet(cmd, args, options);
  if (result.status !== 0) {
    const failure = `${cmd} ${args.join(' ')} failed with exit code ${result.status}`;
    const tail = outputTail(result);
    throw new Error(tail ? `${failure}\n${tail}` : failure);
  }
  return result;
}

const {
  gitWorkingTreeDirty,
  gitStashPush,
  gitStashPopByMessage,
  gitLocalBranchExists,
  gitRemoteBranchExists,
} = createGitHelpers((cmd, args) => runQuiet(cmd, args));

// While a step list is on screen, progress messages become the current step's
// detail line instead of scrolling past it. Warnings always print.
function logInfo(msg) {
  if (!noteActiveStep(msg)) ui.item('info', msg);
}

function logOk(msg) {
  if (!noteActiveStep(msg)) ui.item('ok', msg);
}

function logWarn(msg) {
  ui.item('warn', msg);
}

function detectPlatform() {
  if (process.platform === 'darwin') return 'macos';
  if (process.platform === 'linux') return 'linux';
  if (process.platform === 'win32') return 'windows';
  return 'other';
}

function commandExists(cmd) {
  return sharedCommandExists((command, args) => runQuiet(command, args), cmd);
}

function launchctlDomain() {
  try { return `gui/${process.getuid()}`; } catch { return null; }
}

function launchctlServiceTarget(label = SERVER_SERVICE_LABEL) {
  const domain = launchctlDomain();
  return domain ? `${domain}/${label}` : label;
}

function loadEnvPort() {
  const env = parseEnv(readEnvFileRaw(ENV_FILE));
  const raw = process.env.NEOROUTER_PORT || env.get('NEOROUTER_PORT') || String(DEFAULT_PORT);
  const port = Number(raw);
  return Number.isInteger(port) && port > 0 && port <= 65535 ? port : DEFAULT_PORT;
}

function validateEnvKey(key) {
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(String(key || ''))) {
    throw new Error(`Invalid environment variable name: ${key || '(empty)'}`);
  }
}

function maskEnvValue(key, value) {
  const upper = String(key || '').toUpperCase();
  if (/(PASSWORD|SECRET|TOKEN|KEY|API)/.test(upper) && value) {
    if (String(value).length <= 8) return '********';
    return `${String(value).slice(0, 2)}********${String(value).slice(-2)}`;
  }
  return value;
}

function readInstalledPackageVersion() {
  try {
    // Not require(): an update changes this file while the process runs.
    return JSON.parse(fs.readFileSync(path.join(APP_DIR, 'package.json'), 'utf8')).version || '0.0.0';
  } catch {
    return '0.0.0';
  }
}

function readGitVersionLabel() {
  const gitVersion = runQuiet('git', ['describe', '--tags', '--always', '--dirty']);
  if (gitVersion.status !== 0) return null;
  return gitVersion.stdout.trim().replace(/^v/, '') || null;
}

function currentInstalledVersionLabel() {
  const pkg = readInstalledPackageVersion();
  const git = readGitVersionLabel();
  if (git && git !== pkg) return `${pkg} (${git})`;
  return pkg;
}

function currentReleaseChannel() {
  return readConfiguredReleaseChannel({ envFile: ENV_FILE });
}

function releaseChannelSummary(channel) {
  return describeReleaseChannelPolicy(parseReleaseChannel(channel) || currentReleaseChannel());
}

function resolvePreferredGitBranch(channel) {
  return resolvePreferredBranch(parseReleaseChannel(channel) || currentReleaseChannel(), runQuiet, { onFallback: logWarn });
}

function restoreStashedUpdateChanges(stashMessage) {
  if (!stashMessage) return;
  const result = gitStashPopByMessage(stashMessage);
  if (result.restored) {
    logOk('Restored the local changes that were stashed for the update.');
    return;
  }
  if (result.missing) return;
  logWarn('Update finished, but your previous local changes could not be reapplied automatically. Recover them with: git stash pop');
}

function ensureGitBranchForReleaseChannel(targetBranch) {
  const branchRes = runQuiet('git', ['rev-parse', '--abbrev-ref', 'HEAD']);
  const currentBranch = branchRes.status === 0 ? branchRes.stdout.trim() : '';
  if (currentBranch === targetBranch) return null;
  if (!gitRemoteBranchExists(targetBranch)) {
    throw new Error(`Release channel branch "${targetBranch}" was not found on origin.`);
  }
  let stashMessage = null;
  if (gitWorkingTreeDirty()) {
    logInfo(`Stashing local changes so the update can switch to ${targetBranch}.`);
    stashMessage = gitStashPush(`neorouter-update-autostash ${Date.now()}`);
  }
  try {
    if (gitLocalBranchExists(targetBranch)) runOrThrow('git', ['checkout', targetBranch]);
    else runOrThrow('git', ['checkout', '-b', targetBranch, '--track', `origin/${targetBranch}`]);
  } catch (error) {
    restoreStashedUpdateChanges(stashMessage);
    throw error;
  }
  if (currentBranch) logOk(`Switched git branch ${currentBranch} -> ${targetBranch}`);
  else logOk(`Checked out git branch ${targetBranch}`);
  return stashMessage;
}

function pruneOldRuntimeBackups(backupsDir, keepLatest = 3) {
  if (!fs.existsSync(backupsDir) || keepLatest < 0) return;
  const backupDirs = fs.readdirSync(backupsDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name.startsWith('pre-update-'))
    .map((entry) => {
      const fullPath = path.join(backupsDir, entry.name);
      try {
        return { name: entry.name, fullPath, mtimeMs: fs.statSync(fullPath).mtimeMs };
      } catch {
        return null;
      }
    })
    .filter(Boolean)
    .sort((a, b) => (b.mtimeMs - a.mtimeMs) || b.name.localeCompare(a.name));
  for (const backup of backupDirs.slice(keepLatest)) {
    try { fs.rmSync(backup.fullPath, { recursive: true, force: true }); } catch { /* best effort */ }
  }
}

function backupRuntimeData() {
  const runtime = ensureRuntimeDirs();
  const backupsDir = runtime.backups;
  const stamp = new Date().toISOString().replace(/:/g, '-').replace(/\.\d{3}Z$/, 'Z');
  const target = path.join(backupsDir, `pre-update-${stamp}`);
  fs.mkdirSync(target, { recursive: true });
  if (fs.existsSync(ENV_FILE)) fs.copyFileSync(ENV_FILE, path.join(target, '.env'));
  if (fs.existsSync(runtime.data)) {
    fs.cpSync(runtime.data, path.join(target, 'data'), { recursive: true, force: false, errorOnExist: false });
  }
  pruneOldRuntimeBackups(backupsDir, 3);
  return target;
}

// Dependencies with a compiled part. Being installed is not enough: npm 12
// skips install scripts it was not told to allow (package.json allowScripts),
// and a missing compiler stops a build, both leaving the package without its
// binary until the server crashes on first use.
const NATIVE_DEPENDENCIES = ['better-sqlite3', 'bcrypt'];

function nativeDependenciesLoad() {
  const check = "new (require('better-sqlite3'))(':memory:').close(); require('bcrypt');";
  return runQuiet(process.execPath, ['-e', check], { cwd: APP_DIR }).status === 0;
}

function dependenciesReady() {
  try {
    require.resolve('express', { paths: [APP_DIR] });
    require.resolve('better-sqlite3', { paths: [APP_DIR] });
    require.resolve('zod', { paths: [APP_DIR] });
  } catch {
    return false;
  }
  return nativeDependenciesLoad();
}

// What to install so native modules can be built from source, per system.
function buildToolsHint() {
  if (process.platform === 'darwin') return 'xcode-select --install';
  if (process.platform === 'win32') return 'install "Desktop development with C++" from the Visual Studio Build Tools';
  const osRelease = (() => {
    try {
      return fs.readFileSync('/etc/os-release', 'utf8');
    } catch {
      return '';
    }
  })();
  if (/\b(arch|manjaro|endeavouros|cachyos)\b/i.test(osRelease)) return 'sudo pacman -S --needed base-devel python';
  if (/\b(fedora|rhel|centos|rocky|alma)\b/i.test(osRelease)) return 'sudo dnf install gcc-c++ make python3';
  if (/\balpine\b/i.test(osRelease)) return 'sudo apk add build-base python3';
  return 'sudo apt install build-essential python3';
}

function repairNativeDependencies() {
  logInfo('Rebuilding the database module');
  // npm 12 takes the allowed scripts from package.json (allowScripts) and
  // rejects the command-line flag in a project; older npm runs them anyway.
  runQuiet('npm', ['rebuild', ...NATIVE_DEPENDENCIES], { env: withInstallEnv() });
  if (nativeDependenciesLoad()) {
    logOk('Database module rebuilt');
    return;
  }
  throw new Error(`The database module could not be installed for Node.js ${process.versions.node}. `
    + `Install the build tools (${buildToolsHint()}) and run the installer again.`);
}

function installDependencies() {
  logInfo('Installing dependencies');
  runOrThrow('npm', ['install', '--omit=dev', '--no-audit', '--no-fund'], {
    env: withInstallEnv(),
  });
  if (!nativeDependenciesLoad()) repairNativeDependencies();
}

// Production dependencies from package.json that are not in node_modules,
// e.g. after the code was pulled by hand without `npm install`.
function missingDependencies() {
  try {
    const manifest = JSON.parse(fs.readFileSync(path.join(APP_DIR, 'package.json'), 'utf8'));
    return Object.keys(manifest.dependencies || {})
      .filter((name) => !fs.existsSync(path.join(APP_DIR, 'node_modules', name, 'package.json')));
  } catch {
    return [];
  }
}

function assertSupportedNodeRuntime() {
  const major = Number(String(process.versions.node || '').split('.')[0]);
  if (!Number.isInteger(major) || major < 20) {
    throw new Error(`NeoRouter requires Node.js 20 or newer. Current runtime is ${process.versions.node || 'unknown'}.`);
  }
}

function buildBundledWebClientIfPossible({ required = false } = {}) {
  // Build tools print a lot; only a failure's last lines are worth showing.
  let failedOutput = '';
  return sharedBuildWebClient({
    flutterAppDir: FLUTTER_APP_DIR,
    webClientDir: WEB_CLIENT_DIR,
    runCommand: (command, args, options = {}) => {
      const result = runQuiet(command, args, options);
      if (result.status !== 0) failedOutput = outputTail(result);
      return result;
    },
    commandExistsFn: commandExists,
    channel: currentReleaseChannel(),
    toolsDir: path.join(RUNTIME_HOME, 'tools'),
    onMissingSources: () => logInfo('Using the app that came with NeoRouter'),
    onUsingBundledClient: () => logInfo('Using the app that came with NeoRouter'),
    onMissingFlutter: () => logInfo('Downloading the app'),
    onDownloaded: () => logOk('App downloaded'),
    onInstallingFlutter: () => logInfo('Installing Flutter to build the app — a few minutes, once'),
    onBuildStart: () => logInfo('Building the app — this can take a few minutes'),
    onBuildSuccess: () => logOk('App rebuilt'),
    onBuildFailed: () => logWarn("The app couldn't be rebuilt; keeping the one you have"),
    fail: (message) => {
      throw new Error(failedOutput ? `${message}\n${failedOutput}` : message);
    },
    required,
  });
}

function xml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}

function quoteSystemdValue(value) {
  return `"${String(value).replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
}

function renderLaunchAgent() {
  const runtime = ensureRuntimeDirs();
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>${SERVER_SERVICE_LABEL}</string>
  <key>ProgramArguments</key>
  <array>
    <string>${xml(process.execPath)}</string>
    <string>${xml(path.join(APP_DIR, 'server', 'index.js'))}</string>
  </array>
  <key>WorkingDirectory</key><string>${xml(APP_DIR)}</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>NEOROUTER_HOME</key><string>${xml(runtime.home)}</string>
    <key>NODE_ENV</key><string>production</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>${xml(path.join(runtime.logs, 'neorouter.log'))}</string>
  <key>StandardErrorPath</key><string>${xml(path.join(runtime.logs, 'neorouter-error.log'))}</string>
</dict></plist>
`;
}

function renderSystemdUnit() {
  const runtime = ensureRuntimeDirs();
  const serverEntry = path.join(APP_DIR, 'server', 'index.js');
  // Only ExecStart= and Environment= unquote their values. The path settings
  // take the whole line as the path, spaces included, and refuse quotes.
  return `[Unit]
Description=NeoRouter inference router
After=network-online.target

[Service]
Type=simple
WorkingDirectory=${APP_DIR}
ExecStart=${quoteSystemdValue(process.execPath)} ${quoteSystemdValue(serverEntry)}
Restart=always
RestartSec=2
Environment=${quoteSystemdValue(`NEOROUTER_HOME=${runtime.home}`)}
Environment=NODE_ENV=production
EnvironmentFile=-${ENV_FILE}
StandardOutput=append:${path.join(runtime.logs, 'neorouter.log')}
StandardError=append:${path.join(runtime.logs, 'neorouter-error.log')}

[Install]
WantedBy=default.target
`;
}

// Stops the detached process an earlier install fell back to, if there is one.
function stopFallback() {
  if (!fs.existsSync(PID_FILE)) return false;
  const pid = Number(fs.readFileSync(PID_FILE, 'utf8').trim());
  if (Number.isFinite(pid) && pid > 0) {
    try {
      process.kill(pid, 'SIGTERM');
    } catch {
      // A stale PID file; the process is already gone.
    }
  }
  fs.rmSync(PID_FILE, { force: true });
  return true;
}

function startFallback() {
  const runtime = ensureRuntimeDirs();
  if (fs.existsSync(PID_FILE)) {
    const existingPid = Number(fs.readFileSync(PID_FILE, 'utf8'));
    if (Number.isFinite(existingPid) && existingPid > 0) {
      try {
        process.kill(existingPid, 0);
        logOk(`NeoRouter is already running (process ${existingPid})`);
        return;
      } catch {
        fs.rmSync(PID_FILE, { force: true });
      }
    }
  }
  const out = fs.openSync(path.join(runtime.logs, 'neorouter.log'), 'a');
  const err = fs.openSync(path.join(runtime.logs, 'neorouter-error.log'), 'a');
  const child = spawn(process.execPath, [path.join(APP_DIR, 'server', 'index.js')], {
    cwd: APP_DIR,
    detached: true,
    stdio: ['ignore', out, err],
    env: { ...process.env, NEOROUTER_HOME: runtime.home },
  });
  child.unref();
  fs.writeFileSync(PID_FILE, String(child.pid), { mode: 0o600 });
  logOk(`Running as a background process (${child.pid})`);
}

function installMacService() {
  ensureRuntimeDirs();
  fs.mkdirSync(path.dirname(SERVER_PLIST_DST), { recursive: true });
  fs.writeFileSync(SERVER_PLIST_DST, renderLaunchAgent(), { mode: 0o600 });
  const domain = launchctlDomain();
  if (domain) {
    runQuiet('launchctl', ['bootout', domain, SERVER_PLIST_DST]);
    const bootstrap = runQuiet('launchctl', ['bootstrap', domain, SERVER_PLIST_DST]);
    if (bootstrap.status !== 0) {
      runQuiet('launchctl', ['unload', SERVER_PLIST_DST]);
      runOrThrow('launchctl', ['load', SERVER_PLIST_DST]);
    } else {
      runQuiet('launchctl', ['enable', launchctlServiceTarget()]);
      runQuiet('launchctl', ['kickstart', '-k', launchctlServiceTarget()]);
    }
  } else {
    runQuiet('launchctl', ['unload', SERVER_PLIST_DST]);
    runOrThrow('launchctl', ['load', SERVER_PLIST_DST]);
  }
}

function installLinuxService() {
  ensureRuntimeDirs();
  fs.mkdirSync(path.dirname(SERVER_SYSTEMD_UNIT), { recursive: true });
  fs.writeFileSync(SERVER_SYSTEMD_UNIT, renderSystemdUnit(), { mode: 0o600 });
  stopFallback();
  runOrThrow('systemctl', ['--user', 'daemon-reload']);
  runOrThrow('systemctl', ['--user', 'enable', 'neorouter.service']);
  runOrThrow('systemctl', ['--user', 'start', 'neorouter.service']);
  runOrThrow('systemctl', ['--user', 'is-active', '--quiet', 'neorouter.service']);
}

function installService() {
  if (IN_CONTAINER) {
    // A user service inside a container is supervised by the container
    // runtime, not by systemd/launchd: installing one would start a second
    // server next to the one the entrypoint runs.
    throw new Error('NeoRouter runs inside a container. Start it with `neorouter serve` '
      + '(the image default) and let Docker, Compose or Kubernetes manage the lifecycle.');
  }
  const platform = detectPlatform();
  if (platform === 'macos' && commandExists('launchctl')) {
    installMacService();
    return 'launchd';
  }
  if (platform === 'linux' && commandExists('systemctl')) {
    try {
      installLinuxService();
      return 'systemd';
    } catch (error) {
      logWarn("Couldn't set up the system service, so NeoRouter won't restart by itself");
      ui.paragraph(error.message.replace(/\s+/g, ' '), { indent: 4, style: ui.paint.muted });
    }
  }
  startFallback();
  return 'detached';
}

function isPortOpen(port) {
  return new Promise((resolve) => {
    const socket = net.connect({ host: '127.0.0.1', port }, () => {
      socket.end();
      resolve(true);
    });
    socket.on('error', () => resolve(false));
    socket.setTimeout(1000, () => {
      socket.destroy();
      resolve(false);
    });
  });
}

async function waitForServerReady(port, timeoutMs = 30000) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    if (await isPortOpen(port)) {
      try {
        const response = await fetch(`http://127.0.0.1:${port}/health`, { signal: AbortSignal.timeout(1500) });
        if (response.ok) return true;
      } catch {
        // still booting
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 400));
  }
  const error = new Error(`NeoRouter did not become reachable on port ${port}.`);
  error.code = 'SERVER_NOT_READY';
  throw error;
}

function ensureDefaultEnvFile() {
  ensureRuntimeDirs();
  if (!fs.existsSync(ENV_FILE)) {
    const example = path.join(APP_DIR, '.env.example');
    if (fs.existsSync(example)) {
      fs.copyFileSync(example, ENV_FILE);
    } else {
      fs.writeFileSync(ENV_FILE, '# NeoRouter local configuration\nNEOROUTER_HOST=127.0.0.1\nNEOROUTER_PORT=4600\n', { mode: 0o600 });
    }
    try { fs.chmodSync(ENV_FILE, 0o600); } catch (_) { /* ignore */ }
  }
  if (!parseEnv(readEnvFileRaw(ENV_FILE)).has('NEOROUTER_RELEASE_CHANNEL') && !process.env.NEOROUTER_RELEASE_CHANNEL) {
    writeReleaseChannelToEnvFile(currentReleaseChannel(), ENV_FILE);
  }
}

function migrateDatabase() {
  // The server's log lines would land in the middle of the step list.
  require('../server/db/migrate').migrate(undefined, { quiet: true });
}

function scanForInstalledInstance() {
  const evidence = [];
  if (fs.existsSync(ENV_FILE)) evidence.push({ kind: 'config', path: ENV_FILE });
  if (fs.existsSync(SERVER_PLIST_DST)) evidence.push({ kind: 'launchd', path: SERVER_PLIST_DST });
  if (fs.existsSync(SERVER_SYSTEMD_UNIT)) evidence.push({ kind: 'systemd', path: SERVER_SYSTEMD_UNIT });
  if (fs.existsSync(PID_FILE)) evidence.push({ kind: 'pid', path: PID_FILE });
  if (hasBundledWebClient(WEB_CLIENT_DIR)) evidence.push({ kind: 'web', path: WEB_CLIENT_DIR });
  if (dependenciesReady()) evidence.push({ kind: 'deps', path: path.join(APP_DIR, 'node_modules') });
  return { installed: evidence.length > 0, evidence };
}

async function cmdInstall() {
  printSetupProgress(0);
  ui.title(`Welcome to ${APP_NAME}`, "Let's get it running on this computer. It can take a few minutes.");
  ui.blank();
  ensureDefaultEnvFile();
  assertSupportedNodeRuntime();
  const port = loadEnvPort();
  let manager = null;
  const steps = [
    [["Preparing NeoRouter's files", "Prepared NeoRouter's files"], () => {
      if (!dependenciesReady()) installDependencies();
    }],
    [['Preparing the app', 'Prepared the app'], () => {
      if (!hasBundledWebClient(WEB_CLIENT_DIR)) buildBundledWebClientIfPossible({ required: true });
    }],
    [['Updating the database', 'Updated the database'], migrateDatabase],
  ];
  if (IN_CONTAINER) {
    // Nothing to install: the image already holds the code and the client, and
    // the container runtime supervises the process. Run the server in the
    // foreground so this is `docker run … neorouter install` and it stays up.
    await runSteps(steps, { bar: true });
    logOk('Container detected: starting the server in the foreground');
    return cmdServe();
  }
  steps.push(
    [['Starting NeoRouter', 'Started NeoRouter'], () => { manager = installService(); }],
    [["Checking it's answering", "It's answering"], () => waitForServerReady(port)],
  );
  await runSteps(steps, { bar: true });
  const url = appUrl(port);
  printSetupProgress(2);
  printSetupFinished({
    url,
    startsItself: manager !== 'detached',
    hasAccount: hasAccount(),
    notes: [
      'Add your providers and create a router key in the app.',
      `Point your apps at ${ui.paint.accent(`http://localhost:${port}/v1`)}, the OpenAI-compatible address.`,
    ],
  });
  if (!ui.isInteractive()) return;
  ui.keys([['enter', 'open it'], ['q', 'done']]);
  if (await prompt.waitForKey(['return', 'q', 'escape']) === 'return') openInBrowser(url);
}

function hasAccount() {
  try {
    return require('../server/db/database').getDatabase().prepare('SELECT 1 FROM users LIMIT 1').get() !== undefined;
  } catch {
    return false;
  }
}

function openInBrowser(url) {
  const opener = { darwin: 'open', win32: 'explorer' }[process.platform] || 'xdg-open';
  spawnSync(opener, [url], { stdio: 'ignore' });
}

async function cmdSetup(args = []) {
  ensureDefaultEnvFile();
  assertSupportedNodeRuntime();
  const channelArg = args.find((arg) => arg === 'stable' || arg === 'beta')
    || (args.includes('--channel') ? args[args.indexOf('--channel') + 1] : null)
    || args.find((arg) => arg.startsWith('--channel='))?.split('=')[1];
  const channel = channelArg ? parseReleaseChannel(channelArg) : currentReleaseChannel();
  if (!channel) throw inputError('Usage: neorouter setup [--channel stable|beta]');
  if (channelArg) {
    writeReleaseChannelToEnvFile(channel, ENV_FILE);
    process.env.NEOROUTER_RELEASE_CHANNEL = channel;
  }
  ui.title(`Setting up ${APP_NAME}`, `Updates come from the ${channel} channel.`);
  ui.blank();
  await runSteps([
    [["Preparing NeoRouter's files", "Prepared NeoRouter's files"], () => {
      if (!dependenciesReady()) installDependencies();
    }],
    [['Updating the database', 'Updated the database'], migrateDatabase],
    [['Preparing the app', 'Prepared the app'], () => {
      if (!hasBundledWebClient(WEB_CLIENT_DIR)) buildBundledWebClientIfPossible({ required: true });
    }],
  ]);
  ui.blank();
  ui.item('ok', ui.paint.bold('Setup complete'), RUNTIME_HOME);
  ui.note(IN_CONTAINER
    ? `Next, run ${ui.paint.command('neorouter serve')} to start the server.`
    : `Next, run ${ui.paint.command('neorouter install')} to start NeoRouter on this computer.`);
  ui.blank();
}

// One step list for a fixed sequence of [label, task] pairs. Returns each
// task's result in order.
async function runSteps(steps, options) {
  const list = createStepList(steps.map(([label]) => label), options);
  const results = [];
  try {
    for (const [index, [, task]] of steps.entries()) {
      results.push(await list.run(index, task));
    }
  } finally {
    closeStepList(list);
  }
  return results;
}

function printRunning(headline) {
  ui.blank();
  ui.item('ok', ui.paint.bold(headline), '');
  ui.print(`    ${ui.paint.accent(appUrl(loadEnvPort()))}`);
}

function appUrl(port) {
  return `http://localhost:${port}/app`;
}

// Starts whichever service manager this installation uses.
function launchService() {
  const platform = detectPlatform();
  if (platform === 'macos' && fs.existsSync(SERVER_PLIST_DST)) {
    installMacService();
  } else if (platform === 'linux' && fs.existsSync(SERVER_SYSTEMD_UNIT)) {
    runOrThrow('systemctl', ['--user', 'start', 'neorouter.service']);
    runOrThrow('systemctl', ['--user', 'is-active', '--quiet', 'neorouter.service']);
  } else {
    startFallback();
  }
}

// Asks the service manager and any detached process to stop NeoRouter. Says
// whether there was anything to stop.
function stopService() {
  let stopped = stopFallback();
  const platform = detectPlatform();
  if (platform === 'macos' && fs.existsSync(SERVER_PLIST_DST)) {
    const domain = launchctlDomain();
    if (domain) {
      runQuiet('launchctl', ['bootout', domain, SERVER_PLIST_DST]);
      runQuiet('launchctl', ['bootout', launchctlServiceTarget()]);
    }
    runQuiet('launchctl', ['unload', SERVER_PLIST_DST]);
    stopped = true;
  } else if (platform === 'linux' && fs.existsSync(SERVER_SYSTEMD_UNIT)) {
    runQuiet('systemctl', ['--user', 'stop', 'neorouter.service']);
    stopped = true;
  }
  return stopped;
}

// A server still shutting down would answer the next start's readiness check.
async function stopAndWait() {
  const stopped = stopService();
  const port = loadEnvPort();
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline && await isPortOpen(port)) {
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  return stopped;
}

async function cmdStart() {
  ensureDefaultEnvFile();
  if (IN_CONTAINER) {
    // A container has exactly one foreground process; "starting" means running
    // it here instead of handing off to a service manager that is not there.
    logInfo('Container detected: running in the foreground');
    if (!hasBundledWebClient(WEB_CLIENT_DIR)) buildBundledWebClientIfPossible({ required: true });
    return cmdServe();
  }
  const port = loadEnvPort();
  await runSteps([
    [['Starting NeoRouter', 'Started NeoRouter'], () => {
      if (!hasBundledWebClient(WEB_CLIENT_DIR)) buildBundledWebClientIfPossible({ required: true });
      launchService();
    }],
    [["Checking it's answering", "It's answering"], () => waitForServerReady(port)],
  ]);
  printRunning(`${APP_NAME} is running.`);
  ui.blank();
}

function cmdServe() {
  ensureDefaultEnvFile();
  ui.title(`${APP_NAME} is starting`, 'Press Ctrl+C to stop it.');
  ui.blank();
  require('../server/index.js');
}

// The server is PID 1's child in a container and nothing else can stop it from
// inside; the container runtime owns the lifecycle.
function printContainerLifecycle(verb) {
  ui.blank();
  logInfo(`NeoRouter runs in a container: ${verb} it with \`docker ${verb} <container>\``);
  ui.blank();
}

async function cmdStop() {
  if (IN_CONTAINER) {
    printContainerLifecycle('stop');
    return;
  }
  const [stopped] = await runSteps([[['Stopping NeoRouter', 'Stopped NeoRouter'], stopAndWait]]);
  ui.blank();
  if (stopped) logOk(`${APP_NAME} stopped`);
  else logWarn("NeoRouter wasn't running");
  ui.blank();
}

async function cmdRestart() {
  if (IN_CONTAINER) {
    printContainerLifecycle('restart');
    return;
  }
  const startedAt = Date.now();
  ensureDefaultEnvFile();
  const port = loadEnvPort();
  await runSteps([
    [['Getting the app ready', 'Got the app ready'], () => buildBundledWebClientIfPossible()],
    [['Stopping NeoRouter', 'Stopped NeoRouter'], stopAndWait],
    [['Starting it again', 'Started it again'], launchService],
    [["Checking it's answering", "It's answering"], () => waitForServerReady(port)],
  ]);
  printRunning(`Back online in ${formatDuration(Date.now() - startedAt)}.`);
  ui.blank();
}

async function cmdRebuildWeb() {
  await runSteps([[['Rebuilding the app', 'Rebuilt the app'], () => buildBundledWebClientIfPossible({ required: true })]]);
  ui.blank();
  logOk('App rebuilt');
  ui.blank();
}

async function fetchRunningVersion(port) {
  try {
    const response = await fetch(`http://127.0.0.1:${port}/health`, { signal: AbortSignal.timeout(1500) });
    return response.ok ? (await response.json()).version || null : null;
  } catch {
    return null;
  }
}

function isGitCheckout() {
  return fs.existsSync(path.join(APP_DIR, '.git')) && commandExists('git');
}

async function findPendingUpdate(channel) {
  if (!isGitCheckout()) return { checked: false, update: null };
  return checkChannelForUpdate({ run: runQuiet, channel });
}

async function collectStatusFacts({ checkForUpdate = false } = {}) {
  const port = loadEnvPort();
  const running = await isPortOpen(port);
  const platform = detectPlatform();
  let serviceActive = false;
  if (platform === 'macos' && fs.existsSync(SERVER_PLIST_DST)) {
    const result = runQuiet('launchctl', ['list', SERVER_SERVICE_LABEL]);
    serviceActive = result.status === 0 && Boolean(result.stdout.trim());
  } else if (platform === 'linux' && fs.existsSync(SERVER_SYSTEMD_UNIT)) {
    const result = runQuiet('systemctl', ['--user', 'is-active', 'neorouter.service']);
    serviceActive = result.status === 0 && result.stdout.trim() === 'active';
  }
  const channel = currentReleaseChannel();
  const updateCheck = checkForUpdate && !IN_CONTAINER ? await findPendingUpdate(channel) : { checked: false, update: null };
  return {
    appDir: APP_DIR,
    envFile: ENV_FILE,
    port,
    url: appUrl(port),
    running,
    inContainer: IN_CONTAINER,
    version: readInstalledPackageVersion(),
    versionLabel: currentInstalledVersionLabel(),
    runningVersion: running ? await fetchRunningVersion(port) : null,
    channelSummary: releaseChannelSummary(channel),
    serviceActive,
    configPresent: fs.existsSync(ENV_FILE),
    webClientPresent: hasBundledWebClient(WEB_CLIENT_DIR),
    update: updateCheck.update,
    updateChecked: updateCheck.checked,
  };
}

async function cmdStatus(args = []) {
  const facts = await collectStatusFacts({ checkForUpdate: ui.isInteractive() });
  printStatus(facts, { details: args.includes('--details') });
}

async function cmdDoctor() {
  const port = loadEnvPort();
  let runtimeWritable = true;
  try { fs.accessSync(RUNTIME_HOME, fs.constants.R_OK | fs.constants.W_OK); } catch { runtimeWritable = false; }
  const nodeSupported = Number(String(process.versions.node || '').split('.')[0]) >= 20;
  const running = await isPortOpen(port);
  const checks = [
    { ok: runtimeWritable, label: 'Data folder', message: runtimeWritable ? 'Can be read and written' : `Can't write to ${RUNTIME_HOME}: check its permissions` },
    { ok: fs.existsSync(ENV_FILE), label: 'Settings', message: fs.existsSync(ENV_FILE) ? 'Saved' : 'Not set up yet', action: 'neorouter setup' },
    { ok: dependenciesReady(), label: 'Dependencies', message: dependenciesReady() ? 'Installed' : 'Some are missing', action: 'neorouter repair' },
    { ok: hasBundledWebClient(WEB_CLIENT_DIR), label: 'App in your browser', message: hasBundledWebClient(WEB_CLIENT_DIR) ? 'Installed' : 'Missing', action: 'neorouter rebuild-web' },
    { ok: nodeSupported, label: 'Node.js', message: nodeSupported ? process.versions.node : `${process.versions.node}: NeoRouter needs 20 or newer` },
    { ok: running, label: 'NeoRouter', message: running ? `Answering on port ${port}` : `Not answering on port ${port}`, action: 'neorouter start' },
  ];
  ui.title(`Checking ${APP_NAME}`, 'Nothing is changed while this runs.');
  printChecks(checks.map((entry) => ({
    tone: entry.ok ? 'ok' : 'error',
    label: entry.label,
    detail: entry.message,
    fix: entry.ok ? null : entry.action,
  })));
  ui.blank();
  if (checks.every((entry) => entry.ok)) {
    ui.item('ok', ui.paint.bold('No problems found'));
    ui.blank();
  } else if (!IN_CONTAINER && ui.isInteractive() && await prompt.confirm('Fix these for you now? Your data stays where it is.')) {
    await cmdFix();
  } else {
    ui.note(`Most problems are fixed by ${ui.paint.command('neorouter repair')}.`);
    ui.blank();
  }
}

function cmdLogs() {
  ensureRuntimeDirs();
  const log = path.join(LOG_DIR, 'neorouter.log');
  const err = path.join(LOG_DIR, 'neorouter-error.log');
  if (!fs.existsSync(log) && !fs.existsSync(err)) {
    ui.blank();
    ui.item('info', 'No logs yet.');
    ui.blank();
    return;
  }
  const files = [log, err].filter((file) => fs.existsSync(file));
  if (commandExists('tail')) {
    ui.title('What NeoRouter is doing', 'Press Ctrl+C to stop watching.');
    ui.blank();
    spawnSync('tail', ['-n', '80', '-f', ...files], { stdio: 'inherit', cwd: APP_DIR });
    return;
  }
  for (const file of files) {
    ui.section(file);
    process.stdout.write(fs.readFileSync(file, 'utf8'));
  }
}

function cmdChannel(args = []) {
  ensureDefaultEnvFile();
  if (!args[0]) {
    ui.blank();
    ui.item('info', `Updates come from the ${currentReleaseChannel()} channel.`);
    ui.note(`Switch with ${ui.paint.command('neorouter channel stable')} or ${ui.paint.command('neorouter channel beta')}.`);
    ui.blank();
    return currentReleaseChannel();
  }
  const nextChannel = parseReleaseChannel(args[0]);
  if (!nextChannel) throw inputError('Usage: neorouter channel [stable|beta]');
  writeReleaseChannelToEnvFile(nextChannel, ENV_FILE);
  process.env.NEOROUTER_RELEASE_CHANNEL = nextChannel;
  logOk(`Updates now come from the ${nextChannel} channel`);
  ui.note(`Get it with ${ui.paint.command('neorouter update')}.`);
  return nextChannel;
}

function printChanges(title, changes) {
  if (changes.length === 0) return;
  ui.section(title);
  for (const change of changes) ui.item('info', change);
}

// Puts the code from before the update back and starts it again.
async function rollBackUpdate(updateGit, before) {
  if (!updateGit.restore(before)) throw new Error('The previous version could not be restored.');
  installDependencies();
  buildBundledWebClientIfPossible();
  await stopAndWait();
  launchService();
  await waitForServerReady(loadEnvPort());
}

// The one update flow: `neorouter update` and the admin UI's Update button
// both run this. `onProgress` receives status patches for the UI.
async function cmdUpdate(args = [], { onProgress = () => {} } = {}) {
  ensureDefaultEnvFile();
  ensureRuntimeDirs();
  if (IN_CONTAINER) {
    // Updating the code in place inside a running image is not a thing: the
    // image is replaced. Say exactly how, instead of pulling git into a layer
    // that the next `docker compose up` discards.
    ui.title('NeoRouter runs from a container image', 'Update the image, not the code inside it:');
    ui.blank();
    ui.print(`  ${ui.paint.command('docker compose pull && docker compose up -d')}`);
    ui.print(`    ${ui.paint.muted('for the published image')}`);
    ui.print(`  ${ui.paint.command('docker compose build --pull && docker compose up -d')}`);
    ui.print(`    ${ui.paint.muted('when built from this checkout')}`);
    ui.blank();
    ui.note(`Data in ${RUNTIME_HOME} is on a volume and survives the update.`);
    ui.blank();
    return { message: 'NeoRouter runs from a container image.' };
  }
  const notesOnly = args.includes('--notes');
  const channelArg = args.find((arg) => !arg.startsWith('--'));
  const requestedChannel = channelArg ? parseReleaseChannel(channelArg) : null;
  if (channelArg && !requestedChannel) throw inputError('Usage: neorouter update [stable|beta] [--notes]');
  const releaseChannel = requestedChannel || currentReleaseChannel();
  if (requestedChannel && !notesOnly) {
    writeReleaseChannelToEnvFile(releaseChannel, ENV_FILE);
    process.env.NEOROUTER_RELEASE_CHANNEL = releaseChannel;
  }
  const versionBefore = readInstalledPackageVersion();
  onProgress({ progress: 5, phase: 'checking', message: 'Checking for a new version', versionBefore });
  const fromGit = isGitCheckout();
  const updateGit = createUpdateGit(runQuiet);
  let targetBranch = null;
  let pending = null;
  let before = null;
  if (fromGit) {
    if (!updateGit.fetchRemote({ timeout: 120_000 })) {
      throw new Error("NeoRouter couldn't reach GitHub to look for updates. Check the internet connection and try again.");
    }
    targetBranch = resolvePreferredGitBranch(releaseChannel);
    pending = updateGit.pendingUpdate(targetBranch, { allowOlder: Boolean(requestedChannel) });
    before = updateGit.head();
  }

  if (notesOnly) {
    ui.blank();
    if (pending) {
      ui.item('warn', ui.paint.bold(`${pending.version || 'A new version'} is ready`), `you have ${versionBefore}`);
      printChanges("What's new", updateGit.changesBetween(before.sha, pending.remoteRef));
      ui.blank();
      ui.note(`Get it with ${ui.paint.command('neorouter update')}.`);
    } else if (fromGit) {
      logOk(`You have the latest version (${versionBefore})`);
    } else {
      logInfo("This install didn't come from Git, so there are no notes to show.");
    }
    ui.blank();
    return { message: pending ? `${pending.version || 'A new version'} is ready` : `You have the latest version (${versionBefore})` };
  }

  // An up-to-date checkout still gets repaired if it was pulled by hand.
  const incomplete = missingDependencies().length > 0 || !hasBundledWebClient(WEB_CLIENT_DIR);
  if (fromGit && !pending && !incomplete) {
    ui.blank();
    logOk(`You have the latest version (${versionBefore})`);
    ui.blank();
    return { message: `You have the latest version (${versionBefore})`, versionBefore, versionAfter: versionBefore };
  }

  const versionAfter = pending?.version || versionBefore;
  onProgress({
    progress: 10, phase: 'preparing', message: 'Preparing the update', versionAfter, targetBranch,
    changelog: pending ? updateGit.changesBetween(before.sha, pending.remoteRef) : [],
  });
  ui.title(
    `Updating ${APP_NAME}`,
    `${versionAfter === versionBefore ? versionBefore : `${versionBefore} → ${versionAfter}`} · ${releaseChannel}`,
  );
  ui.blank();
  const githubInstallRef = releaseChannel === 'beta' ? '#beta' : '';
  const githubInstallSpec = `git+https://github.com/${GITHUB_REPO}.git${githubInstallRef}`;
  const list = createStepList([
    ['Saving a backup of your settings and data', 'Saved a backup of your settings and data'],
    ['Downloading the new version', 'Downloaded the new version'],
    ['Installing it', 'Installed it'],
    ['Restarting NeoRouter', 'Restarted NeoRouter'],
    ['Making sure it all works', 'Made sure it all works'],
  ], { bar: true });
  const phases = ['backup', 'downloading', 'installing', 'restarting', 'verifying'];
  const runUpdateStep = (index, task) => {
    onProgress({ progress: 15 + index * 16, phase: phases[index], message: list.steps[index].doing });
    return list.run(index, task);
  };
  let codeChanged = false;
  try {
    await runUpdateStep(0, () => backupRuntimeData());
    await runUpdateStep(1, () => {
      if (!fromGit) {
        if (!commandExists('npm')) throw new Error('npm is required for global NeoRouter updates.');
        runOrThrow('npm', ['install', '-g', githubInstallSpec, '--force'], { env: withInstallEnv() });
        return;
      }
      codeChanged = true;
      let stashMessage = null;
      try {
        stashMessage = ensureGitBranchForReleaseChannel(targetBranch);
        runOrThrow('git', ['pull', '--rebase', '--autostash', 'origin', targetBranch]);
      } finally {
        restoreStashedUpdateChanges(stashMessage);
      }
    });
    await runUpdateStep(2, () => {
      if (!fromGit || pending || missingDependencies().length > 0) installDependencies();
      buildBundledWebClientIfPossible();
      if (!hasBundledWebClient(WEB_CLIENT_DIR)) {
        throw new Error('No bundled Flutter web client found after update.');
      }
      migrateDatabase();
    });
    await runUpdateStep(3, async () => {
      await stopAndWait();
      launchService();
    });
    await runUpdateStep(4, async () => {
      const port = loadEnvPort();
      await waitForServerReady(port);
      const serving = await fetchRunningVersion(port);
      if (serving && serving !== readInstalledPackageVersion()) {
        throw new Error(`NeoRouter restarted but is still running ${serving}.`);
      }
    });
  } catch (error) {
    if (!codeChanged) {
      closeStepList(list);
      throw error;
    }
    onProgress({ progress: 85, phase: 'rollback', message: `Restoring ${versionBefore} after the update failed` });
    const rollback = list.add([`Going back to ${versionBefore}`, `Went back to ${versionBefore}`]);
    let rollbackError = null;
    try {
      await list.run(rollback, () => rollBackUpdate(updateGit, before));
    } catch (restoreError) {
      rollbackError = restoreError;
    }
    closeStepList(list);
    const failure = new Error(rollbackError
      ? `${error.message}\nGoing back to ${versionBefore} also failed: ${rollbackError.message}`
      : `${error.message}\nNeoRouter is running ${versionBefore} again; nothing was lost.`);
    failure.code = rollbackError ? 'UPDATE_ROLLBACK_FAILED' : 'UPDATE_ROLLED_BACK';
    throw failure;
  }
  closeStepList(list);

  ui.blank();
  ui.item('ok', ui.paint.bold(`${APP_NAME} ${readInstalledPackageVersion()} is ready`), appUrl(loadEnvPort()));
  const changelog = fromGit ? updateGit.changesBetween(before.sha, 'HEAD') : [];
  printChanges("What's new", changelog);
  ui.blank();
  ui.note('Your settings, keys and usage were kept.');
  ui.blank();
  return {
    message: `${APP_NAME} ${readInstalledPackageVersion()} is ready. Your settings, keys and usage were kept.`,
    versionBefore,
    versionAfter: readInstalledPackageVersion(),
    changelog,
  };
}

async function cmdEnv(args = []) {
  ensureDefaultEnvFile();
  const action = (args[0] || '').trim().toLowerCase();
  if (!action) {
    ui.title('Settings', `Saved in ${ENV_FILE}`);
    ui.blank();
    for (const [command, description] of [
      ['env list', 'List all settings (secrets masked)'],
      ['env get KEY', 'Print one setting'],
      ['env set KEY VALUE', 'Change a setting'],
      ['env unset KEY', 'Remove a setting'],
    ]) {
      ui.print(`  ${ui.padEnd(`neorouter ${command}`, 30)}${ui.paint.muted(description)}`);
    }
    ui.blank();
    return;
  }
  if (action === 'list') {
    const env = parseEnv(readEnvFileRaw(ENV_FILE));
    if (env.size === 0) {
      logWarn(`No .env found at ${ENV_FILE}`);
      return;
    }
    for (const [k, v] of [...env.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
      console.log(`${k}=${maskEnvValue(k, v)}`);
    }
    return;
  }
  if (action === 'get') {
    const key = args[1];
    if (!key) throw inputError('Usage: neorouter env get <KEY>');
    const env = parseEnv(readEnvFileRaw(ENV_FILE));
    if (!env.has(key)) throw inputError(`Key not found: ${key}`);
    console.log(env.get(key));
    return;
  }
  if (action === 'set') {
    const key = args[1];
    const value = args.slice(2).join(' ');
    if (!key || !value) throw inputError('Usage: neorouter env set <KEY> <VALUE>');
    validateEnvKey(key);
    upsertEnvValue(ENV_FILE, key, value);
    logOk(`Set ${key} in ${ENV_FILE}`);
    return;
  }
  if (action === 'unset') {
    const key = args[1];
    if (!key) throw inputError('Usage: neorouter env unset <KEY>');
    removeEnvValue(ENV_FILE, key);
    logOk(`Removed ${key} from ${ENV_FILE}`);
    return;
  }
  throw inputError('Usage: neorouter env [list|get|set|unset] ...');
}

function cmdVersion() {
  console.log(currentInstalledVersionLabel());
}

async function cmdFix() {
  ui.title(`Repairing ${APP_NAME}`, 'Your data stays where it is. This can take a few minutes.');
  ui.blank();
  ensureDefaultEnvFile();
  assertSupportedNodeRuntime();
  const steps = [
    [['Reinstalling what it needs', 'Reinstalled what it needs'], () => {
      if (!dependenciesReady()) installDependencies();
    }],
    [['Preparing the app', 'Prepared the app'], () => buildBundledWebClientIfPossible({ required: true })],
    [['Updating the database', 'Updated the database'], migrateDatabase],
  ];
  if (IN_CONTAINER) {
    // No service to reinstall; the container runtime restarts the process.
    await runSteps(steps, { bar: true });
    ui.blank();
    logOk('Repair complete. Restart the container to pick up the changes.');
    ui.blank();
    return;
  }
  steps.push(
    [['Starting NeoRouter', 'Started NeoRouter'], () => installService()],
    [["Checking it's answering", "It's answering"], () => waitForServerReady(loadEnvPort())],
  );
  await runSteps(steps, { bar: true });
  printRunning('Repair complete.');
  ui.note(`Check on it any time with ${ui.paint.command('neorouter status')}.`);
  ui.blank();
}

async function cmdUninstall() {
  if (IN_CONTAINER) {
    printContainerLifecycle('stop');
    return;
  }
  if (ui.isInteractive() && !(await prompt.confirm(
    'Stop NeoRouter and stop it starting with your computer? Your data stays.',
    { initial: false },
  ))) {
    return;
  }
  stopService();
  fs.rmSync(SERVER_PLIST_DST, { force: true });
  if (fs.existsSync(SERVER_SYSTEMD_UNIT)) {
    runQuiet('systemctl', ['--user', 'disable', 'neorouter.service']);
    fs.rmSync(SERVER_SYSTEMD_UNIT, { force: true });
    runQuiet('systemctl', ['--user', 'daemon-reload']);
  }
  ui.blank();
  logOk('NeoRouter no longer starts with your computer');
  ui.note(`Your settings and data are still in ${RUNTIME_HOME}. Turn it back on with ${ui.paint.command('neorouter install')}.`);
  ui.blank();
}

async function cmdBackup() {
  ui.title(`Backing up ${APP_NAME}`);
  ui.blank();
  const [target] = await runSteps([
    [['Saving your settings and data', 'Saved your settings and data'], () => backupRuntimeData()],
  ]);
  ui.blank();
  ui.item('ok', ui.paint.bold('Backup saved'), target);
  ui.blank();
  return target;
}

async function resetPassword(account, password) {
  if (!account || !password || password.length < 12) {
    throw inputError('Usage: neorouter reset-password <username-or-email> <new-password-of-at-least-12-characters>');
  }
  const db = require('../server/db/database').getDatabase();
  require('../server/db/migrate').migrate(db, { quiet: true });
  const user = db.prepare('SELECT * FROM users WHERE username=? COLLATE NOCASE OR email=? COLLATE NOCASE').get(account, account);
  if (!user) throw inputError('User not found.');
  const passwordHash = await require('../server/utils/crypto').hashPassword(password);
  db.transaction(() => {
    db.prepare('UPDATE users SET password_hash=? WHERE id=?').run(passwordHash, user.id);
    db.prepare("UPDATE user_sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id=? AND revoked_at IS NULL").run(user.id);
  })();
  logOk(`Password reset and sessions revoked for ${user.username}.`);
}

function resetTwoFactor(account) {
  if (!account) throw inputError('Usage: neorouter reset-2fa <username-or-email>');
  const db = require('../server/db/database').getDatabase();
  require('../server/db/migrate').migrate(db, { quiet: true });
  const user = db.prepare('SELECT * FROM users WHERE username=? COLLATE NOCASE OR email=? COLLATE NOCASE').get(account, account);
  if (!user) throw inputError('User not found.');
  db.transaction(() => {
    db.prepare('DELETE FROM user_two_factor WHERE user_id=?').run(user.id);
    db.prepare('DELETE FROM user_recovery_codes WHERE user_id=?').run(user.id);
  })();
  logOk(`Two-factor authentication removed for ${user.username}.`);
}

// Adds provider accounts from a JSON file: an array of
// { definitionId, secret, billingMode?, label?, baseUrl?, options? }.
// Providers the user already has are skipped, so the import can be re-run.
async function importProviders(args) {
  const file = args.find((arg) => !arg.startsWith('--'));
  const userIndex = args.indexOf('--user');
  const account = userIndex >= 0 ? args[userIndex + 1] : null;
  if (!file || !account) throw inputError('Usage: neorouter providers import <file.json> --user <username-or-email>');
  const entries = JSON.parse(fs.readFileSync(path.resolve(file), 'utf8'));
  if (!Array.isArray(entries)) throw inputError('The import file must contain a JSON array.');
  const db = require('../server/db/database').getDatabase();
  require('../server/db/migrate').migrate(db, { quiet: true });
  const user = db.prepare('SELECT * FROM users WHERE username=? COLLATE NOCASE OR email=? COLLATE NOCASE').get(account, account);
  if (!user) throw inputError('User not found.');
  const accounts = require('../server/services/providers/account_service');
  const catalog = require('../server/services/catalog/catalog_service');
  ui.title(`Adding providers for ${user.username}`);
  ui.blank();
  let added = 0;
  for (const entry of entries) {
    const exists = db.prepare('SELECT 1 FROM provider_accounts WHERE user_id=? AND definition_id=?').get(user.id, entry.definitionId);
    if (exists) {
      logInfo(`${entry.definitionId}: already connected, skipped`);
      continue;
    }
    try {
      const { account: created, validation } = await accounts.create(user.id, entry);
      const sync = await catalog.syncAccount(created.id).catch((error) => ({ error: error.message }));
      const check = await require('../server/services/providers/billing_check').verifyAccount(created.id);
      if (check) logInfo(`${entry.definitionId}: billing ${check.state} — ${check.message}`);
      added += 1;
      if (sync?.error) logWarn(`${entry.definitionId}: added (${validation.status}), model sync failed: ${sync.error}`);
      else logOk(`${entry.definitionId}: added (${validation.status}), ${sync?.total ?? sync?.models ?? 0} models`);
    } catch (error) {
      logWarn(`${entry.definitionId}: ${error.message}`);
    }
  }
  ui.blank();
  logInfo(`${added} of ${entries.length} providers added.`);
  ui.blank();
}

function homeActions(facts) {
  return [
    facts.running
      ? { label: 'Open NeoRouter in my browser', value: async () => openInBrowser(facts.url) }
      : { label: 'Start NeoRouter', value: cmdStart },
    facts.update && {
      label: 'Get the latest version',
      hint: `${facts.update.version || 'A new version'} is ready`,
      value: () => cmdUpdate(),
    },
    { label: 'Check that everything works', value: () => cmdStatus(['--details']) },
    facts.running && { label: 'Restart NeoRouter', value: cmdRestart },
    { label: "Something's wrong — help me fix it", value: cmdDoctor },
    { label: 'More…', hint: 'every command', value: async () => printHelp() },
  ].filter(Boolean);
}

async function cmdDefault() {
  if (!scanForInstalledInstance().installed) {
    if (fs.existsSync(ENV_FILE)) logInfo(`Your existing settings in ${ENV_FILE} are kept.`);
    await cmdInstall();
    return;
  }
  const facts = await collectStatusFacts({ checkForUpdate: ui.isInteractive() });
  // Running state and updates are in the header and the menu already.
  printHomeHeader(facts, statusChecks(facts)
    .filter((check) => check.id !== 'server' && check.id !== 'version' && needsAttention(check)));
  ui.blank();
  if (!ui.isInteractive()) {
    ui.print(`  ${ui.paint.faint('Every command:')} ${ui.paint.command('neorouter help')}`);
    ui.blank();
    return;
  }
  const action = await prompt.select('What would you like to do?', homeActions(facts), { cancelValue: null });
  if (action) await action();
}

async function runCLI(argv) {
  ensureRuntimeDirs();
  const [command = '', ...args] = argv;
  switch (command) {
    case '':
      return cmdDefault();
    case 'help':
    case '--help':
    case '-h':
      printHelp({ all: args.includes('--all') });
      return;
    case 'install':
      return cmdInstall(args);
    case 'setup':
      return cmdSetup(args);
    case 'start':
      return cmdStart();
    case 'serve':
    case 'run':
      return cmdServe();
    case 'stop':
      return cmdStop();
    case 'restart':
      return cmdRestart();
    case 'status':
      return cmdStatus(args);
    case 'doctor':
      return cmdDoctor();
    case 'logs':
      return cmdLogs();
    case 'backup':
      return cmdBackup();
    case 'rebuild-web':
      return cmdRebuildWeb();
    case 'repair':
    case 'fix':
      return cmdFix(args);
    case 'uninstall':
      return cmdUninstall();
    case 'channel':
      return cmdChannel(args);
    case 'update':
      return cmdUpdate(args);
    case 'env':
      return cmdEnv(args);
    case 'reset-password':
      return resetPassword(args[0], args[1]);
    case 'reset-2fa':
      return resetTwoFactor(args[0]);
    case 'providers':
      if (args[0] === 'import') return importProviders(args.slice(1));
      throw inputError('Usage: neorouter providers import <file.json> --user <username-or-email>');
    case 'version':
    case '--version':
    case '-V':
    case '-v':
      return cmdVersion();
    default:
      throw inputError(`There's no neorouter ${command} command.`);
  }
}

module.exports = {
  renderSystemdUnit,
  runCLI,
  printHelp,
  cmdInstall,
  cmdSetup,
  cmdStart,
  cmdStop,
  cmdRestart,
  cmdStatus,
  cmdDoctor,
  cmdLogs,
  cmdChannel,
  cmdUpdate,
  cmdEnv,
  cmdVersion,
  cmdBackup,
  resetPassword,
  scanForInstalledInstance,
};
