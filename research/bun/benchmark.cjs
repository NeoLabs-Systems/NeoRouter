'use strict';

// Controlled local workload. No user runtime, provider credentials or request
// bodies are read or written to result artifacts. Run with Node as driver.
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const net = require('node:net');
const crypto = require('node:crypto');
const { spawn, execFileSync } = require('node:child_process');
const root = path.resolve(__dirname, '../..');
const bun = process.env.BUN_BINARY;
if (!bun) throw new Error('Set BUN_BINARY to the Bun executable to compare.');
const durationMs = Number(process.env.BENCH_DURATION_MS || 600000);
const rps = Number(process.env.BENCH_RPS || 5);
const startupRuns = Number(process.env.BENCH_STARTUP_RUNS || 8);
const output = process.env.BENCH_OUTPUT || path.join(__dirname, 'results.json');
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const stats = (values) => {
  const sorted = [...values].sort((a, b) => a - b);
  return { count: sorted.length, median: sorted[Math.floor(sorted.length * 0.5)], p95: sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * 0.95))], min: sorted[0], max: sorted.at(-1) };
};
const rss = (pid) => Number(execFileSync('ps', ['-o', 'rss=', '-p', String(pid)], { encoding: 'utf8' }).trim()) / 1024;
async function freePort() {
  const server = net.createServer();
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;
  await new Promise((resolve) => server.close(resolve));
  return port;
}
async function start(runtime) {
  const port = await freePort();
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-runtime-bench-'));
  const started = performance.now();
  const child = spawn(runtime, ['server/index.js'], { cwd: root, env: {
    ...process.env, NODE_ENV: 'production', NEOROUTER_HOME: home, NEOROUTER_HOST: '127.0.0.1', NEOROUTER_PORT: String(port),
    NEOROUTER_WORKERS_ENABLED: 'false', NEOROUTER_METADATA_SOURCES_ENABLED: 'false', NEOROUTER_LOG_LEVEL: 'error',
    NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS: 'true', ANALYTICS_ENABLED: 'false',
  }, stdio: ['ignore', 'ignore', 'pipe'] });
  let error = '';
  child.stderr.on('data', (data) => { error += data.toString().slice(0, 2000); });
  const base = `http://127.0.0.1:${port}`;
  for (;;) {
    if (child.exitCode !== null) throw new Error(`Server exited ${child.exitCode}: ${error}`);
    if (performance.now() - started > 15000) { child.kill('SIGKILL'); throw new Error('Readiness timeout'); }
    try { const response = await fetch(`${base}/ready`); if (response.ok) break; } catch {}
    await sleep(5);
  }
  return { child, home, base, startupMs: performance.now() - started };
}
async function stop(server) {
  server.child.kill('SIGTERM');
  await Promise.race([new Promise((resolve) => server.child.once('exit', resolve)), sleep(2000)]);
  if (server.child.exitCode === null) server.child.kill('SIGKILL');
  fs.rmSync(server.home, { recursive: true, force: true });
}
async function json(base, route, body, token) {
  const response = await fetch(`${base}${route}`, { method: body ? 'POST' : 'GET', headers: { 'content-type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) }, ...(body ? { body: JSON.stringify(body) } : {}) });
  const data = await response.json();
  if (!response.ok) throw new Error(`HTTP ${response.status}: ${JSON.stringify(data.error || data)}`);
  return data;
}
async function main() {
  const results = { machine: { platform: os.platform(), arch: os.arch(), cpu: os.cpus()[0].model, ramGiB: os.totalmem() / 2 ** 30 }, node: process.version, bun: execFileSync(bun, ['--version'], { encoding: 'utf8' }).trim(), durationMs, rpsPerRuntime: rps, startupRuns, runtimes: {} };
  for (const name of ['node', 'bun']) results.runtimes[name] = { startup: [], rssSamplesMiB: [], latencyMs: [], errors: 0 };
  for (let index = 0; index < startupRuns; index++) for (const name of index % 2 ? ['bun', 'node'] : ['node', 'bun']) {
    const server = await start(name === 'node' ? process.execPath : bun);
    results.runtimes[name].startup.push(server.startupMs);
    await stop(server);
  }
  const mock = http.createServer(async (req, res) => {
    if (req.url === '/v1/models') { res.setHeader('content-type', 'application/json'); res.end(JSON.stringify({ data: [{ id: 'benchmark-chat', context_length: 32768, supports_tools: true }] })); return; }
    for await (const _ of req) { /* Drain synthetic request without retaining it. */ }
    res.setHeader('content-type', 'application/json');
    res.end(JSON.stringify({ id: 'benchmark', object: 'chat.completion', created: 1, model: 'benchmark-chat', choices: [{ index: 0, message: { role: 'assistant', content: 'ok' }, finish_reason: 'stop' }], usage: { prompt_tokens: 12, completion_tokens: 1, total_tokens: 13 } }));
  });
  await new Promise((resolve) => mock.listen(0, '127.0.0.1', resolve));
  const mockBase = `http://127.0.0.1:${mock.address().port}`;
  const servers = [];
  try {
    for (const name of ['node', 'bun']) {
      const server = await start(name === 'node' ? process.execPath : bun);
      servers.push(server);
      await sleep(1000);
      results.runtimes[name].idleRssMiB = rss(server.child.pid);
      const session = (await json(server.base, '/api/v1/auth/register', { username: 'benchmark', email: 'benchmark@example.test', password: crypto.randomBytes(24).toString('hex') })).session.token;
      await json(server.base, '/api/v1/providers/accounts', { definitionId: 'custom_openai', slug: 'benchmark', baseUrl: `${mockBase}/v1`, billingMode: 'free_tier' }, session);
      server.key = (await json(server.base, '/api/v1/keys', { name: 'benchmark', mode: 'free_only' }, session)).secret;
      server.name = name;
      for (let index = 0; index < 50; index++) await json(server.base, '/v1/chat/completions', { model: 'auto', messages: [{ role: 'user', content: 'ping' }] }, server.key);
    }
    const direct = [];
    for (let index = 0; index < 200; index++) {
      const at = performance.now();
      await json(mockBase, '/v1/chat/completions', { model: 'benchmark-chat', messages: [{ role: 'user', content: 'ping' }] });
      if (index >= 50) direct.push(performance.now() - at);
    }
    results.directMockMs = stats(direct);
    const began = performance.now();
    console.log(JSON.stringify({ phase: 'traffic-started', durationMs, rpsPerRuntime: rps, processes: servers.map((server) => ({ runtime: server.name, pid: server.child.pid })) }));
    let lastSample = -1;
    for (let tick = 0; performance.now() - began < durationMs; tick++) {
      await sleep(Math.max(0, tick * 1000 / rps - (performance.now() - began)));
      await Promise.all(servers.map(async (server) => {
        const at = performance.now();
        try {
          await json(server.base, '/v1/chat/completions', { model: tick % 4 === 0 ? 'benchmark/benchmark-chat' : 'auto', messages: [{ role: 'user', content: 'ping' }] }, server.key);
          results.runtimes[server.name].latencyMs.push(performance.now() - at);
        } catch { results.runtimes[server.name].errors++; }
      }));
      const seconds = Math.floor((performance.now() - began) / 1000);
      if (Math.floor(seconds / 30) > lastSample) {
        lastSample = Math.floor(seconds / 30);
        for (const server of servers) results.runtimes[server.name].rssSamplesMiB.push({ seconds, rss: rss(server.child.pid) });
        fs.writeFileSync(output, JSON.stringify(results, null, 2));
        console.log(JSON.stringify({ phase: 'progress', seconds, counts: servers.map((server) => ({ runtime: server.name, requests: results.runtimes[server.name].latencyMs.length, rssMiB: rss(server.child.pid) })) }));
      }
    }
    for (const server of servers) {
      const data = results.runtimes[server.name];
      data.finalRssMiB = rss(server.child.pid);
      data.startup = stats(data.startup);
      data.latency = stats(data.latencyMs);
      data.medianAddedOverheadMs = data.latency.median - results.directMockMs.median;
      delete data.latencyMs;
    }
    fs.writeFileSync(output, JSON.stringify(results, null, 2));
    console.log(JSON.stringify({ phase: 'complete', output }));
  } finally {
    for (const server of servers) await stop(server);
    await new Promise((resolve) => mock.close(resolve));
  }
}
main().catch((error) => { console.error(error.message); process.exitCode = 1; });
