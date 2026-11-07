'use strict';

const crypto = require('node:crypto');

// A signal about quality that needs nobody to rate anything: a client sending
// the very same conversation again right after getting an answer did not like
// that answer. Only a hash of the conversation is kept, in memory, for minutes.
const WITHIN_MS = 3 * 60 * 1000;
const MAX_REMEMBERED = 5000;

const answers = new Map();

function fingerprint(keyId, messages) {
  return crypto.createHash('sha256').update(`${keyId}|${JSON.stringify(messages)}`).digest('base64url');
}

// The answer this request repeats, if any: `{ ref, task }`, once per answer.
function repeatedAnswer(id, now = Date.now()) {
  const earlier = answers.get(id);
  if (!earlier) return null;
  answers.delete(id);
  return now - earlier.at <= WITHIN_MS ? earlier : null;
}

function rememberAnswer(id, { ref, task }, now = Date.now()) {
  // A Map keeps insertion order: the first key is the oldest.
  if (answers.size >= MAX_REMEMBERED) answers.delete(answers.keys().next().value);
  answers.set(id, { ref, task, at: now });
}

function resetForTests() {
  answers.clear();
}

module.exports = { fingerprint, repeatedAnswer, rememberAnswer, resetForTests };
