'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { generateSync } = require('otplib');
const { createApp } = require('../../server/app');
const { TOTP_OPTIONS } = require('../../server/services/auth/two_factor_service');

const PASSWORD = 'correct-horse-battery';
let app;
let session;
let secret;
let recoveryCodes;
const code = () => generateSync({ ...TOTP_OPTIONS, secret });

test.before(async () => {
  app = createApp();
  session = (await request(app).post('/api/v1/auth/register').send({ username: 'tfa', email: 'tfa@example.test', password: PASSWORD })).body.session.token;
});

test('setup needs the current password', async () => {
  const response = await request(app).post('/api/v1/auth/2fa/setup').set('Authorization', `Bearer ${session}`).send({ currentPassword: 'wrong-password-123' });
  assert.equal(response.status, 401);
});

test('enable with an authenticator code returns recovery codes', async () => {
  const setup = await request(app).post('/api/v1/auth/2fa/setup').set('Authorization', `Bearer ${session}`).send({ currentPassword: PASSWORD });
  assert.equal(setup.status, 200);
  assert.match(setup.body.otpauthUrl, /^otpauth:\/\/totp\/NeoRouter/);
  secret = setup.body.manualKey;
  const wrong = await request(app).post('/api/v1/auth/2fa/enable').set('Authorization', `Bearer ${session}`).send({ code: '000000' });
  assert.equal(wrong.status, 401);
  const enabled = await request(app).post('/api/v1/auth/2fa/enable').set('Authorization', `Bearer ${session}`).send({ code: code() });
  assert.equal(enabled.status, 200);
  recoveryCodes = enabled.body.recoveryCodes;
  assert.equal(recoveryCodes.length, 10);
  assert.equal(enabled.body.status.enabled, true);
});

test('login requires the second step', async () => {
  const first = await request(app).post('/api/v1/auth/login').send({ account: 'tfa', password: PASSWORD });
  assert.equal(first.status, 200);
  assert.equal(first.body.twoFactorRequired, true);
  assert.equal(first.body.session, undefined);
  const bad = await request(app).post('/api/v1/auth/login/2fa').send({ challenge: first.body.challenge, code: '123456' });
  assert.equal(bad.status, 401);
  const good = await request(app).post('/api/v1/auth/login/2fa').send({ challenge: first.body.challenge, code: code() });
  assert.equal(good.status, 200);
  assert.match(good.body.session.token, /^nrs_/);
  const reused = await request(app).post('/api/v1/auth/login/2fa').send({ challenge: first.body.challenge, code: code() });
  assert.equal(reused.status, 401, 'a challenge works only once');
});

test('an enabled factor cannot be swapped with only the password', async () => {
  const swap = await request(app).post('/api/v1/auth/2fa/setup').set('Authorization', `Bearer ${session}`).send({ currentPassword: PASSWORD });
  assert.equal(swap.status, 409, JSON.stringify(swap.body));
  assert.equal(swap.body.error.code, 'TWO_FACTOR_ENABLED');
  const first = await request(app).post('/api/v1/auth/login').send({ account: 'tfa', password: PASSWORD });
  const still = await request(app).post('/api/v1/auth/login/2fa').send({ challenge: first.body.challenge, code: code() });
  assert.equal(still.status, 200, 'the original authenticator still works');
});

test('a recovery code works exactly once', async () => {
  const first = await request(app).post('/api/v1/auth/login').send({ account: 'tfa', password: PASSWORD });
  const ok = await request(app).post('/api/v1/auth/login/2fa').send({ challenge: first.body.challenge, code: recoveryCodes[0].toLowerCase() });
  assert.equal(ok.status, 200);
  const again = await request(app).post('/api/v1/auth/login').send({ account: 'tfa', password: PASSWORD });
  const reuse = await request(app).post('/api/v1/auth/login/2fa').send({ challenge: again.body.challenge, code: recoveryCodes[0] });
  assert.equal(reuse.status, 401);
  const statusResponse = await request(app).get('/api/v1/auth/2fa').set('Authorization', `Bearer ${session}`);
  assert.equal(statusResponse.body.recoveryCodesRemaining, 9);
});

test('repeated wrong codes lock the second step', async () => {
  const first = await request(app).post('/api/v1/auth/login').send({ account: 'tfa', password: PASSWORD });
  let last;
  for (let attempt = 0; attempt < 6; attempt += 1) {
    last = await request(app).post('/api/v1/auth/login/2fa').send({ challenge: first.body.challenge, code: '999999' });
  }
  assert.equal(last.status, 429);
  require('../../server/db/database').getDatabase().prepare('UPDATE user_two_factor SET locked_until = 0, failed_attempts = 0').run();
});

test('disable needs password and code', async () => {
  const denied = await request(app).post('/api/v1/auth/2fa/disable').set('Authorization', `Bearer ${session}`).send({ currentPassword: PASSWORD, code: '000000' });
  assert.equal(denied.status, 401);
  const disabled = await request(app).post('/api/v1/auth/2fa/disable').set('Authorization', `Bearer ${session}`).send({ currentPassword: PASSWORD, code: code() });
  assert.equal(disabled.status, 200);
  assert.equal(disabled.body.status.enabled, false);
  const login = await request(app).post('/api/v1/auth/login').send({ account: 'tfa', password: PASSWORD });
  assert.ok(login.body.session.token);
});
