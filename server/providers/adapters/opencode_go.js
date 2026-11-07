'use strict';

const crypto = require('node:crypto');
const { OpenAICompatibleAdapter } = require('./openai_compatible');

// OpenCode Go's gateway wants every request to carry a stable session id in
// `x-opencode-session`, so it can route a conversation consistently and reuse
// its prompt cache; a request without the header is refused outright
// (400 MissingSessionID). NeoRouter keeps no conversations of its own, so the
// id is derived from the request: the system messages and the first user turn
// stay identical across the turns of one conversation, so they name it. Only a
// hash leaves the process, never the content.
function sessionId(canonical) {
  const messages = Array.isArray(canonical?.messages) ? canonical.messages : [];
  const system = messages.filter((message) => message.role === 'system' || message.role === 'developer');
  const firstUser = messages.find((message) => message.role === 'user');
  if (!system.length && !firstUser) return null;
  const seed = JSON.stringify([system.map((message) => message.content), firstUser?.content ?? null]);
  return `ses_${crypto.createHash('sha256').update(seed).digest('hex').slice(0, 32)}`;
}

class OpenCodeGoAdapter extends OpenAICompatibleAdapter {
  requestHeaders(canonical) {
    const id = sessionId(canonical);
    return id ? { 'x-opencode-session': id } : {};
  }
}

module.exports = { OpenCodeGoAdapter, sessionId };
