'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const net = require('node:net');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { getDatabase } = require('../../server/db/database');
const pool = require('../../server/services/proxies/pool');
const settings = require('../../server/services/settings/settings_service');
const { request: providerRequest } = require('../../server/providers/http');
let app, admin, member, origin, socks, tunnel;
let dropped = 0;
const sockets = new Set();
const servers = [];
const api = (method, path, token = admin) => request(app)[method](`/api/v1/admin/proxies${path}`).set('Authorization', `Bearer ${token}`);
async function listen(server) {
  server.on('connection', (socket) => { sockets.add(socket); socket.on('error', () => {}); socket.on('close', () => sockets.delete(socket)); });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  servers.push(server);
  return server.address().port;
}
function reset() {
  for (const row of pool.list()) pool.remove(row.id);
  settings.setGlobal('outbound_proxy_policy', { enabled: true, fallbackDirect: false });
}
test.before(async () => {
  app = createApp();
  admin = (await request(app).post('/api/v1/auth/register').send({ username: 'proxy-admin', email: 'proxy-admin@example.test', password: 'correct-horse-battery' })).body.session.token;
  settings.setGlobal('registration_open', true);
  member = (await request(app).post('/api/v1/auth/register').send({ username: 'proxy-member', email: 'proxy-member@example.test', password: 'correct-horse-battery' })).body.session.token;
  origin = await listen(http.createServer((req, res) => {
    if (req.url === '/drop') { dropped++; req.socket.destroy(); return; }
    if (req.url === '/stream') { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write('data: hello\n\n'); return; }
    res.writeHead(req.url === '/limited' ? 429 : 200); res.end('through proxy'); }));
  // Real SOCKS5 handshake and TCP forwarding, deliberately fragmented-safe.
  socks = await listen(net.createServer((socket) => {
    let stage = 0, buffer = Buffer.alloc(0);
    const receive = (chunk) => {
      buffer = Buffer.concat([buffer, chunk]);
      if (stage === 0) {
        if (buffer.length < 2 || buffer.length < 2 + buffer[1]) return;
        buffer = buffer.subarray(2 + buffer[1]); socket.write(Buffer.from([5, 0])); stage = 1;
      }
      if (stage !== 1 || buffer.length < 5) return;
      const length = buffer[3] === 1 ? 10 : 7 + buffer[4];
      if (buffer.length < length) return;
      const port = buffer.readUInt16BE(length - 2);
      const host = buffer[3] === 1 ? [...buffer.subarray(4, 8)].join('.') : buffer.subarray(5, length - 2).toString();
      stage = 2; socket.removeListener('data', receive);
      const target = net.connect(port, host, () => {
        socket.write(Buffer.from([5, 0, 0, 1, 127, 0, 0, 1, 0, 0]));
        if (buffer.length > length) target.write(buffer.subarray(length));
        socket.pipe(target).pipe(socket);
      });
      sockets.add(target); target.on('close', () => sockets.delete(target));
      target.on('error', () => socket.destroy()); socket.on('close', () => target.destroy());
    };
    socket.on('data', receive);
  }));
  tunnel = await listen(http.createServer().on('connect', (req, socket, head) => {
    assert.equal(req.headers['proxy-authorization'], `Basic ${Buffer.from('user:secret').toString('base64')}`);
    const target = net.connect(origin, '127.0.0.1', () => { socket.write('HTTP/1.1 200 Connection Established\r\n\r\n'); if (head.length) target.write(head); socket.pipe(target).pipe(socket); });
    sockets.add(target); target.on('close', () => sockets.delete(target));
    target.on('error', () => socket.destroy()); socket.on('close', () => target.destroy());
  }));
});
test.after(async () => { reset(); for (const socket of sockets) socket.destroy(); await Promise.all(servers.map((server) => new Promise((resolve) => server.close(resolve)))); });

test('admin-only bulk import deduplicates formats, rejects bad lines and never returns credentials', async () => {
  reset();
  assert.equal((await api('get', '', member)).status, 403);
  const result = await api('post', '/import').send({ text: `host.test:1080:user:secret\nsocks5://user:secret@host.test:1080\nhost2.test,1080,user,secret\nuser:secret@host3.test:1080\nnot-a-proxy\nftp://host.test:80` });
  assert.equal(result.status, 200);
  assert.equal(pool.parse('socks5://[::1]:1080').endpoint, 'socks5://[::1]:1080');
  assert.throws(() => pool.parse('socks5://user:%zz@host.test:1080'));
  assert.deepEqual(result.body, { added: 3, duplicates: 1, invalidLines: [5, 6] });
  const rows = (await api('get', '')).body.proxies;
  assert.equal(rows.length, 3);
  assert.equal(JSON.stringify(rows).includes('secret'), false);
  for (const row of getDatabase().prepare('SELECT secret_encrypted FROM outbound_proxies').all()) assert.equal(row.secret_encrypted.includes('secret'), false);
  assert.equal((await api('post', `/${rows[0].id}/test`).send({ url: 'http://127.0.0.1/' })).status, 400);
});

test('SOCKS5 forwards requests and provider HTTP errors do not quarantine a healthy proxy', async () => {
  reset(); pool.importLines(`127.0.0.1:${socks}`, 'socks5');
  const result = await pool.fetch(`http://127.0.0.1:${origin}/`, { method: 'POST', body: 'test' });
  assert.equal(await result.text(), 'through proxy');
  const limited = await pool.fetch(`http://127.0.0.1:${origin}/limited`);
  assert.equal(limited.status, 429); await limited.body.cancel();
  assert.equal(pool.list()[0].served, 2); assert.equal(pool.list()[0].failures, 0);
  assert.ok(pool.list()[0].latencyMs !== null);
});

test('HTTP CONNECT sends proxy authentication and preserves the response', async () => {
  reset(); pool.importLines(`http://user:secret@127.0.0.1:${tunnel}`);
  assert.equal(await (await pool.fetch(`http://127.0.0.1:${origin}/`)).text(), 'through proxy');
});

test('failed connection cools down, fails over safely, and can recover after a probe', async () => {
  reset();
  const unavailable = net.createServer(); const closedPort = await listen(unavailable);
  await new Promise((resolve) => unavailable.close(resolve)); servers.splice(servers.indexOf(unavailable), 1);
  pool.importLines(`127.0.0.1:${closedPort}`, 'socks5');
  await assert.rejects(pool.fetch(`http://127.0.0.1:${origin}/`, { method: 'POST', body: 'test' }), /No healthy/);
  assert.ok(pool.list()[0].until > Date.now());
  pool.importLines(`127.0.0.1:${socks}`, 'socks5');
  assert.equal(await (await pool.fetch(`http://127.0.0.1:${origin}/`)).text(), 'through proxy');
  const good = pool.list().find((row) => row.endpoint.endsWith(`:${socks}`));
  assert.equal((await pool.probe(good.id, `http://127.0.0.1:${closedPort}/`)).ok, false);
  assert.ok(pool.list().find((row) => row.id === good.id).until > Date.now());
  assert.equal((await pool.probe(good.id, `http://127.0.0.1:${origin}/`)).ok, true);
  assert.equal(pool.list().find((row) => row.id === good.id).until, 0);
  reset();
  const denied = await listen(http.createServer().on('connect', (_req, socket) => socket.end('HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n')));
  pool.importLines(`http://127.0.0.1:${denied}`);
  settings.setGlobal('outbound_proxy_policy', { enabled: true, fallbackDirect: true });
  assert.equal(await (await pool.fetch(`http://127.0.0.1:${origin}/`, { method: 'POST', body: 'test' })).text(), 'through proxy');
  assert.equal(pool.list()[0].failures, 1, 'failed CONNECT is safe to retry before sending the POST');
});

test('optional direct fallback and private provider bypass respect the configured policy', async () => {
  reset(); const url = `http://127.0.0.1:${origin}/`;
  await assert.rejects(pool.fetch(url), /No healthy/);
  assert.equal((await providerRequest(url)).data, 'through proxy');
  settings.setGlobal('outbound_proxy_policy', { enabled: true, fallbackDirect: true });
  assert.equal(await (await pool.fetch(url)).text(), 'through proxy');
  settings.setGlobal('outbound_proxy_policy', { enabled: false, fallbackDirect: false });
  assert.equal(await (await pool.fetch(url)).text(), 'through proxy');
});

test('streams hold their lease until cancellation and ambiguous POST failures are never replayed', async () => {
  reset(); pool.importLines(`127.0.0.1:${socks}`, 'socks5');
  const stream = await pool.fetch(`http://127.0.0.1:${origin}/stream`);
  assert.equal(pool.list()[0].active, 1);
  const reader = stream.body.getReader();
  assert.match(new TextDecoder().decode((await reader.read()).value), /data: hello/);
  await reader.cancel();
  assert.equal(pool.list()[0].active, 0);
  assert.equal(pool.list()[0].failures, 0);
  settings.setGlobal('outbound_proxy_policy', { enabled: true, fallbackDirect: true });
  await assert.rejects(pool.fetch(`http://127.0.0.1:${origin}/drop`, { method: 'POST', body: 'test' }), /transport failed/);
  assert.equal(dropped, 1, 'direct fallback must not replay a request that reached the origin');
});
