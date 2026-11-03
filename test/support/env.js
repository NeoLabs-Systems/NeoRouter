'use strict';

const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');

// supertest calls listen(0), which binds every interface. The system may then
// hand out a port another program holds on 127.0.0.1, and that program answers
// the test's request (an unrelated 401, say). On loopback the port is really free.
// supertest also reads the port right after the call, and listen() with a host
// only binds after a lookup, so this binds directly, as listen() does internally.
const { listen } = http.Server.prototype;
http.Server.prototype.listen = function listenOnLoopback(port, ...rest) {
  if (port !== 0 || (rest.length > 0 && typeof rest[0] !== 'function')) return listen.call(this, port, ...rest);
  if (rest[0]) this.once('listening', rest[0]);
  this._listen2('127.0.0.1', 0, 4, 511);
  return this;
};

// Every test file gets an isolated runtime home and database.
const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-test-'));
process.env.NODE_ENV = 'test';
process.env.NEOROUTER_HOME = home;
process.env.NEOROUTER_WORKERS_ENABLED = 'false';
process.env.NEOROUTER_METADATA_SOURCES_ENABLED = 'false';
process.env.NEOROUTER_LOG_LEVEL = 'error';
// The mock providers listen on loopback; tests of the private-address guard
// switch this off.
process.env.NEOROUTER_ALLOW_PRIVATE_PROVIDER_URLS = 'true';

module.exports = { home };
