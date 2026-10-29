'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');

test('GET /api serves the gateway reference as Markdown with this server\'s origin', async () => {
  const response = await request(createApp()).get('/api').set('Host', 'router.example.test');
  assert.equal(response.status, 200);
  assert.match(response.headers['content-type'], /^text\/markdown/);
  assert.match(response.text, /^# NeoRouter API/);
  assert.ok(response.text.includes('http://router.example.test/v1/chat/completions'));
  assert.ok(response.text.includes('ws://router.example.test/v1/realtime'));
  for (const section of ['## Authentication', '## Choosing a model', '## Errors', '## Getting a key from an app (OAuth)']) assert.ok(response.text.includes(section), section);
});
