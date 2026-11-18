'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { EventEmitter } = require('node:events');
const catalog = require('../../server/services/local_models/catalog');
const fit = require('../../server/services/local_models/fit');
const engines = require('../../server/services/local_models/engines');
const { globToRegex } = require('../../server/services/local_models/hf_files');
const { download } = require('../../server/services/local_models/downloader');
const llamaCpp = require('../../server/services/local_models/runtimes/llama_cpp');
const { contextSize } = require('../../server/services/local_models/engines/llama_server');
const { speechParts } = require('../../server/services/local_models/engines/transformers_worker');
const { Runner } = require('../../server/services/local_models/runner');
const starter = require('../../server/services/local_models/starter');

const machine = (overrides = {}) => ({
  platform: 'linux',
  arch: 'x64',
  cpu: { name: 'Test CPU', cores: 8 },
  memory: { totalMb: 16384, limited: false },
  gpu: null,
  disk: { freeMb: 500_000, totalMb: 1_000_000 },
  ...overrides,
});

test('the catalog is valid: unique ids, one model file per llama.cpp entry, voices for speech', () => {
  const entries = catalog.all();
  assert.ok(entries.length >= 20);
  assert.equal(new Set(entries.map((entry) => entry.id)).size, entries.length);
  for (const kind of ['chat', 'moderation', 'embedding', 'transcription', 'speech']) {
    assert.ok(entries.some((entry) => entry.kind === kind && entry.recommended), `a recommended ${kind} model`);
  }
  for (const entry of entries.filter((item) => item.engine === 'llama_cpp')) {
    assert.equal(entry.source.include.length, 1, `${entry.id} loads exactly one model file`);
    assert.match(entry.source.include[0], /\.gguf$/);
  }
  for (const entry of entries.filter((item) => item.kind === 'speech')) assert.ok(entry.voices.length);
});

test('System One models keep the family name System One asks them by', () => {
  const { family } = require('../../server/capabilities/media/system_one');
  for (const entry of catalog.all().filter((item) => item.kind === 'moderation')) assert.notEqual(family(entry.id), 'generic', entry.id);
});

test('file patterns stay inside one folder', () => {
  assert.ok(globToRegex('*.json').test('config.json'));
  assert.ok(!globToRegex('*.json').test('onnx/config.json'));
  assert.ok(globToRegex('onnx/*').test('onnx/model.onnx_data'));
  assert.ok(globToRegex('Qwen3.5-4B-Q4_K_M.gguf').test('Qwen3.5-4B-Q4_K_M.gguf'));
  assert.ok(!globToRegex('Qwen3.5-4B-Q4_K_M.gguf').test('Qwen3.5-4BxQ4_K_M.gguf'));
});

test('fit: small models run anywhere, big ones need memory, a graphics card makes them great', () => {
  const tiny = catalog.get('qwen3.5-0.8b');
  const big = catalog.get('gpt-oss-120b');
  const mid = catalog.get('qwen3.5-9b');
  assert.equal(fit.assess(tiny, machine()).rating, 'good');
  assert.equal(fit.assess(big, machine()).rating, 'too_big');
  assert.equal(fit.assess(mid, machine()).rating, 'slow');
  assert.equal(fit.assess(mid, machine({ gpu: { type: 'cuda', name: 'RTX', memoryMb: 24576, cuda: '13.0' } })).rating, 'great');
  // Running models leave less room for the next one.
  assert.equal(fit.assess(mid, machine(), { usedMb: 8000 }).fitsNow, false);
  assert.equal(fit.assess(mid, machine({ disk: { freeMb: 2000, totalMb: 10_000 } })).diskOk, false);
});

test('fit: measured memory beats the estimate, and what else runs leaves less room', () => {
  const mid = catalog.get('qwen3.5-9b');
  const estimate = fit.assess(mid, machine()).memoryMb;
  const measured = fit.assess(mid, machine(), { measuredMb: 5000 });
  assert.equal(measured.memoryMb, 5000);
  assert.equal(measured.measured, true);
  assert.ok(estimate > 5000);
  // Without measurements only the fixed reserve is kept.
  assert.equal(fit.roomMb(machine()), 16384 - fit.RESERVE_MB);
  // Other programs using 9 GB on average leave 16 - 9 - 1 GB of headroom.
  const busy = machine({ memory: { totalMb: 16384, limited: false, othersMb: 9216 } });
  assert.equal(fit.roomMb(busy), 16384 - 9216 - fit.HEADROOM_MB);
  // Fits the machine, but not beside what it normally runs: tight, still installable.
  const tight = fit.assess(mid, busy);
  assert.equal(tight.rating, 'tight');
  assert.equal(tight.fitsNow, false);
  assert.equal(fit.assess(catalog.get('gpt-oss-120b'), busy).rating, 'too_big');
  assert.equal(fit.assess(catalog.get('qwen3.5-0.8b'), busy).rating, 'good');
});

test('memory: averages what was measured, per model and for everything else', () => {
  const memory = require('../../server/services/local_models/memory');
  memory.setForTests({ others: 6000, models: { 'qwen3.5-4b': 3300 } });
  assert.equal(memory.othersMb(), 6000);
  assert.equal(memory.modelMb('qwen3.5-4b'), 3300);
  assert.equal(memory.modelMb('whisper-base'), null);
  memory.forget('qwen3.5-4b');
  assert.equal(memory.modelMb('qwen3.5-4b'), null);
  memory.setForTests(null);
});

test('memory: measures this machine', async () => {
  const memory = require('../../server/services/local_models/memory');
  memory.setForTests(null);
  await memory.refresh();
  const others = memory.othersMb();
  assert.ok(others > 0 && others < os.totalmem() / (1024 * 1024), `${others} MB`);
  memory.setForTests(null);
});

test('fit: speech models work everywhere, text models only where llama.cpp runs', () => {
  const windows = machine({ platform: 'win32' });
  assert.equal(fit.assess(catalog.get('whisper-base'), windows).rating, 'great');
  assert.equal(fit.assess(catalog.get('qwen3.5-0.8b'), windows).rating, 'unsupported');
});

test('llama.cpp builds: GPU first where the driver allows, CPU as the fallback', () => {
  const ids = (hardware) => llamaCpp.variants(hardware).map((variant) => variant.id);
  assert.deepEqual(ids(machine({ platform: 'darwin', arch: 'arm64' })), ['macos-arm64']);
  assert.deepEqual(ids(machine()), ['ubuntu-x64']);
  assert.deepEqual(ids(machine({ gpu: { type: 'cuda', cuda: '13.0' } })), ['ubuntu-cuda-13.5-x64', 'ubuntu-x64']);
  assert.deepEqual(ids(machine({ gpu: { type: 'cuda', cuda: '12.4' } })), ['ubuntu-cuda-12.8-x64', 'ubuntu-x64']);
  assert.deepEqual(ids(machine({ arch: 'arm64' })), ['ubuntu-arm64']);
  assert.deepEqual(ids(machine({ platform: 'win32' })), []);
  const cuda = llamaCpp.variants(machine({ gpu: { type: 'cuda', cuda: '13.0' } }))[0];
  assert.equal(cuda.assets.length, 2, 'CUDA builds bring their runtime libraries');
});

test('context grows with memory; embeddings keep their own size', () => {
  assert.equal(contextSize(catalog.get('qwen3.5-4b'), machine()), 8192);
  assert.equal(contextSize(catalog.get('qwen3.5-4b'), machine({ memory: { totalMb: 32768 } })), 16384);
  assert.equal(contextSize(catalog.get('qwen3.5-4b'), machine({ memory: { totalMb: 131072 } })), 32768);
  assert.equal(contextSize(catalog.get('nomic-embed-text-v1.5'), machine()), 8192);
});

test('long speech is split into parts at sentence ends', () => {
  const text = `${'This is a sentence. '.repeat(40)}${'word '.repeat(120)}`;
  const parts = speechParts(text, 300);
  assert.ok(parts.length > 3);
  assert.ok(parts.every((part) => part.length <= 300));
  assert.equal(parts.join(' ').replace(/\s+/g, ' ').trim(), text.replace(/\s+/g, ' ').trim());
});

test('downloads resume where they stopped and are checked against their hash', async () => {
  const body = crypto.randomBytes(300_000);
  const sha256 = crypto.createHash('sha256').update(body).digest('hex');
  const ranges = [];
  const server = http.createServer((req, res) => {
    const range = req.headers.range?.match(/bytes=(\d+)-/);
    ranges.push(range ? Number(range[1]) : 0);
    if (range) {
      res.writeHead(206, { 'Content-Length': body.length - Number(range[1]) });
      res.end(body.subarray(Number(range[1])));
    } else {
      res.writeHead(200, { 'Content-Length': body.length });
      res.end(body);
    }
  });
  await new Promise((resolve) => { server.listen(0, '127.0.0.1', resolve); });
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'nr-download-'));
  try {
    const dest = path.join(dir, 'model.bin');
    fs.writeFileSync(`${dest}.part`, body.subarray(0, 100_000));
    let seen = 0;
    const file = { url: `http://127.0.0.1:${server.address().port}/model.bin`, size: body.length, sha256 };
    await download(file, dest, { onBytes: (delta) => { seen += delta; } });
    assert.deepEqual(ranges, [100_000]);
    assert.equal(seen, body.length);
    assert.ok(fs.readFileSync(dest).equals(body));
    assert.ok(!fs.existsSync(`${dest}.part`));
    // A complete file is kept as it is.
    await download(file, dest);
    assert.equal(ranges.length, 1);
  } finally {
    server.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

// A model process that can be crashed on purpose.
function fakeEngine() {
  const started = [];
  return {
    started,
    support: () => ({ ok: true }),
    prepare: async () => {},
    async start() {
      const proc = new EventEmitter();
      proc.exited = false;
      proc.stop = async () => {
        proc.exited = true;
        proc.emit('exit', { code: 0, signal: null });
      };
      proc.crash = () => {
        proc.exited = true;
        proc.emit('exit', { code: null, signal: 'SIGKILL' });
      };
      started.push(proc);
      return { proc, health: async () => true, gpu: false, contextWindow: 8192 };
    },
  };
}

const settle = () => new Promise((resolve) => { setTimeout(resolve, 30); });

test('a crashed model is started again; repeated crashes give up and ask for a repair', async (t) => {
  const engine = fakeEngine();
  const original = engines.engineFor;
  engines.engineFor = () => engine;
  t.after(() => { engines.engineFor = original; });
  const runner = new Runner('fake', async () => ({ entry: { engine: 'fake' } }), { backoffMs: [5, 5, 5] });
  let gaveUp = false;
  runner.on('gave-up', () => { gaveUp = true; });
  runner.start();
  await settle();
  assert.equal(runner.state, 'running');
  engine.started.at(-1).crash();
  assert.equal(runner.state, 'restarting');
  assert.equal(runner.problem.code, 'crashed');
  await settle();
  assert.equal(runner.state, 'running');
  assert.equal(engine.started.length, 2);
  for (let index = 0; index < 3; index += 1) {
    engine.started.at(-1).crash();
    await settle();
  }
  assert.equal(runner.state, 'failed');
  assert.ok(gaveUp);
  await runner.stop();
  assert.equal(runner.state, 'stopped');
});

test('stopping a model ends its process and does not restart it', async (t) => {
  const engine = fakeEngine();
  const original = engines.engineFor;
  engines.engineFor = () => engine;
  t.after(() => { engines.engineFor = original; });
  const runner = new Runner('fake', async () => ({ entry: { engine: 'fake' } }), { backoffMs: [5] });
  runner.start();
  await settle();
  await runner.stop();
  await settle();
  assert.equal(runner.state, 'stopped');
  assert.equal(engine.started.length, 1);
  assert.ok(engine.started[0].exited);
});

test('the starter set is a text model, speech recognition and a voice that fit together', () => {
  const fits = (machineSpec) => {
    const set = starter.suggest(machineSpec);
    const budget = fit.roomMb(machineSpec);
    assert.ok(set.reduce((sum, item) => sum + item.memoryMb, 0) <= budget);
    return set;
  };
  const mac = fits(machine({ platform: 'darwin', arch: 'arm64', gpu: { type: 'metal', name: 'Apple GPU', memoryMb: 11468 } }));
  assert.deepEqual(mac.map((item) => item.kind), ['chat', 'transcription', 'speech']);
  assert.equal(mac[0].id, 'qwen3.5-4b', 'the recommended text model when it runs well');
  // Without a graphics card the recommended model is slow here, so a smaller one is chosen.
  const cpu = fits(machine());
  assert.equal(cpu[0].kind, 'chat');
  assert.notEqual(cpu[0].id, 'qwen3.5-4b');
  // A tiny machine still gets what fits; Windows gets speech only.
  assert.ok(fits(machine({ memory: { totalMb: 4096 } })).length >= 2);
  assert.deepEqual(fits(machine({ platform: 'win32' })).map((item) => item.kind), ['transcription', 'speech']);
});

test('only Whisper models are offered for translation', () => {
  const { LocalRuntimeAdapter } = require('../../server/providers/adapters/local_runtime');
  const adapter = new LocalRuntimeAdapter(require('../../server/providers/registry').get('neorouter_local'), { options: {} });
  assert.equal(adapter.canTranslate('whisper-small'), true);
  assert.equal(adapter.canTranslate('moonshine-base'), false);
});
