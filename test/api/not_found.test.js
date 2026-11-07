'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');

test('an unknown page in a browser gets the 404 page, API clients keep the JSON error', async () => {
  const app = createApp();
  const page = await request(app).get('/no/such/page').set('Accept', 'text/html,application/xhtml+xml,*/*;q=0.8');
  assert.equal(page.status, 404);
  assert.match(page.headers['content-type'], /^text\/html/);
  assert.match(page.text, /None go here/);

  for (const accept of ['application/json', '*/*']) {
    const api = await request(app).get('/no/such/page').set('Accept', accept);
    assert.equal(api.status, 404, accept);
    assert.equal(api.body.error.code, 'NOT_FOUND', accept);
  }
  for (const url of ['/v1/nope', '/api/v1/nope']) {
    const api = await request(app).get(url).set('Accept', 'text/html');
    assert.match(api.headers['content-type'], /json/, url);
  }
});
