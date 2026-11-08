'use strict';

// A model process NeoRouter started: its output is kept only until it is
// ready (enough to explain a failed start, never prompts), it is ended with
// NeoRouter, and its pid is written down so that the next start can end it
// if NeoRouter itself crashed.
const fs = require('node:fs');
const net = require('node:net');
const { spawn, fork, execFileSync } = require('node:child_process');
const { EventEmitter } = require('node:events');
const paths = require('../paths');

const TAIL_LINES = 40;
const STOP_GRACE_MS = 10_000;
const live = new Set();

function readPids() {
  try { return JSON.parse(fs.readFileSync(paths.PROCESSES_FILE, 'utf8')); } catch { return {}; }
}

function writePids(pids) {
  try {
    paths.ensureDirs();
    fs.writeFileSync(paths.PROCESSES_FILE, JSON.stringify(pids));
  } catch { /* best effort: only used to clean up after a crash */ }
}

function remember(pid, marker) {
  writePids({ ...readPids(), [pid]: marker });
}

function forget(pid) {
  const pids = readPids();
  delete pids[pid];
  writePids(pids);
}

// The command line of a pid, to be sure it is still ours before ending it.
function commandOf(pid) {
  try {
    if (process.platform === 'linux') return fs.readFileSync(`/proc/${pid}/cmdline`, 'utf8').replace(/\0/g, ' ');
    return execFileSync('ps', ['-p', String(pid), '-o', 'command='], { encoding: 'utf8', timeout: 5000 });
  } catch {
    return '';
  }
}

/** Ends model processes a crashed NeoRouter left behind. */
function endOrphans() {
  for (const [pid, marker] of Object.entries(readPids())) {
    if (commandOf(pid).includes(marker)) {
      try { process.kill(Number(pid), 'SIGKILL'); } catch { /* already gone */ }
    }
  }
  writePids({});
}

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.unref();
    server.on('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address();
      server.close(() => resolve(port));
    });
  });
}

class ManagedProcess extends EventEmitter {
  constructor(child, marker) {
    super();
    this.child = child;
    this.tail = [];
    this.capturing = true;
    this.exited = false;
    live.add(this);
    if (child.pid) remember(child.pid, marker);
    const capture = (chunk) => {
      if (!this.capturing) return;
      this.tail.push(...String(chunk).split('\n').filter(Boolean));
      if (this.tail.length > TAIL_LINES) this.tail.splice(0, this.tail.length - TAIL_LINES);
    };
    child.stdout?.on('data', capture);
    child.stderr?.on('data', capture);
    child.on('error', (error) => {
      this.tail.push(error.message);
      this.finish(null, null);
    });
    child.on('exit', (code, signal) => this.finish(code, signal));
  }

  finish(code, signal) {
    if (this.exited) return;
    this.exited = true;
    live.delete(this);
    if (this.child.pid) forget(this.child.pid);
    this.emit('exit', { code, signal });
  }

  // Called once the model answers: from then on nothing it prints is kept.
  ready() {
    this.capturing = false;
    this.tail = [];
  }

  output() {
    return this.tail.join('\n');
  }

  stop() {
    if (this.exited) return Promise.resolve();
    return new Promise((resolve) => {
      const timer = setTimeout(() => this.child.kill('SIGKILL'), STOP_GRACE_MS);
      this.once('exit', () => {
        clearTimeout(timer);
        resolve();
      });
      this.child.kill('SIGTERM');
    });
  }
}

function start(command, args, { cwd, env, marker }) {
  return new ManagedProcess(spawn(command, args, { cwd, env, stdio: ['ignore', 'pipe', 'pipe'] }), marker);
}

function startNode(script, { env, marker }) {
  return new ManagedProcess(fork(script, [], { env, serialization: 'advanced', stdio: ['ignore', 'pipe', 'pipe', 'ipc'] }), marker);
}

// Model processes never outlive NeoRouter.
process.on('exit', () => {
  for (const managed of live) {
    try { managed.child.kill('SIGKILL'); } catch { /* already gone */ }
  }
});

module.exports = { start, startNode, freePort, endOrphans };
