'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');

test('model presets are saved per user and validated', async () => {
  const app = createApp();
  const token = (await request(app).post('/api/v1/auth/register').send({ username: 'presets', email: 'presets@example.test', password: 'correct-horse-battery' })).body.session.token;
  const api = (method, body) => request(app)[method]('/api/v1/model-presets').set('Authorization', `Bearer ${token}`).send(body);
  assert.deepEqual((await api('get')).body.presets, []);
  const presets = [{ id: 'p1', name: 'Smart', models: ['deepseek/deepseek-v4', 'qwen/qwen-max'] }];
  assert.deepEqual((await api('put', { presets })).body.presets, presets);
  assert.deepEqual((await api('get')).body.presets, presets);
  assert.equal((await api('put', { presets: [{ id: 'p2', name: '', models: [] }] })).status, 400);
});
