'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');

test('a test server is on loopback with its port known at once', async () => {
  const server = http.createServer((_req, res) => res.end('ok'));
  const listening = new Promise((resolve) => { server.listen(0, resolve); });
  const { address, port } = server.address();
  assert.equal(address, '127.0.0.1');
  assert.ok(port > 0);
  await listening;
  assert.equal(await (await fetch(`http://127.0.0.1:${port}/`)).text(), 'ok');
  await new Promise((resolve) => { server.close(resolve); });
});
