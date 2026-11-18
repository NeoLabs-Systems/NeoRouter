'use strict';

const { createHash } = require('node:crypto');
const consent = require('./consent');
const privacy = require('./privacy');
const policy = require('./policy');
const { isPeerEntry } = require('../pools/links');

const staged = new Map();
const pending = new Set();
const recent = new Map();
const expiryTimers = new WeakMap();

function count(userId) {
  if (userId) return staged.get(userId)?.size || 0;
  return [...staged.values()].reduce((total, samples) => total + samples.size, 0);
}

function schedule(input) {
  if (!input.ticket || !input.task || input.signal?.aborted || isPeerEntry(input.entry) || input.canonical.n > 1 ||
    input.canonical.messages.some(({ content }) => Array.isArray(content) && content.some((part) => part?.type !== 'text')) ||
    (input.message.content != null && typeof input.message.content !== 'string') ||
    Buffer.byteLength(input.message.content || '') > policy.maxPayloadBytes ||
    !['stop', 'tool_calls'].includes(input.finishReason) || input.inputTokens * 4 > policy.maxPayloadBytes ||
    pending.size >= policy.parallel || count() >= policy.maxSamples || count(input.userId) >= policy.maxSamplesPerUser) return;
  const item = { userId: input.userId, keyId: input.keyId };
  pending.add(item);
  item.handle = setImmediate(async () => {
    item.handle = null;
    try {
      const active = () => !item.cancelled && !input.signal?.aborted && consent.ticket(input.userId, input.keyId) === input.ticket;
      if (!active()) return;
      const clean = await privacy.sanitize(input.userId, input.canonical, input.message, input.findings);
      if (!clean || !active() || count() >= policy.maxSamples || count(input.userId) >= policy.maxSamplesPerUser) return;
      const samples = staged.get(input.userId) || new Map();
      const digest = createHash('sha256').update(JSON.stringify(clean.canonical)).digest('hex');
      if (!recent.has(input.userId) && recent.size >= policy.maxSamples) return;
      const seen = recent.get(input.userId) || new Map();
      const now = Date.now();
      for (const [hash, record] of seen) if (record.expiresAt <= now) seen.delete(hash);
      if (samples.has(digest) || seen.has(digest) || seen.size >= policy.maxSamplesPerUser) return;
      const expiresAt = now + policy.sampleLifetimeMs;
      seen.set(digest, { keyId: input.keyId, expiresAt });
      recent.set(input.userId, seen);
      const sample = {
        userId: input.userId, keyId: input.keyId, ticket: input.ticket, ...clean, task: input.task,
        source: input.entry.canonicalId, digest, expiresAt, nextAt: 0, failures: 0, answers: [], excluded: new Set(),
      };
      expiryTimers.set(sample, setTimeout(() => remove(sample), policy.sampleLifetimeMs).unref());
      samples.set(digest, sample);
      staged.set(input.userId, samples);
    } catch {
      // Collection is best effort, including when privacy processing fails.
    } finally {
      pending.delete(item);
    }
  });
}

function remove(sample) {
  const samples = staged.get(sample.userId);
  if (samples?.get(sample.digest) === sample) samples.delete(sample.digest);
  clearTimeout(expiryTimers.get(sample));
  if (samples?.size === 0) staged.delete(sample.userId);
}

consent.events.on('revoke', ({ userId, keyId }) => {
  const matches = (item) => (!userId || userId === item.userId) && (!keyId || keyId === item.keyId);
  for (const item of pending) {
    if (!matches(item)) continue;
    item.cancelled = true;
    if (item.handle) {
      clearImmediate(item.handle);
      pending.delete(item);
    }
  }
  for (const [ownerId, seen] of recent) {
    for (const [hash, record] of seen) if (matches({ userId: ownerId, keyId: record.keyId })) seen.delete(hash);
    if (!seen.size) recent.delete(ownerId);
  }
  for (const samples of staged.values()) for (const sample of samples.values()) if (matches(sample)) remove(sample);
});

function prune(now) {
  for (const [userId, seen] of recent) {
    for (const [hash, record] of seen) if (record.expiresAt <= now) seen.delete(hash);
    if (!seen.size) recent.delete(userId);
  }
}

module.exports = { schedule, count, staged, remove, prune };
