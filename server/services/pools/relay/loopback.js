'use strict';

// A relayed request is answered by this NeoRouter itself, exactly as if it
// had come in over the network: through the same app, router-key check,
// share limits and privacy rules. It is handed over on a private listener on
// 127.0.0.1 (opened on first use) that serves the same app. Which requests may
// be handed over at all is decided by the endpoint (endpoint.js), not here.
const http = require('node:http');

let app = null;
let listening = null;
let server = null;

function setApp(value) {
  app = value;
}

function address() {
  if (!app) return Promise.reject(new Error('The relay has no app to answer with.'));
  if (!listening) {
    server = http.createServer(app);
    server.requestTimeout = 0;
    server.headersTimeout = 65_000;
    listening = new Promise((resolve, reject) => {
      server.once('error', reject);
      server.listen(0, '127.0.0.1', () => resolve(server.address()));
    });
    server.unref();
  }
  return listening;
}

/** Sends the request to this NeoRouter; resolves with the response (a readable stream). */
async function dispatch({ method, path, headers, body }, signal) {
  const { port } = await address();
  return new Promise((resolve, reject) => {
    const outgoing = http.request({ host: '127.0.0.1', port, method, path, headers, signal }, resolve);
    outgoing.once('error', reject);
    outgoing.end(body);
  });
}

function closeForTests() {
  if (server) server.close();
  server = null;
  listening = null;
}

module.exports = { setApp, dispatch, closeForTests };
