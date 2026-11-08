'use strict';

// Puts the `neorouter` command on the PATH for an install from a git
// checkout (install.sh), which otherwise only has `node bin/neorouter.js`.
// It writes a small launcher into ~/.local/bin, which needs no sudo and is on
// the PATH of most Linux shells; where it is not, one line tells the person
// how to add it for their shell. A global npm install has the command
// already, and a container needs none.
//
// Only a launcher NeoRouter wrote (it carries MARKER) is ever replaced or
// removed: a `neorouter` someone put there themselves is left alone.

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const COMMAND = 'neorouter';
const MARKER = '# Written by `neorouter install`; `neorouter uninstall` removes it.';

function binDir(home = os.homedir()) {
  return path.join(home, '.local', 'bin');
}

/** The launcher: runs this checkout's CLI with this Node, or any Node on the PATH if that one is gone (nvm upgrades). */
function launcherScript(appDir, execPath = process.execPath) {
  const quote = (value) => `'${String(value).replace(/'/g, `'\\''`)}'`;
  return [
    '#!/bin/sh',
    MARKER,
    `NODE=${quote(execPath)}`,
    '[ -x "$NODE" ] || NODE=node',
    `exec "$NODE" ${quote(path.join(appDir, 'bin', 'neorouter.js'))} "$@"`,
    '',
  ].join('\n');
}

function isOurs(file) {
  try {
    return fs.readFileSync(file, 'utf8').includes(MARKER);
  } catch {
    return false;
  }
}

function pathDirs(env = process.env) {
  return String(env.PATH || '').split(path.delimiter).filter(Boolean).map((dir) => path.resolve(dir));
}

/** Whether typing `neorouter` in a new terminal finds a command. */
function commandReachable(env = process.env) {
  return pathDirs(env).some((dir) => {
    try {
      fs.accessSync(path.join(dir, COMMAND), fs.constants.X_OK);
      return true;
    } catch {
      return false;
    }
  });
}

/** One line that adds `dir` to the PATH for the person's shell (from $SHELL). */
function pathHint(dir, { shell = process.env.SHELL || '', home = os.homedir(), platform = process.platform } = {}) {
  const shown = dir.startsWith(`${home}${path.sep}`) ? `~${dir.slice(home.length)}` : dir;
  const name = path.basename(shell);
  if (name === 'fish') return `fish_add_path ${shown}`;
  const exported = `export PATH="${shown.replace(/^~/, '$HOME')}:$PATH"`;
  if (name === 'zsh') return `echo '${exported}' >> ~/.zshrc`;
  if (name === 'bash') return `echo '${exported}' >> ${platform === 'darwin' ? '~/.bash_profile' : '~/.bashrc'}`;
  return exported;
}

/**
 * Writes or refreshes the launcher for this checkout.
 * status: created | updated | unchanged | foreign (someone else's file, left alone) | unsupported (Windows)
 * hint: a line to run when ~/.local/bin is not on the PATH, else null.
 */
function ensureCommandLink({ appDir, home = os.homedir(), env = process.env, execPath = process.execPath, platform = process.platform } = {}) {
  if (platform === 'win32') return { status: 'unsupported', path: null, hint: null };
  const dir = binDir(home);
  const file = path.join(dir, COMMAND);
  const script = launcherScript(appDir, execPath);
  let status = 'created';
  if (fs.existsSync(file) || isSymlink(file)) {
    if (!isOurs(file)) return { status: 'foreign', path: file, hint: null };
    status = fs.readFileSync(file, 'utf8') === script ? 'unchanged' : 'updated';
  }
  if (status !== 'unchanged') {
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(file, script, { mode: 0o755 });
    fs.chmodSync(file, 0o755);
  }
  const onPath = pathDirs(env).includes(path.resolve(dir));
  return { status, path: file, hint: onPath ? null : pathHint(dir, { shell: env.SHELL || '', home, platform }) };
}

function isSymlink(file) {
  try {
    return fs.lstatSync(file).isSymbolicLink();
  } catch {
    return false;
  }
}

/** Removes the launcher if NeoRouter wrote it. True when one was removed. */
function removeCommandLink({ home = os.homedir() } = {}) {
  const file = path.join(binDir(home), COMMAND);
  if (!isOurs(file)) return false;
  fs.rmSync(file, { force: true });
  return true;
}

module.exports = { COMMAND, binDir, launcherScript, commandReachable, pathHint, ensureCommandLink, removeCommandLink };
