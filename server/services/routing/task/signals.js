'use strict';

const { textOf, contentParts } = require('../../../capabilities/chat/canonical');

// What the task detectors look at, read once per request. Like the hosted
// routers, the latest user message carries most of the signal; the system
// prompt and earlier turns only add context (agents send long system prompts
// with every turn, which says nothing about this turn).
const LAST_USER_CHARS = 12000;
const SYSTEM_CHARS = 4000;

function userMessages(messages) {
  return (messages || []).filter((message) => message.role === 'user');
}

function readSignals(request, { inputTokens = 0 } = {}) {
  const messages = request.messages || [];
  const users = userMessages(messages);
  const last = users[users.length - 1];
  const lastUser = last ? textOf(last.content).slice(-LAST_USER_CHARS) : '';
  const system = messages.filter((message) => message.role === 'system' || message.role === 'developer')
    .map((message) => textOf(message.content)).join('\n').slice(0, SYSTEM_CHARS);
  const hasImages = messages.some((message) => contentParts(message.content)
    .some((part) => ['image_url', 'input_image', 'image'].includes(part.type)));
  const format = request.response_format?.type;
  return {
    lastUser,
    // Rough token count of the latest user message.
    lastUserTokens: Math.ceil(lastUser.length / 4),
    system,
    turns: users.length,
    toolCount: (request.tools || []).length,
    // Tool results in this turn: the request continues agent work.
    toolResults: messages.slice(-6).filter((message) => message.role === 'tool').length,
    wantsJson: format === 'json_object' || format === 'json_schema',
    hasImages,
    inputTokens,
    codeBlocks: (lastUser.match(/```/g) || []).length / 2,
  };
}

module.exports = { readSignals };
