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
  choosePreferredBranchForChannel,
  describeReleaseChannelPolicy,
  parseReleaseChannel,
  readConfiguredReleaseChannel,
  writeReleaseChannelToEnvFile,
} = require('../runtime/release_channel');
const { createGitHelpers } = require('../runtime/git_helpers');
const { detectContainer } = require('../runtime/container');

const APP_NAME = 'NeoRouter';
const GITHUB_REPO = 'NeoLabs-Systems/NeoRouter';
const DEFAULT_PORT = 4600;

// Inside a container the image is the unit of deployment: no launchd or user
// systemd to install, no checkout to pull, and the process must stay in the
// foreground so the container runtime can supervise it.
const IN_CONTAINER = detectContainer();

loadEnvironment();

function runQuiet(cmd, args, options = {}) {
  return spawnSync(cmd, args, {
    encoding: 'utf8',
    cwd: options.cwd || APP_DIR,
    env: options.env || process.env,
    stdio: options.stdio || ['ignore', 'pipe', 'pipe'],
    timeout: options.timeout,
  });
}

function runOrThrow(cmd, args, options = {}) {
  const result = spawnSync(cmd, args, {
    encoding: 'utf8',
    cwd: options.cwd || APP_DIR,
    env: options.env || process.env,
    stdio: options.stdio || 'inherit',
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(`${cmd} ${args.join(' ')} failed with exit code ${result.status}`);
  }
  return result;
}

const {
  latestStableGitTagVersion,
  latestBetaGitTagVersion,
  gitWorkingTreeDirty,
  gitStashPush,
  gitStashPopByMessage,
  gitLocalBranchExists,
  gitRemoteBranchExists,
} = createGitHelpers((cmd, args) => runQuiet(cmd, args));

const COLORS = process.stdout.isTTY
  ? {
      reset: '\x1b[0m',
      bold: '\x1b[1m',
      red: '\x1b[1;31m',
      green: '\x1b[1;32m',
      yellow: '\x1b[1;33m',
      cyan: '\x1b[1;36m',
      dim: '\x1b[2m',
    }
  : { reset: '', bold: '', red: '', green: '', yellow: '', cyan: '', dim: '' };

function logInfo(msg) {
  const mark = process.stdout.isTTY ? `${COLORS.cyan}◇${COLORS.reset}` : '->';
  console.log(`  ${mark} ${msg}`);
}

function logOk(msg) {
  const mark = process.stdout.isTTY ? `${COLORS.green}◆${COLORS.reset}` : 'ok';
  console.log(`  ${mark} ${msg}`);
}

function logWarn(msg) {
  const mark = process.stdout.isTTY ? `${COLORS.yellow}▲${COLORS.reset}` : 'warn';
  console.warn(`  ${mark} ${msg}`);
}

function heading(text) {
  console.log('');
  if (process.stdout.isTTY) console.log(`${COLORS.bold}${COLORS.cyan}${text}${COLORS.reset}`);
  else console.log(text);
}

function cliBanner(title = APP_NAME, subtitle = 'inference routing for every model') {
  if (!process.stdout.isTTY) {
    console.log(`\n${title}\n${subtitle}\n`);
    return;
  }
  const c = COLORS;
  const width = 42;
  const stripAnsi = (text) => String(text).replace(/\x1b\[[0-9;]*m/g, '');
  const boxLine = (content) => {
    const padding = Math.max(0, width - stripAnsi(content).length);
    console.log(`  ${c.cyan}│${c.reset} ${content}${' '.repeat(padding)} ${c.cyan}│${c.reset}`);
  };
  console.log('');
  console.log(`  ${c.cyan}╭────────────────────────────────────────────╮${c.reset}`);
  boxLine(`${c.bold}NeoRouter${c.reset} ${c.dim}inference router${c.reset}`);
  boxLine(`${c.bold}${title}${c.reset} ${c.dim}${subtitle}${c.reset}`);
  console.log(`  ${c.cyan}╰────────────────────────────────────────────╯${c.reset}`);
}

function cliSection(text) {
  if (process.stdout.isTTY) console.log(`${COLORS.dim}  ──${COLORS.reset} ${COLORS.bold}${text}${COLORS.reset}`);
  else console.log(`-- ${text}`);
}

function statusLine(ok, label, value, hint = '') {
  const mark = ok
    ? (process.stdout.isTTY ? `${COLORS.green}●${COLORS.reset}` : 'ok')
    : (process.stdout.isTTY ? `${COLORS.yellow}●${COLORS.reset}` : 'warn');
  const suffix = hint ? ` ${COLORS.dim}${hint}${COLORS.reset}` : '';
  console.log(`  ${mark} ${String(label).padEnd(10)} ${value}${suffix}`);
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
    return require(path.join(APP_DIR, 'package.json')).version || '0.0.0';
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
  const normalized = parseReleaseChannel(channel) || currentReleaseChannel();
  const hasMain = gitRemoteBranchExists('main');
  const hasBeta = gitRemoteBranchExists('beta');
  if (normalized === 'stable') {
    if (hasMain) return 'main';
    if (hasBeta) {
      logWarn('Stable channel selected, but origin/main is missing; using origin/beta until a stable branch exists.');
      return 'beta';
    }
    throw new Error('No origin/main or origin/beta branch was found for updates.');
  }
  const preferred = choosePreferredBranchForChannel(normalized, {
    stable: latestStableGitTagVersion(),
    beta: latestBetaGitTagVersion(),
  });
  if (preferred === 'beta') {
    if (hasBeta) return 'beta';
    if (hasMain) return 'main';
  } else if (hasMain) {
    return 'main';
  } else if (hasBeta) {
    return 'beta';
  }
  throw new Error(`Release channel branch "${preferred}" was not found on origin.`);
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
  logOk(`Runtime backup saved at ${target}`);
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
  logWarn('Native modules did not load; rebuilding them');
  // npm 12 takes the allowed scripts from package.json (allowScripts) and
  // rejects the command-line flag in a project; older npm runs them anyway.
  runQuiet('npm', ['rebuild', ...NATIVE_DEPENDENCIES], {
    cwd: APP_DIR,
    env: withInstallEnv(),
    stdio: 'inherit',
  });
  if (nativeDependenciesLoad()) {
    logOk('Native modules rebuilt');
    return;
  }
  throw new Error(`The database module could not be installed for Node.js ${process.versions.node}. `
    + `Install the build tools (${buildToolsHint()}) and run the installer again.`);
}

function installDependencies() {
  heading('Dependencies');
  runOrThrow('npm', ['install', '--omit=dev', '--no-audit', '--no-fund'], {
    env: withInstallEnv(),
  });
  if (!nativeDependenciesLoad()) repairNativeDependencies();
  logOk('Dependencies installed');
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
  logOk(`Node.js ${process.versions.node}`);
}

function buildBundledWebClientIfPossible({ required = false } = {}) {
  heading('Web Client');
  return sharedBuildWebClient({
    flutterAppDir: FLUTTER_APP_DIR,
    webClientDir: WEB_CLIENT_DIR,
    runCommand: (command, args, options = {}) =>
      runQuiet(command, args, options.stdio ? options : { ...options, stdio: 'inherit' }),
    commandExistsFn: commandExists,
    channel: currentReleaseChannel(),
    toolsDir: path.join(RUNTIME_HOME, 'tools'),
    onMissingSources: () => logWarn('Flutter app sources not found; keeping existing bundled web client'),
    onUsingBundledClient: () => logOk('Using bundled Flutter web client'),
    onMissingFlutter: () => logInfo('Flutter SDK not found; downloading the prebuilt web client'),
    onDownloaded: () => logOk('Prebuilt web client installed'),
    onInstallingFlutter: () => logInfo('No prebuilt web client available; installing the Flutter SDK (a few minutes, once)'),
    onBuildSuccess: () => logOk('Bundled Flutter web client updated'),
    onBuildFailed: () => logWarn('Flutter web build failed; keeping existing bundled web client'),
    fail: (message) => { throw new Error(message); },
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
  return `[Unit]
Description=NeoRouter inference router
After=network-online.target

[Service]
Type=simple
WorkingDirectory=${quoteSystemdValue(APP_DIR)}
ExecStart=${quoteSystemdValue(process.execPath)} ${quoteSystemdValue(serverEntry)}
Restart=always
RestartSec=2
Environment=NEOROUTER_HOME=${runtime.home}
Environment=NODE_ENV=production
EnvironmentFile=-${quoteSystemdValue(ENV_FILE)}
StandardOutput=append:${path.join(runtime.logs, 'neorouter.log')}
StandardError=append:${path.join(runtime.logs, 'neorouter-error.log')}

[Install]
WantedBy=default.target
`;
}

function startFallback() {
  const runtime = ensureRuntimeDirs();
  if (fs.existsSync(PID_FILE)) {
    const existingPid = Number(fs.readFileSync(PID_FILE, 'utf8'));
    if (Number.isFinite(existingPid) && existingPid > 0) {
      try {
        process.kill(existingPid, 0);
        logOk(`NeoRouter is already running with process ${existingPid}`);
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
  logOk(`Started detached process (pid ${child.pid})`);
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
  logOk(`launchd service loaded (${SERVER_SERVICE_LABEL})`);
}

function installLinuxService() {
  ensureRuntimeDirs();
  fs.mkdirSync(path.dirname(SERVER_SYSTEMD_UNIT), { recursive: true });
  fs.writeFileSync(SERVER_SYSTEMD_UNIT, renderSystemdUnit(), { mode: 0o600 });
  runOrThrow('systemctl', ['--user', 'daemon-reload']);
  runOrThrow('systemctl', ['--user', 'enable', 'neorouter.service']);
  runOrThrow('systemctl', ['--user', 'start', 'neorouter.service']);
  runOrThrow('systemctl', ['--user', 'is-active', '--quiet', 'neorouter.service']);
  logOk('systemd user service installed and started');
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
      logWarn(`systemd setup failed (${error.message}); falling back to detached process`);
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
  throw new Error(`NeoRouter did not become reachable on port ${port}.`);
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
  heading('Database');
  require('../server/db/migrate').migrate();
  logOk('Schema is current');
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
  cliBanner(`Install ${APP_NAME}`, 'guided bootstrap');
  heading(`Install ${APP_NAME}`);
  ensureDefaultEnvFile();
  assertSupportedNodeRuntime();
  if (!dependenciesReady()) installDependencies();
  else logOk('Runtime dependencies are ready');
  if (!hasBundledWebClient(WEB_CLIENT_DIR)) buildBundledWebClientIfPossible({ required: true });
  else logOk('Bundled web client is ready');
  migrateDatabase();
  if (IN_CONTAINER) {
    // Nothing to install: the image already holds the code and the client, and
    // the container runtime supervises the process. Run the server in the
    // foreground so this is `docker run … neorouter install` and it stays up.
    logOk('Container detected: starting the server in the foreground');
    return cmdServe();
  }
  const manager = installService();
  const port = loadEnvPort();
  await waitForServerReady(port);
  logOk(`Running with ${manager} on http://localhost:${port}`);
  logInfo(`Open http://localhost:${port}/ for the site and /app for the dashboard.`);
  logInfo('Sign in at /app, add your providers and create a router key. The OpenAI-compatible endpoint is /v1.');
}

async function cmdSetup(args = []) {
  cliBanner(`Setup ${APP_NAME}`, 'runtime preparation');
  heading(`Setup ${APP_NAME}`);
  ensureDefaultEnvFile();
  assertSupportedNodeRuntime();
  const channelArg = args.find((arg) => arg === 'stable' || arg === 'beta')
    || (args.includes('--channel') ? args[args.indexOf('--channel') + 1] : null)
    || args.find((arg) => arg.startsWith('--channel='))?.split('=')[1];
  if (channelArg) {
    const channel = parseReleaseChannel(channelArg);
    if (!channel) throw new Error('Usage: neorouter setup [--channel stable|beta]');
    writeReleaseChannelToEnvFile(channel, ENV_FILE);
    process.env.NEOROUTER_RELEASE_CHANNEL = channel;
    logOk(`Release channel set to ${releaseChannelSummary(channel)}`);
  } else {
    logInfo(`Using release channel ${releaseChannelSummary(currentReleaseChannel())}`);
  }
  if (!dependenciesReady()) installDependencies();
  migrateDatabase();
  if (!hasBundledWebClient(WEB_CLIENT_DIR)) buildBundledWebClientIfPossible({ required: true });
  else logOk('Bundled web client is ready');
  logOk(`Setup completed in ${RUNTIME_HOME}`);
  if (IN_CONTAINER) {
    logInfo('Run `neorouter serve` to start the server in the foreground.');
    return;
  }
  logInfo('Run `neorouter install` to install or update the background service.');
}

function cmdStart() {
  cliBanner(`Start ${APP_NAME}`, 'boot sequence');
  heading(`Start ${APP_NAME}`);
  ensureDefaultEnvFile();
  if (!hasBundledWebClient(WEB_CLIENT_DIR)) buildBundledWebClientIfPossible({ required: true });
  if (IN_CONTAINER) {
    // A container has exactly one foreground process; "starting" means running
    // it here instead of handing off to a service manager that is not there.
    logInfo('Container detected: running in the foreground');
    return cmdServe();
  }
  const platform = detectPlatform();
  if (platform === 'macos' && fs.existsSync(SERVER_PLIST_DST)) {
    installMacService();
    return;
  }
  if (platform === 'linux' && fs.existsSync(SERVER_SYSTEMD_UNIT)) {
    runOrThrow('systemctl', ['--user', 'start', 'neorouter.service']);
    runOrThrow('systemctl', ['--user', 'is-active', '--quiet', 'neorouter.service']);
    logOk('systemd start requested');
    return;
  }
  startFallback();
}

function cmdServe() {
  heading(`Serve ${APP_NAME}`);
  ensureDefaultEnvFile();
  require('../server/index.js');
}

function cmdStop() {
  heading(`Stop ${APP_NAME}`);
  if (IN_CONTAINER) {
    // The server is PID 1's child in this container and nothing else can stop
    // it from inside; the container runtime owns the lifecycle.
    logInfo('NeoRouter runs in a container: stop it with `docker stop <container>`');
    return;
  }
  const platform = detectPlatform();
  if (platform === 'macos' && fs.existsSync(SERVER_PLIST_DST)) {
    const domain = launchctlDomain();
    if (domain) {
      runQuiet('launchctl', ['bootout', domain, SERVER_PLIST_DST]);
      runQuiet('launchctl', ['bootout', launchctlServiceTarget()]);
    }
    runQuiet('launchctl', ['unload', SERVER_PLIST_DST]);
    logOk('launchd stop requested');
  } else if (platform === 'linux' && fs.existsSync(SERVER_SYSTEMD_UNIT)) {
    runQuiet('systemctl', ['--user', 'stop', 'neorouter.service']);
    logOk('systemd stop requested');
  } else if (fs.existsSync(PID_FILE)) {
    const pid = Number(fs.readFileSync(PID_FILE, 'utf8').trim());
    if (Number.isFinite(pid) && pid > 0) {
      try {
        process.kill(pid, 'SIGTERM');
        logOk(`Stopped pid ${pid}`);
      } catch {
        logWarn(`pid ${pid} was not running (stale PID file)`);
      }
    }
    fs.rmSync(PID_FILE, { force: true });
  } else {
    logWarn('No running process found');
  }
}

function cmdRestart() {
  heading(`Restart ${APP_NAME}`);
  buildBundledWebClientIfPossible();
  cmdStop();
  cmdStart();
}

async function cmdRebuildWeb() {
  heading('Rebuild Flutter Web Client');
  buildBundledWebClientIfPossible({ required: true });
}

async function cmdStatus({ showBanner = true } = {}) {
  if (showBanner) cliBanner(`${APP_NAME} Status`, 'systems sweep');
  heading(`${APP_NAME} Status`);
  const port = loadEnvPort();
  const running = await isPortOpen(port);
  const releaseChannel = currentReleaseChannel();
  const platform = detectPlatform();
  cliSection('Runtime');
  statusLine(running, 'server', running ? `http://localhost:${port}` : `not reachable on port ${port}`);
  if (IN_CONTAINER) {
    statusLine(true, 'manager', 'container', 'docker/compose owns the lifecycle');
  } else if (platform === 'macos' && fs.existsSync(SERVER_PLIST_DST)) {
    const svcRes = runQuiet('launchctl', ['list', SERVER_SERVICE_LABEL]);
    statusLine(
      svcRes.status === 0 && Boolean(svcRes.stdout.trim()),
      'service',
      svcRes.status === 0 && svcRes.stdout.trim() ? `launchd (${SERVER_SERVICE_LABEL})` : 'launchd unit not loaded',
      svcRes.status === 0 && svcRes.stdout.trim() ? '' : 'run: neorouter install',
    );
  } else if (platform === 'linux' && fs.existsSync(SERVER_SYSTEMD_UNIT)) {
    const svcRes = runQuiet('systemctl', ['--user', 'is-active', 'neorouter.service']);
    statusLine(
      svcRes.status === 0 && svcRes.stdout.trim() === 'active',
      'service',
      svcRes.status === 0 && svcRes.stdout.trim() === 'active' ? 'systemd (neorouter)' : 'systemd unit not active',
      svcRes.status === 0 ? '' : 'run: neorouter install',
    );
  }
  cliSection('Assets');
  statusLine(fs.existsSync(ENV_FILE), 'config', fs.existsSync(ENV_FILE) ? ENV_FILE : '.env not found', fs.existsSync(ENV_FILE) ? '' : 'run: neorouter setup');
  statusLine(
    hasBundledWebClient(WEB_CLIENT_DIR),
    'web',
    hasBundledWebClient(WEB_CLIENT_DIR) ? 'bundled Flutter client present' : 'no bundled client',
    hasBundledWebClient(WEB_CLIENT_DIR) ? '' : 'run: neorouter rebuild-web',
  );
  cliSection('Build');
  console.log(`  install   ${APP_DIR}`);
  console.log(`  version   ${currentInstalledVersionLabel()}`);
  console.log(`  channel   ${releaseChannelSummary(releaseChannel)}`);
}

async function cmdDoctor() {
  cliBanner(`${APP_NAME} Doctor`, 'read-only diagnostics');
  heading(`${APP_NAME} Doctor`);
  const port = loadEnvPort();
  let runtimeWritable = true;
  try { fs.accessSync(RUNTIME_HOME, fs.constants.R_OK | fs.constants.W_OK); } catch { runtimeWritable = false; }
  const checks = [
    { id: 'runtime', ok: runtimeWritable, message: runtimeWritable ? 'Runtime data directory is accessible' : 'Runtime data directory is not writable', action: 'Check permissions for ~/.neorouter' },
    { id: 'config', ok: fs.existsSync(ENV_FILE), message: fs.existsSync(ENV_FILE) ? 'Configuration is present' : 'Configuration is missing', action: 'Run `neorouter setup`' },
    { id: 'deps', ok: dependenciesReady(), message: dependenciesReady() ? 'Runtime dependencies are present' : 'Runtime dependencies are incomplete', action: 'Run `neorouter repair`' },
    { id: 'web', ok: hasBundledWebClient(WEB_CLIENT_DIR), message: hasBundledWebClient(WEB_CLIENT_DIR) ? 'Bundled client is present' : 'Bundled client is missing', action: 'Run `neorouter rebuild-web`' },
    { id: 'node', ok: Number(String(process.versions.node || '').split('.')[0]) >= 20, message: `Node.js ${process.versions.node}`, action: 'Install Node.js 20 or newer' },
    { id: 'server', ok: await isPortOpen(port), message: '', action: 'Run `neorouter start` or `neorouter repair`' },
  ];
  const serverCheck = checks.find((check) => check.id === 'server');
  serverCheck.message = serverCheck.ok
    ? `NeoRouter is reachable on port ${port}`
    : `NeoRouter is not reachable on port ${port}`;
  for (const check of checks) statusLine(check.ok, check.id, check.message, check.ok ? '' : check.action);
}

function cmdLogs() {
  heading('Logs');
  ensureRuntimeDirs();
  const log = path.join(LOG_DIR, 'neorouter.log');
  const err = path.join(LOG_DIR, 'neorouter-error.log');
  if (!fs.existsSync(log) && !fs.existsSync(err)) {
    console.log('No logs yet.');
    return;
  }
  if (commandExists('tail')) {
    const files = [log, err].filter((file) => fs.existsSync(file));
    runOrThrow('tail', ['-n', '80', '-f', ...files], { cwd: APP_DIR });
    return;
  }
  for (const file of [log, err]) {
    if (!fs.existsSync(file)) continue;
    console.log(`\n-- ${file}`);
    process.stdout.write(fs.readFileSync(file, 'utf8'));
  }
}

function cmdChannel(args = []) {
  heading('Release Channel');
  ensureDefaultEnvFile();
  if (!args[0]) {
    console.log(`  configured ${releaseChannelSummary(currentReleaseChannel())}`);
    return currentReleaseChannel();
  }
  const nextChannel = parseReleaseChannel(args[0]);
  if (!nextChannel) throw new Error('Usage: neorouter channel [stable|beta]');
  writeReleaseChannelToEnvFile(nextChannel, ENV_FILE);
  process.env.NEOROUTER_RELEASE_CHANNEL = nextChannel;
  logOk(`Release channel set to ${releaseChannelSummary(nextChannel)}`);
  return nextChannel;
}

async function cmdUpdate(args = []) {
  heading(`Update ${APP_NAME}`);
  ensureDefaultEnvFile();
  ensureRuntimeDirs();
  if (IN_CONTAINER) {
    // Updating the code in place inside a running image is not a thing: the
    // image is replaced. Say exactly how, instead of pulling git into a layer
    // that the next `docker compose up` discards.
    logInfo('NeoRouter runs from a container image; update the image, not the code inside it:');
    logInfo('  docker compose pull && docker compose up -d      (published image)');
    logInfo('  docker compose build --pull && docker compose up -d   (built from this checkout)');
    logInfo(`Data in ${RUNTIME_HOME} is on a volume and survives the update.`);
    return;
  }
  const requestedChannel = args[0] ? parseReleaseChannel(args[0]) : null;
  if (args[0] && !requestedChannel) throw new Error('Usage: neorouter update [stable|beta]');
  const releaseChannel = requestedChannel || currentReleaseChannel();
  if (requestedChannel) {
    writeReleaseChannelToEnvFile(releaseChannel, ENV_FILE);
    process.env.NEOROUTER_RELEASE_CHANNEL = releaseChannel;
    logOk(`Release channel set to ${releaseChannelSummary(releaseChannel)}`);
  }
  const versionBefore = currentInstalledVersionLabel();
  const githubInstallRef = releaseChannel === 'beta' ? '#beta' : '';
  const githubInstallSpec = `git+https://github.com/${GITHUB_REPO}.git${githubInstallRef}`;

  if (fs.existsSync(path.join(APP_DIR, '.git')) && commandExists('git')) {
    const current = runQuiet('git', ['rev-parse', '--short', 'HEAD']);
    runOrThrow('git', ['fetch', 'origin', '--tags', '--force']);
    const targetBranch = resolvePreferredGitBranch(releaseChannel);
    logInfo(`Using git branch ${targetBranch} for the ${releaseChannel} channel.`);
    let stashMessage = null;
    try {
      stashMessage = ensureGitBranchForReleaseChannel(targetBranch);
      backupRuntimeData();
      runOrThrow('git', ['pull', '--rebase', '--autostash', 'origin', targetBranch]);
      const next = runQuiet('git', ['rev-parse', '--short', 'HEAD']);
      if (current.status === 0 && next.status === 0 && current.stdout.trim() !== next.stdout.trim()) {
        logOk(`Updated ${current.stdout.trim()} -> ${next.stdout.trim()}`);
        installDependencies();
        buildBundledWebClientIfPossible();
      } else {
        logOk('Already up to date');
        const missing = missingDependencies();
        if (missing.length) {
          logInfo(`Installing missing dependencies: ${missing.join(', ')}`);
          installDependencies();
        }
        buildBundledWebClientIfPossible();
      }
    } finally {
      restoreStashedUpdateChanges(stashMessage);
    }
  } else {
    logWarn(`No git repo detected; installing from ${githubInstallSpec}.`);
    if (!commandExists('npm')) throw new Error('npm is required for global NeoRouter updates.');
    backupRuntimeData();
    runOrThrow('npm', ['install', '-g', githubInstallSpec, '--force'], { env: withInstallEnv() });
    logOk('npm global update completed from GitHub');
  }

  if (!hasBundledWebClient(WEB_CLIENT_DIR)) {
    throw new Error('No bundled Flutter web client found after update.');
  }
  migrateDatabase();
  cmdRestart();
  logOk(`Installed version ${versionBefore} -> ${currentInstalledVersionLabel()}`);
}

async function cmdEnv(args = []) {
  heading('Environment Variables');
  ensureDefaultEnvFile();
  const action = (args[0] || '').trim().toLowerCase();
  if (!action) {
    console.log('Usage: neorouter env <subcommand>');
    console.log('');
    console.log('  neorouter env list            List all variables (secrets masked)');
    console.log('  neorouter env get KEY         Print a single variable');
    console.log('  neorouter env set KEY VALUE   Set a variable');
    console.log('  neorouter env unset KEY       Remove a variable');
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
    if (!key) throw new Error('Usage: neorouter env get <KEY>');
    const env = parseEnv(readEnvFileRaw(ENV_FILE));
    if (!env.has(key)) throw new Error(`Key not found: ${key}`);
    console.log(env.get(key));
    return;
  }
  if (action === 'set') {
    const key = args[1];
    const value = args.slice(2).join(' ');
    if (!key || !value) throw new Error('Usage: neorouter env set <KEY> <VALUE>');
    validateEnvKey(key);
    upsertEnvValue(ENV_FILE, key, value);
    logOk(`Set ${key} in ${ENV_FILE}`);
    return;
  }
  if (action === 'unset') {
    const key = args[1];
    if (!key) throw new Error('Usage: neorouter env unset <KEY>');
    removeEnvValue(ENV_FILE, key);
    logOk(`Removed ${key} from ${ENV_FILE}`);
    return;
  }
  throw new Error('Usage: neorouter env [list|get|set|unset] ...');
}

function cmdVersion() {
  console.log(currentInstalledVersionLabel());
}

async function cmdFix() {
  cliBanner(`Fix ${APP_NAME}`, 'reset and recover');
  heading(`Repair ${APP_NAME}`);
  ensureDefaultEnvFile();
  assertSupportedNodeRuntime();
  if (!dependenciesReady()) installDependencies();
  else logOk('Runtime dependencies are ready');
  buildBundledWebClientIfPossible({ required: true });
  migrateDatabase();
  if (IN_CONTAINER) {
    // No service to reinstall; the container runtime restarts the process.
    logOk('Repair complete. Restart the container to pick up the changes.');
    return;
  }
  installService();
  await waitForServerReady(loadEnvPort());
  logOk('Repair complete');
}

function cmdUninstall() {
  heading(`Uninstall service for ${APP_NAME}`);
  cmdStop();
  if (fs.existsSync(SERVER_PLIST_DST)) {
    fs.rmSync(SERVER_PLIST_DST, { force: true });
    logOk(`Removed ${SERVER_PLIST_DST}`);
  }
  if (fs.existsSync(SERVER_SYSTEMD_UNIT)) {
    runQuiet('systemctl', ['--user', 'disable', 'neorouter.service']);
    fs.rmSync(SERVER_SYSTEMD_UNIT, { force: true });
    runQuiet('systemctl', ['--user', 'daemon-reload']);
    logOk(`Removed ${SERVER_SYSTEMD_UNIT}`);
  }
  logInfo(`Runtime data at ${RUNTIME_HOME} was preserved.`);
}

function cmdBackup() {
  heading(`Backup ${APP_NAME}`);
  const target = backupRuntimeData();
  logOk(`Backup written to ${target}`);
  return target;
}

async function resetPassword(account, password) {
  if (!account || !password || password.length < 12) {
    throw new Error('Usage: neorouter reset-password <username-or-email> <new-password-of-at-least-12-characters>');
  }
  const db = require('../server/db/database').getDatabase();
  require('../server/db/migrate').migrate(db);
  const user = db.prepare('SELECT * FROM users WHERE username=? COLLATE NOCASE OR email=? COLLATE NOCASE').get(account, account);
  if (!user) throw new Error('User not found.');
  const passwordHash = await require('../server/utils/crypto').hashPassword(password);
  db.transaction(() => {
    db.prepare('UPDATE users SET password_hash=? WHERE id=?').run(passwordHash, user.id);
    db.prepare("UPDATE user_sessions SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id=? AND revoked_at IS NULL").run(user.id);
  })();
  logOk(`Password reset and sessions revoked for ${user.username}.`);
}

function resetTwoFactor(account) {
  if (!account) throw new Error('Usage: neorouter reset-2fa <username-or-email>');
  const db = require('../server/db/database').getDatabase();
  require('../server/db/migrate').migrate(db);
  const user = db.prepare('SELECT * FROM users WHERE username=? COLLATE NOCASE OR email=? COLLATE NOCASE').get(account, account);
  if (!user) throw new Error('User not found.');
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
  if (!file || !account) throw new Error('Usage: neorouter providers import <file.json> --user <username-or-email>');
  const entries = JSON.parse(fs.readFileSync(path.resolve(file), 'utf8'));
  if (!Array.isArray(entries)) throw new Error('The import file must contain a JSON array.');
  const db = require('../server/db/database').getDatabase();
  require('../server/db/migrate').migrate(db);
  const user = db.prepare('SELECT * FROM users WHERE username=? COLLATE NOCASE OR email=? COLLATE NOCASE').get(account, account);
  if (!user) throw new Error('User not found.');
  const accounts = require('../server/services/providers/account_service');
  const catalog = require('../server/services/catalog/catalog_service');
  heading(`Import providers for ${user.username}`);
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
  logInfo(`${added} of ${entries.length} providers added.`);
}

async function cmdDefault() {
  cliBanner('Welcome', 'installation scan');
  heading('Scan This Computer');
  logInfo('Checking NeoRouter runtime data, system services, and bundled client…');
  const scan = scanForInstalledInstance();
  if (scan.installed) {
    logOk('Existing NeoRouter installation found');
    const locations = scan.evidence.map((item) => item.path).filter(Boolean);
    if (locations.length > 0) logInfo(`Detected ${locations.join(', ')}`);
    const port = loadEnvPort();
    const healthy = fs.existsSync(ENV_FILE)
      && dependenciesReady()
      && hasBundledWebClient(WEB_CLIENT_DIR)
      && await isPortOpen(port);
    if (!healthy) {
      logWarn('The installation needs attention.');
      await cmdDoctor();
      logInfo('Run `neorouter repair` after reviewing the diagnostic checks.');
      return;
    }
    await cmdStatus({ showBanner: false });
    console.log('');
    logInfo('Run `neorouter --help` to see all commands.');
    return;
  }
  logInfo('No installed NeoRouter instance was found. Starting first-time setup.');
  if (fs.existsSync(ENV_FILE)) logInfo(`Existing configuration at ${ENV_FILE} will be preserved.`);
  await cmdInstall();
}

function printHelp() {
  console.log(`NeoRouter ${currentInstalledVersionLabel()}`);
  console.log('');
  console.log(`  Run ${COLORS.bold}neorouter${COLORS.reset} without a command to detect or install NeoRouter.`);
  console.log('');
  const row = (cmd, desc) => console.log(`  ${cmd.padEnd(28)} ${desc}`);
  cliSection('Lifecycle');
  row('install', 'Guided bootstrap, dependencies, web client, service');
  row('start', 'Start the server service');
  row('stop', 'Stop the server service');
  row('restart', 'Stop, then start');
  row('serve', 'Run the server in the foreground');
  row('status', 'Health overview (server and config)');
  row('doctor', 'Read-only setup and runtime diagnostics');
  row('logs', 'Tail server logs');
  row('backup', 'Snapshot config and data');
  row('repair', 'Repair dependencies, web client, and service');
  row('fix', 'Alias for repair');
  row('uninstall', 'Remove the user service (keeps data)');
  console.log('');
  cliSection('Configuration');
  row('setup', 'Prepare database, env, and web client');
  row('setup --channel beta|stable', 'Setup and set release channel');
  row('env list|get|set|unset', 'Manage ~/.neorouter/.env');
  row('channel', 'Show current release channel');
  row('channel stable|beta', 'Switch release channel');
  console.log('');
  cliSection('Updates');
  row('update', 'Update to latest on current channel');
  row('update stable|beta', 'Update and switch channel');
  console.log('');
  cliSection('Maintenance');
  row('rebuild-web', 'Rebuild the bundled Flutter web client');
  row('reset-password <user> <pw>', 'Reset a local account password');
  row('reset-2fa <user>', 'Remove two-factor authentication from an account');
  row('providers import <file> --user <u>', 'Add provider keys from a JSON file');
  row('version', 'Print installed version');
  row('help', 'Show this help');
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
      printHelp();
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
      return cmdStatus();
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
      throw new Error('Usage: neorouter providers import <file.json> --user <username-or-email>');
    case 'version':
    case '--version':
    case '-V':
    case '-v':
      return cmdVersion();
    default:
      throw new Error(`Unknown command: ${command}. Run "neorouter --help" for usage.`);
  }
}

module.exports = {
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
