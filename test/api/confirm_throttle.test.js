'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { generateSync } = require('otplib');
const { createApp } = require('../../server/app');
const { getDatabase } = require('../../server/db/database');
const { TOTP_OPTIONS } = require('../../server/services/auth/two_factor_service');

// Asking a signed-in account for its password or a code again is throttled
// per account, apart from signing in.
const PASSWORD = 'correct-horse-battery';
let app;

test.before(async () => {
  app = createApp();
  const admin = await request(app).post('/api/v1/auth/register').send({ username: 'confirmadmin', email: 'confirmadmin@example.test', password: PASSWORD });
  await request(app).patch('/api/v1/admin/settings').set('Authorization', `Bearer ${admin.body.session.token}`).send({ registrationOpen: true });
});

async function register(username) {
  const response = await request(app).post('/api/v1/auth/register').send({ username, email: `${username}@example.test`, password: PASSWORD });
  return { token: response.body.session.token, id: response.body.user.id };
}

const failures = (userId, kind) => getDatabase().prepare('SELECT COUNT(*) count FROM confirm_failures WHERE user_id = ? AND kind = ?').get(userId, kind).count;
const age = (userId, ms) => getDatabase().prepare('UPDATE confirm_failures SET failed_at_ms = failed_at_ms - ? WHERE user_id = ?').run(ms, userId);

test('wrong current passwords are throttled per account, not only per address', async () => {
  const { token, id } = await register('confirmpw');
  const change = (currentPassword) => request(app).put('/api/v1/auth/password').set('Authorization', `Bearer ${token}`)
    .send({ currentPassword, newPassword: 'another-long-passphrase-91' });
  for (let i = 0; i < 5; i += 1) {
    const wrong = await change(`wrong-${i}`);
    assert.equal(wrong.status, 401);
    assert.equal(wrong.body.error.code, 'INVALID_PASSWORD');
  }
  // From the 5th wrong answer the next try waits, even with the right password.
  const locked = await change(PASSWORD);
  assert.equal(locked.status, 429);
  assert.equal(locked.body.error.code, 'CONFIRM_LOCKED');
  assert.ok(locked.body.error.details.retryAfter >= 1);
  assert.equal(locked.headers['retry-after'], String(locked.body.error.details.retryAfter));

  // Counted apart from sign-in: the owner can still sign in.
  const login = await request(app).post('/api/v1/auth/login').send({ account: 'confirmpw', password: PASSWORD });
  assert.equal(login.status, 200);

  // Once the wait is over the right password goes through and clears the count.
  age(id, 60_000);
  const setup = await request(app).post('/api/v1/auth/2fa/setup').set('Authorization', `Bearer ${token}`).send({ currentPassword: PASSWORD });
  assert.equal(setup.status, 200);
  assert.equal(failures(id, 'password'), 0);
});

test('ten wrong answers lock confirmations for the window', async () => {
  const { token, id } = await register('confirmlock');
  const db = getDatabase();
  const now = Date.now();
  const insert = db.prepare('INSERT INTO confirm_failures (user_id, kind, failed_at_ms) VALUES (?, ?, ?)');
  for (let i = 0; i < 10; i += 1) insert.run(id, 'password', now - 10 * 60_000 + i);
  const locked = await request(app).post('/api/v1/auth/account/export').set('Authorization', `Bearer ${token}`).send({ currentPassword: PASSWORD });
  assert.equal(locked.status, 429);
  assert.ok(locked.body.error.details.retryAfter > 200, 'locked until the oldest failure leaves the 15 minutes');
});

test('wrong codes from a session count per account and leave the sign-in code alone', async () => {
  const { token, id } = await register('confirmcode');
  const setup = await request(app).post('/api/v1/auth/2fa/setup').set('Authorization', `Bearer ${token}`).send({ currentPassword: PASSWORD });
  const secret = setup.body.manualKey;
  const code = () => generateSync({ ...TOTP_OPTIONS, secret });
  assert.equal((await request(app).post('/api/v1/auth/2fa/enable').set('Authorization', `Bearer ${token}`).send({ code: code() })).status, 200);

  for (let i = 0; i < 5; i += 1) {
    const wrong = await request(app).post('/api/v1/auth/2fa/recovery-codes').set('Authorization', `Bearer ${token}`)
      .send({ currentPassword: PASSWORD, code: '000000' });
    assert.equal(wrong.status, 401);
    assert.equal(wrong.body.error.code, 'INVALID_TWO_FACTOR');
  }
  assert.equal(failures(id, 'code'), 5);
  const locked = await request(app).post('/api/v1/auth/2fa/recovery-codes').set('Authorization', `Bearer ${token}`)
    .send({ currentPassword: PASSWORD, code: code() });
  assert.equal(locked.status, 429);

  // Signing in with a code still works: the session's guesses did not lock it.
  const first = await request(app).post('/api/v1/auth/login').send({ account: 'confirmcode', password: PASSWORD });
  const second = await request(app).post('/api/v1/auth/login/2fa').send({ challenge: first.body.challenge, code: code() });
  assert.equal(second.status, 200);
});
