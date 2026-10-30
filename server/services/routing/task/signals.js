'use strict';

const { textOf, contentParts } = require('../../../capabilities/chat/canonical');

// What the task detectors look at, read once per request. Like the hosted
// routers, the latest user message carries most of the signal; the system
// prompt and earlier turns only add context (agents send long system prompts
// with every turn, which says nothing about this turn).
const LAST_USER_CHARS = 12000;
const SYSTEM_CHARS = 4000;
const PREVIOUS_CHARS = 2000;

// A message that opens with a short ask and goes on with a longer text
// ("Summarise this: <article>") carries a passage: the ask says what to do,
// the passage only what to do it with.
const ASK_MAX_CHARS = 400;
const PASSAGE_MIN_CHARS = 300;

// Patterns match from the start of a word, so German stems ("schreib")
// cover their forms; end a pattern with "$" to require the whole word.
const words = (...patterns) => new RegExp(`(^|[^\\p{L}])(${patterns.map((pattern) => pattern.replace(/\$$/, '(?![\\p{L}])')).join('|')})`, 'iu');
// How many different words of a pattern a text uses: repeating one word says no more.
const hits = (pattern, text) => new Set((text.match(new RegExp(pattern.source, `${pattern.flags.replace('g', '')}g`)) || [])
  .map((match) => match.replace(/^[^\p{L}]+/u, '').toLowerCase())).size;

// Numbers ("16", "2.5", "40%", "$5") and the words of quantity that word
// problems are told in.
const NUMBER = /(?<![\p{L}\d.])[$€£]?\d+(?:[.,]\d+)?%?(?![\p{L}\d])/gu;
const QUANTITY_WORDS = words('half$', 'twice$', 'double$', 'triple$', 'third$', 'quarter$', 'percent', 'each$', 'per$', 'every', 'total', 'sum$', 'remain',
  'left over', 'hälfte', 'doppelt', 'drittel', 'viertel', 'prozent', 'jede', 'pro$', 'insgesamt', 'summe$', 'übrig');
// Conditions an answer has to meet: the "constraints" dimension of NVIDIA's
// prompt complexity classifier.
const CONSTRAINT_WORDS = words('must$', 'should$', "shouldn't", 'do not', "don't", 'never', 'always', 'only$', 'at least', 'at most', 'no more than', 'exactly',
  'without', 'make sure', 'ensure', 'limit(ed)? to', 'under \\d+', 'within \\d+', 'muss', 'darf nicht', 'nicht mehr als', 'mindestens', 'höchstens',
  'genau$', 'ohne$', 'nur$', 'achte darauf', 'stelle sicher');
const LIST_ITEM = /^\s*(?:[-*•]|\d+[.)])\s+\S/gm;
// Worked examples in the prompt (few-shot): "Example 2:", "Input: … Output: …", "Q: … A: …".
const EXAMPLE_LINE = /^\s*(?:example|beispiel|input|output|eingabe|ausgabe|q|a)\s*\d*\s*:/gim;
// Long words and acronyms: a rough measure of specialist vocabulary.
const LONG_WORD = /\p{L}{13,}/gu;
const ACRONYM = /\b[A-Z]{2,6}s?\b/g;

const count = (pattern, text) => (text.match(pattern) || []).length;

function userMessages(messages) {
  return (messages || []).filter((message) => message.role === 'user');
}

/** The ask and its passage, when the message reads "<short ask>\n\n<long text>". */
function splitPassage(text) {
  const trimmed = text.trim();
  const breakAt = trimmed.search(/\n\s*\n|(?<=[?:])\s*\n/);
  const end = breakAt > 0 && breakAt <= ASK_MAX_CHARS ? breakAt : -1;
  if (end < 0 || trimmed.length - end < PASSAGE_MIN_CHARS) return { ask: trimmed, passage: '' };
  return { ask: trimmed.slice(0, end).trim(), passage: trimmed.slice(end).trim() };
}

function readSignals(request, { inputTokens = 0 } = {}) {
  const messages = request.messages || [];
  const users = userMessages(messages);
  const last = users[users.length - 1];
  const lastUser = last ? textOf(last.content).slice(-LAST_USER_CHARS) : '';
  const previousUser = users.length > 1 ? textOf(users[users.length - 2].content).slice(-PREVIOUS_CHARS) : '';
  const lastIndex = messages.lastIndexOf(last);
  const lastAssistant = [...messages.slice(0, lastIndex < 0 ? messages.length : lastIndex)].reverse().find((message) => message.role === 'assistant');
  const system = messages.filter((message) => message.role === 'system' || message.role === 'developer')
    .map((message) => textOf(message.content)).join('\n').slice(0, SYSTEM_CHARS);
  const hasImages = messages.some((message) => contentParts(message.content)
    .some((part) => ['image_url', 'input_image', 'image'].includes(part.type)));
  const format = request.response_format?.type;
  const { ask, passage } = splitPassage(lastUser);
  return {
    lastUser,
    // What the user asks for, without a pasted passage: kind words are read here.
    ask,
    passage,
    // Rough token count of the latest user message.
    lastUserTokens: Math.ceil(lastUser.length / 4),
    system,
    // The turn before, for short follow-ups ("now make it faster").
    previousUser,
    previousCode: /```/.test(textOf(lastAssistant?.content || '')),
    turns: users.length,
    toolCount: (request.tools || []).length,
    // Tool results in this turn: the request continues agent work.
    toolResults: messages.slice(-6).filter((message) => message.role === 'tool').length,
    wantsJson: format === 'json_object' || format === 'json_schema',
    hasImages,
    inputTokens,
    codeBlocks: (lastUser.match(/```/g) || []).length / 2,
    numbers: count(NUMBER, ask),
    quantityWords: hits(QUANTITY_WORDS, ask),
    questions: count(/\?/g, ask),
    constraints: hits(CONSTRAINT_WORDS, ask) + count(LIST_ITEM, ask),
    examples: count(EXAMPLE_LINE, lastUser),
    jargon: count(LONG_WORD, ask) + count(ACRONYM, ask),
    askWords: ask.split(/\s+/).filter(Boolean).length,
  };
}

module.exports = { readSignals, splitPassage, words, hits };
