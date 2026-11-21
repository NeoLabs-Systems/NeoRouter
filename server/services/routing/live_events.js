'use strict';

// What the router is doing right now, per user, for the home screen's live
// view: a request started, went to a provider, a provider failed it, it
// finished. Kept in memory only (a short ring of events and the requests
// still in flight) and read by long polling. Never holds prompt text.
const { EventEmitter } = require('node:events');

const RING = 300;
const STALE_MS = 15 * 60_000;
const users = new Map();

function state(userId) {
  if (!users.has(userId)) users.set(userId, { seq: 0, events: [], inFlight: new Map(), bus: new EventEmitter().setMaxListeners(100) });
  return users.get(userId);
}

function push(userId, event) {
  const s = state(userId);
  const entry = { seq: ++s.seq, at: Date.now(), ...event };
  s.events.push(entry);
  if (s.events.length > RING) s.events.splice(0, s.events.length - RING);
  s.bus.emit('event');
}

/** key: the card the request comes from (a key's name, or the pool). */
function started(userId, { id, key, task }) {
  state(userId).inFlight.set(id, { id, key, task, startedAt: Date.now(), attempts: [] });
  push(userId, { type: 'start', id, key, task });
}

/** slug: the connection (account slug) the attempt went to. */
function attempt(userId, { id, slug, model }) {
  state(userId).inFlight.get(id)?.attempts.push({ slug, model, state: 'running' });
  push(userId, { type: 'attempt', id, slug, model });
}

function failed(userId, { id, slug }) {
  const item = state(userId).inFlight.get(id);
  const running = item?.attempts.find((a) => a.slug === slug && a.state === 'running');
  if (running) running.state = 'failed';
  push(userId, { type: 'fail', id, slug });
}

function finished(userId, { id, success, model }) {
  const s = state(userId);
  if (!s.inFlight.delete(id)) return;
  push(userId, { type: 'done', id, success, model });
}

/**
 * Events after [since]. When there are none yet, waits up to [waitMs] for
 * the next one. A [since] that is no longer in the ring (or 0) gets a
 * snapshot of what is in flight instead, so the view starts mid-traffic.
 */
async function read(userId, since, waitMs) {
  const s = state(userId);
  // A request that ended without saying so (an error before it was recorded) does not stay.
  for (const [id, item] of s.inFlight) if (Date.now() - item.startedAt > STALE_MS) s.inFlight.delete(id);
  const oldest = s.events.length ? s.events[0].seq : s.seq + 1;
  if (!since || since < oldest - 1 || since > s.seq) {
    return { seq: s.seq, snapshot: true, inFlight: [...s.inFlight.values()], events: [] };
  }
  if (since === s.seq && waitMs > 0) {
    await new Promise((resolve) => {
      const done = () => { clearTimeout(timer); s.bus.off('event', done); resolve(); };
      const timer = setTimeout(done, waitMs);
      s.bus.on('event', done);
    });
  }
  return { seq: s.seq, snapshot: false, events: s.events.filter((event) => event.seq > since) };
}

function resetForTests() { users.clear(); }

module.exports = { started, attempt, failed, finished, read, resetForTests };
