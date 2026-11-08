'use strict';

// llama.cpp's server, downloaded from its official releases for this machine:
// Metal on Apple Silicon, CUDA where an NVIDIA driver is present, the CPU
// build everywhere else. A build that fails to run (an old driver, a missing
// library) is marked broken and the next one is used, so a GPU problem falls
// back to the CPU instead of leaving the admin stuck.
const fs = require('node:fs');
const path = require('node:path');
const { execFile } = require('node:child_process');
const { promisify } = require('node:util');
const paths = require('../paths');
const { download } = require('../downloader');

const execFileAsync = promisify(execFile);

// The pinned build: tested with every model in the catalog.
const BUILD = 'b12202';
const RELEASE_API = `https://api.github.com/repos/ggml-org/llama.cpp/releases/tags/${BUILD}`;
const DIR = path.join(paths.RUNTIMES_DIR, 'llama.cpp');
const STATE_FILE = path.join(DIR, 'state.json');
const BINARY = 'llama-server';

const cudaMajor = (hardware) => Number(String(hardware.gpu?.cuda || '').split('.')[0]) || 0;

/** The builds to try on this machine, best first; empty when none exists. */
function variants(hardware) {
  const cuda = hardware.gpu?.type === 'cuda' ? cudaMajor(hardware) : 0;
  const key = `${hardware.platform}-${hardware.arch}`;
  const cudaBuild = (arch) => (cuda >= 13 ? [`ubuntu-cuda-13.5-${arch}`] : cuda >= 12 && arch === 'x64' ? ['ubuntu-cuda-12.8-x64'] : []);
  const list = {
    'darwin-arm64': ['macos-arm64'],
    'darwin-x64': ['macos-x64'],
    'linux-x64': [...cudaBuild('x64'), 'ubuntu-x64'],
    'linux-arm64': [...cudaBuild('arm64'), 'ubuntu-arm64'],
  }[key] || [];
  return list.map((id) => ({
    id,
    gpu: id.startsWith('macos-arm64') || id.includes('cuda'),
    assets: [`llama-${BUILD}-bin-${id}.tar.gz`, ...(id.includes('cuda') ? [`cudart-llama-${BUILD}-bin-${id}.tar.gz`] : [])],
  }));
}

function readState() {
  try { return JSON.parse(fs.readFileSync(STATE_FILE, 'utf8')); } catch { return { broken: [] }; }
}

function writeState(state) {
  fs.mkdirSync(DIR, { recursive: true });
  fs.writeFileSync(STATE_FILE, JSON.stringify(state));
}

const variantDir = (id) => path.join(DIR, `${BUILD}-${id}`);
const binaryOf = (id) => path.join(variantDir(id), BINARY);

function usable(hardware) {
  const broken = new Set(readState().broken);
  return variants(hardware).filter((variant) => !broken.has(variant.id));
}

function support(hardware) {
  if (!variants(hardware).length) return { ok: false, reason: 'unsupported_system' };
  return { ok: true, reason: null };
}

async function releaseAssets(signal) {
  const response = await fetch(RELEASE_API, { headers: { 'User-Agent': 'NeoRouter', Accept: 'application/vnd.github+json' }, signal });
  if (!response.ok) throw new Error(`GitHub answered ${response.status} for llama.cpp ${BUILD}.`);
  const release = await response.json();
  return new Map((release.assets || []).map((asset) => [asset.name, {
    url: asset.browser_download_url,
    size: asset.size,
    sha256: String(asset.digest || '').replace(/^sha256:/, '') || null,
  }]));
}

// Releases wrap their files in one folder; its contents go into `target`.
async function extract(archive, target) {
  const staging = `${target}.unpack`;
  fs.rmSync(staging, { recursive: true, force: true });
  fs.mkdirSync(staging, { recursive: true });
  await execFileAsync('tar', ['-xzf', archive, '-C', staging], { timeout: 300_000 });
  const entries = fs.readdirSync(staging);
  const root = entries.length === 1 && fs.statSync(path.join(staging, entries[0])).isDirectory() ? path.join(staging, entries[0]) : staging;
  fs.mkdirSync(target, { recursive: true });
  for (const name of fs.readdirSync(root)) fs.renameSync(path.join(root, name), path.join(target, name));
  fs.rmSync(staging, { recursive: true, force: true });
}

// Whether the build starts on this machine at all.
async function works(id) {
  try {
    await execFileAsync(binaryOf(id), ['--version'], { timeout: 30_000, cwd: variantDir(id), env: environment(id) });
    return true;
  } catch {
    return false;
  }
}

function environment(id) {
  const env = { ...process.env };
  if (process.platform === 'linux') env.LD_LIBRARY_PATH = [variantDir(id), process.env.LD_LIBRARY_PATH].filter(Boolean).join(':');
  return env;
}

async function install(variant, { onBytes, signal }) {
  const assets = await releaseAssets(signal);
  const target = variantDir(variant.id);
  const incoming = `${target}.incoming`;
  fs.rmSync(incoming, { recursive: true, force: true });
  for (const name of variant.assets) {
    const asset = assets.get(name);
    if (!asset) throw new Error(`llama.cpp ${BUILD} has no ${name}.`);
    const archive = path.join(DIR, 'downloads', name);
    await download(asset, archive, { onBytes, signal });
    await extract(archive, incoming);
    fs.rmSync(archive, { force: true });
  }
  fs.rmSync(target, { recursive: true, force: true });
  fs.renameSync(incoming, target);
}

/** Bytes still to download for the build ensure() would use (0 when ready). */
async function pendingBytes(hardware, signal) {
  const variant = usable(hardware)[0];
  if (!variant || fs.existsSync(binaryOf(variant.id))) return 0;
  const assets = await releaseAssets(signal);
  return variant.assets.reduce((sum, name) => sum + (assets.get(name)?.size || 0), 0);
}

// One install at a time: two models downloading at once share the build.
let queue = Promise.resolve();
function serialized(task) {
  const run = queue.then(task, task);
  queue = run.catch(() => {});
  return run;
}

/**
 * The build to run models with, installing it first when needed. Builds that
 * cannot start here are marked broken and skipped. → { id, gpu, binary, dir, env }
 */
function ensure(hardware, options = {}) {
  return serialized(() => ensureNow(hardware, options));
}

async function ensureNow(hardware, { onBytes = () => {}, signal } = {}) {
  for (const variant of usable(hardware)) {
    if (!fs.existsSync(binaryOf(variant.id)) || !(await works(variant.id))) {
      await install(variant, { onBytes, signal });
    }
    if (await works(variant.id)) return { id: variant.id, gpu: variant.gpu, binary: binaryOf(variant.id), dir: variantDir(variant.id), env: environment(variant.id) };
    markBroken(variant.id);
  }
  throw new Error('llama.cpp does not run on this machine.');
}

// A build that installed but cannot run models here (e.g. CUDA with an old driver).
function markBroken(id) {
  const state = readState();
  if (!state.broken.includes(id)) writeState({ ...state, broken: [...state.broken, id] });
  fs.rmSync(variantDir(id), { recursive: true, force: true });
}

// Repairs give every build another chance (a driver may have been updated).
function clearBroken() {
  writeState({ ...readState(), broken: [] });
}

function status(hardware) {
  const variant = usable(hardware).find((item) => fs.existsSync(binaryOf(item.id)));
  return { build: BUILD, installed: variant?.id || null, gpu: Boolean(variant?.gpu), broken: readState().broken };
}

module.exports = { BUILD, variants, support, ensure, pendingBytes, markBroken, clearBroken, status };
