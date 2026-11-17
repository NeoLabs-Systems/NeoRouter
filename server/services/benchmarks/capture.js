'use strict';
const consent = require('./consent');
const store = require('./private_store');
const privacy = require('./privacy');
const { classify, TASK_KINDS } = require('../routing/task/classifier');
const { estimateInputTokens } = require('../../capabilities/chat/canonical');
const { isPeerEntry } = require('../pools/links');
const POLICY = require('./policy');
const queue = [];
let running = false, scheduled = false;

async function capture({ userId, keyId, entry, canonical, message, finishReason, peer = false, internal = false, findings = [], expected = null, task: taskHint = null, signal = null }) {
  if (signal?.aborted || peer || internal || !entry || isPeerEntry(entry) || !['stop', 'tool_calls'].includes(finishReason)) return false;
  const ticket = expected || consent.ticket(userId, keyId);
  if (!ticket || !consent.allowed(userId, keyId, ticket)) return false;
  const clean = await privacy.sanitize(userId, canonical, message, findings);
  if (!clean || signal?.aborted || !consent.allowed(userId, keyId, ticket)) return false;
  const task = [...TASK_KINDS, 'general'].includes(taskHint) ? taskHint : classify(clean.canonical, { inputTokens: estimateInputTokens(clean.canonical) }).primary;
  return store.put(userId, keyId, { ...clean, task, source: { canonicalId: entry.canonicalId, ref: entry.ref } }, ticket);
}

function startSoon(delay = false) {
  if (scheduled || running || !queue.length) return;
  scheduled = true;
  const run = () => { scheduled = false; drain(); };
  if (delay) setTimeout(run, POLICY.tickMs).unref(); else setImmediate(run);
}
function drain() {
  while (queue.length && queue[0].expiresAt <= Date.now()) queue.shift();
  if (!queue.length) return;
  const { input, expected } = queue.shift();
  running = true;
  capture({ ...input, expected }).catch(() => false).finally(() => { running = false; startSoon(); });
}
consent.events.on('revoke', ({ userId, keyId }) => {
  for (let index = queue.length - 1; index >= 0; index--) {
    const item = queue[index].input;
    if ((!userId || item.userId === userId) && (!keyId || item.keyId === keyId)) queue.splice(index, 1);
  }
});
function schedule(input) {
  // Optional collection must never turn a successful gateway response into an
  // error if its settings/storage are temporarily unavailable.
  try {
    if (input.signal?.aborted || input.peer || input.internal || !input.entry || isPeerEntry(input.entry) || !['stop', 'tool_calls'].includes(input.finishReason)) return;
    if ((input.estimatedTokens ?? 0) * 4 > POLICY.maxPayloadBytes) return;
    if (!consent.globalSettings().enabled || queue.length >= POLICY.pendingCaptures) return;
    const expected = consent.ticket(input.userId, input.keyId);
    if (!expected) return;
    queue.push({ input, expected, expiresAt: Date.now() + POLICY.pendingCaptureMs });
    startSoon();
  } catch { /* Drop the sample, never log its contents. */ }
}

module.exports = { capture, schedule };
