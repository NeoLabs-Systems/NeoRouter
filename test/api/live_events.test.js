'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const live = require('../../server/services/routing/live_events');

test('live events: snapshot of what is in flight, then events after a sequence, with long polling', async () => {
  live.resetForTests();
  live.started('u', { id: 'r1', key: 'App', task: 'coding' });
  live.attempt('u', { id: 'r1', slug: 'groq', model: 'groq/llama' });
  const first = await live.read('u', 0, 0);
  assert.equal(first.snapshot, true);
  assert.equal(first.inFlight[0].attempts[0].slug, 'groq');
  const waiting = live.read('u', first.seq, 2000);
  setTimeout(() => live.failed('u', { id: 'r1', slug: 'groq' }), 20);
  const next = await waiting;
  assert.deepEqual(next.events.map((event) => event.type), ['fail']);
  live.finished('u', { id: 'r1', success: true, model: 'x/y' });
  const done = await live.read('u', next.seq, 0);
  assert.deepEqual(done.events.map((event) => event.type), ['done']);
  assert.equal((await live.read('u', 0, 0)).inFlight.length, 0);
});
