'use strict';

// Some hosts (OpenCode) route and cache by a conversation id the client sends.
// The client's own id is passed through; without one the id is derived from the
// start of the conversation, which stays the same for every later turn of it
// and differs between conversations.
const crypto = require('node:crypto');

const CLIENT_HEADERS = ['x-opencode-session', 'x-session-id', 'x-session-affinity', 'x-claude-code-session-id', 'session_id', 'session-id'];
const SAFE = /^[A-Za-z0-9._:-]{1,128}$/;

function clientSession(req) {
  for (const name of CLIENT_HEADERS) {
    const value = String(req?.get?.(name) || '').trim();
    if (SAFE.test(value)) return value;
  }
  return null;
}

const textOf = (content) => (typeof content === 'string' ? content
  : Array.isArray(content) ? content.map((part) => part?.text || '').join('') : '');

function derivedSession(canonical, salt = '') {
  const messages = canonical?.messages || [];
  const head = [
    messages.find((message) => message.role === 'system' || message.role === 'developer'),
    messages.find((message) => message.role === 'user'),
  ].map((message) => textOf(message?.content));
  const hash = crypto.createHash('sha256').update(`${salt}\n${head.join('\n')}`).digest('hex').slice(0, 32);
  return `nr-${hash}`;
}

module.exports = { clientSession, derivedSession };
