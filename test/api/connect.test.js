'use strict';

require('../support/env');
const crypto = require('node:crypto');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');

let app;
let session;

const api = (method, path) => request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${session}`);
const verifier = crypto.randomBytes(40).toString('base64url');
const challenge = crypto.createHash('sha256').update(verifier).digest('base64url');
const redirectUri = 'http://127.0.0.1:4555/callback';
const access = { allowFree: true, allowPlan: false, allowPaid: false };

test.before(async () => {
  app = createApp();
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'connect', email: 'connect@example.test', password: 'correct-horse-battery' })).body.session.token;
});

function authorize(extra = {}) {
  return request(app).get('/oauth/authorize').query({ response_type: 'code', client_id: 'neoagent', client_name: 'NeoAgent', redirect_uri: redirectUri, code_challenge: challenge, code_challenge_method: 'S256', state: 'xyz', scope: 'chat live bogus', ...extra });
}

test('the code flow gives the app a working key without copy-paste', async () => {
  const start = await authorize();
  assert.equal(start.status, 302);
  const requestId = new URL(start.headers.location, 'http://x').searchParams.get('connect');
  assert.ok(requestId);

  const shown = await api('get', `/oauth/apps/requests/${requestId}`);
  assert.equal(shown.status, 200, JSON.stringify(shown.body));
  assert.equal(shown.body.request.name, 'NeoAgent');
  assert.deepEqual(shown.body.request.scopes, ['chat', 'live'], 'unknown scopes are dropped');
  assert.deepEqual(shown.body.request.redirect, { host: '127.0.0.1:4555', secure: true });

  const approved = await api('post', `/oauth/apps/requests/${requestId}/approve`).send(access);
  assert.equal(approved.status, 200);
  const back = new URL(approved.body.redirect);
  assert.equal(back.searchParams.get('state'), 'xyz');
  const code = back.searchParams.get('code');

  const wrong = await request(app).post('/oauth/token').type('form').send({ grant_type: 'authorization_code', code, client_id: 'neoagent', redirect_uri: redirectUri, code_verifier: 'x'.repeat(43) });
  assert.equal(wrong.status, 400);
  assert.equal(wrong.body.error, 'invalid_grant');

  // A failed exchange burns the code too.
  const reused = await request(app).post('/oauth/token').type('form').send({ grant_type: 'authorization_code', code, client_id: 'neoagent', redirect_uri: redirectUri, code_verifier: verifier });
  assert.equal(reused.body.error, 'invalid_grant');
});

test('a correct exchange returns a key and where to use it', async () => {
  const start = await authorize({ state: undefined });
  const requestId = new URL(start.headers.location, 'http://x').searchParams.get('connect');
  const approved = await api('post', `/oauth/apps/requests/${requestId}/approve`).send(access);
  const code = new URL(approved.body.redirect).searchParams.get('code');
  const token = await request(app).post('/oauth/token').type('form').send({ grant_type: 'authorization_code', code, client_id: 'neoagent', redirect_uri: redirectUri, code_verifier: verifier });
  assert.equal(token.status, 200, JSON.stringify(token.body));
  assert.match(token.body.access_token, /^nrt_/);
  assert.match(token.body.base_url, /\/v1$/);
  assert.equal(token.headers['access-control-allow-origin'], '*');

  const models = await request(app).get('/v1/models').set('Authorization', `Bearer ${token.body.access_token}`);
  assert.equal(models.status, 200);
  const listed = (await api('get', '/keys')).body.keys.find((key) => key.id === token.body.key_id);
  assert.deepEqual(listed.app, { name: 'NeoAgent', origin: '127.0.0.1:4555', scopes: ['chat', 'live'] });
  assert.equal(listed.allowPlan, false);
});

test('bad requests never redirect to an unchecked address', async () => {
  const bad = await authorize({ redirect_uri: 'javascript:alert(1)' });
  assert.equal(bad.status, 302);
  assert.match(bad.headers.location, /^\/app\/\?connect_error=/);
  const noPkce = await authorize({ code_challenge: undefined });
  assert.equal(new URL(noPkce.headers.location).searchParams.get('error'), 'invalid_request');
});

test('declining sends the app access_denied', async () => {
  const start = await authorize();
  const requestId = new URL(start.headers.location, 'http://x').searchParams.get('connect');
  const denied = await api('post', `/oauth/apps/requests/${requestId}/deny`);
  assert.equal(new URL(denied.body.redirect).searchParams.get('error'), 'access_denied');
  assert.equal((await api('get', `/oauth/apps/requests/${requestId}`)).status, 404);
});

test('the device flow works with a typed code', async () => {
  const device = await request(app).post('/oauth/device/code').send({ client_id: 'neoagent-server', client_name: 'NeoAgent (home)', scope: 'chat transcription' });
  assert.equal(device.status, 200);
  assert.match(device.body.user_code, /^[A-Z]{4}-[A-Z]{4}$/);
  const poll = () => request(app).post('/oauth/token').send({ grant_type: 'urn:ietf:params:oauth:grant-type:device_code', device_code: device.body.device_code, client_id: 'neoagent-server' });
  assert.equal((await poll()).body.error, 'authorization_pending');
  assert.equal((await poll()).body.error, 'slow_down', 'polling faster than the interval is slowed down');

  const typed = device.body.user_code.toLowerCase().replace('-', ' ');
  const shown = await api('get', `/oauth/apps/requests/${encodeURIComponent(typed)}`);
  assert.equal(shown.status, 200);
  assert.equal(shown.body.request.kind, 'device');
  await api('post', `/oauth/apps/requests/${shown.body.request.id}/approve`).send({ allowFree: true, allowPlan: true, allowPaid: true, budgetUsd: 5, budgetPeriod: 'monthly' }).expect(200);

  const token = await poll();
  assert.equal(token.status, 200, JSON.stringify(token.body));
  const listed = (await api('get', '/keys')).body.keys.find((key) => key.id === token.body.key_id);
  assert.equal(listed.budgetUsd, 5);
  assert.equal(listed.app.origin, null);
  assert.equal((await poll()).body.error, 'expired_token', 'the device code works once');
});

test('the server describes itself for OAuth libraries', async () => {
  const meta = await request(app).get('/.well-known/oauth-authorization-server');
  assert.equal(meta.status, 200);
  assert.match(meta.body.token_endpoint, /\/oauth\/token$/);
  assert.deepEqual(meta.body.code_challenge_methods_supported, ['S256']);
});
