'use strict';

// Prompt compression in levels. Every engine works on the canonical request,
// only ever touches text (never images or tool-call structure) and is kept
// only when it makes the request smaller.
//
//   lossless    whitespace clean-up, minified JSON, duplicate blocks dropped
//   balanced    + repeated log lines collapsed, uniform JSON lists as tables,
//                 very long tool outputs cut to their head and tail
//   aggressive  + older tool outputs hidden (observation masking), older long
//                 turns condensed, optional hard token target
//
// System messages and the latest user message only ever get lossless
// clean-up. Lossy engines start only above a size threshold, so short chats
// pass through untouched.

const LEVELS = Object.freeze(['off', 'lossless', 'balanced', 'aggressive']);
const ENGINES = Object.freeze({
  whitespace: 'lossless',
  jsonMinify: 'lossless',
  dedupe: 'lossless',
  repeatedLines: 'balanced',
  jsonTables: 'balanced',
  trimToolOutput: 'balanced',
  maskOldToolOutput: 'aggressive',
  condenseHistory: 'aggressive',
  tokenTarget: 'aggressive',
});
// Steps that keep all information run on every request of their level; the
// size threshold only gates the steps that leave content out.
const KEEPS_INFORMATION = new Set(['whitespace', 'jsonMinify', 'dedupe', 'repeatedLines', 'jsonTables']);

const DEFAULTS = Object.freeze({ level: 'off', engines: {}, minTokens: 2000, targetTokens: null, keepRecent: 6 });
const DEDUPE_MIN_CHARS = 400;
const TOOL_LIMIT = { older: 4000, latest: 16000 };
const MASK_KEEP_LAST = 3;
const CONDENSE_MIN_CHARS = 1500;

const estimate = (text) => Math.ceil(text.length / 4);

function textParts(message) {
  if (typeof message.content === 'string') return [{ get: () => message.content, set: (value) => { message.content = value; } }];
  if (!Array.isArray(message.content)) return [];
  return message.content
    .map((part, index) => ({ part, index }))
    .filter(({ part }) => part.type === 'text' && typeof part.text === 'string')
    .map(({ index }) => ({ get: () => message.content[index].text, set: (value) => { message.content[index] = { ...message.content[index], text: value }; } }));
}

function messageText(message) {
  return textParts(message).map((part) => part.get()).join('\n');
}

function totalTokens(messages) {
  return messages.reduce((sum, message) => sum + estimate(messageText(message)) + estimate(JSON.stringify(message.tool_calls || '')), 0);
}

// Applies a text transform outside fenced code blocks only.
function outsideCode(text, transform) {
  return text.split(/(```[\s\S]*?```)/g).map((chunk, index) => (index % 2 ? chunk : transform(chunk))).join('');
}

function whitespace(text) {
  return outsideCode(text.replace(/[​-‍﻿]/g, ''), (chunk) => chunk.replace(/[ \t]+$/gm, '').replace(/\n{3,}/g, '\n\n'));
}

function parseJson(text) {
  const trimmed = text.trim();
  if (!/^[[{]/.test(trimmed)) return undefined;
  try { return JSON.parse(trimmed); } catch { return undefined; }
}

function jsonMinify(text) {
  const parsed = parseJson(text);
  return parsed === undefined ? text : JSON.stringify(parsed);
}

// [{a,b},{a,b},…] → {"columns":[a,b],"rows":[[…],[…]]}: same data, keys once.
function tabulate(value) {
  if (Array.isArray(value)) {
    const objects = value.length >= 4 && value.every((item) => item && typeof item === 'object' && !Array.isArray(item));
    if (objects) {
      const columns = Object.keys(value[0]);
      const uniform = value.every((item) => Object.keys(item).length === columns.length && columns.every((key) => key in item));
      if (uniform) return { columns, rows: value.map((item) => columns.map((key) => tabulate(item[key]))) };
    }
    return value.map(tabulate);
  }
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, tabulate(item)]));
  return value;
}

function jsonTables(text) {
  const parsed = parseJson(text);
  return parsed === undefined ? text : JSON.stringify(tabulate(parsed));
}

function repeatedLines(text) {
  const lines = text.split('\n');
  const output = [];
  for (let index = 0; index < lines.length;) {
    let run = 1;
    while (index + run < lines.length && lines[index + run] === lines[index] && lines[index].trim()) run += 1;
    output.push(lines[index]);
    if (run >= 3) output.push(`[… previous line repeated ${run - 1} more times …]`);
    else for (let extra = 1; extra < run; extra += 1) output.push(lines[index]);
    index += run;
  }
  return output.join('\n');
}

// Head and tail of a long text, cut at line breaks where possible.
function headTail(text, limit) {
  if (text.length <= limit) return text;
  const headSize = Math.floor(limit * 0.65);
  const tailSize = limit - headSize;
  let headEnd = text.lastIndexOf('\n', headSize);
  if (headEnd < headSize * 0.6) headEnd = headSize;
  let tailStart = text.indexOf('\n', text.length - tailSize);
  if (tailStart < 0 || tailStart > text.length - tailSize * 0.6) tailStart = text.length - tailSize;
  const omitted = tailStart - headEnd;
  return `${text.slice(0, headEnd)}\n[… ${omitted.toLocaleString('en')} characters omitted to save space …]\n${text.slice(tailStart)}`;
}

function resolveSettings(raw = {}, headerLevel = null) {
  const settings = { ...DEFAULTS, ...raw, engines: { ...(raw.engines || {}) } };
  if (!LEVELS.includes(settings.level)) settings.level = 'off';
  // A request header may lower the level, never raise it.
  if (headerLevel && LEVELS.includes(headerLevel) && LEVELS.indexOf(headerLevel) < LEVELS.indexOf(settings.level)) settings.level = headerLevel;
  return settings;
}

function engineEnabled(settings, name, lossyAllowed) {
  if (settings.level === 'off') return false;
  if (settings.engines[name] === false) return false;
  if (LEVELS.indexOf(ENGINES[name]) > LEVELS.indexOf(settings.level)) return false;
  return KEEPS_INFORMATION.has(name) || lossyAllowed;
}

function compress(canonical, rawSettings, { headerLevel = null } = {}) {
  const settings = resolveSettings(rawSettings, headerLevel);
  const before = totalTokens(canonical.messages);
  const result = { canonical, stats: { level: settings.level, beforeTokens: before, afterTokens: before, savedTokens: 0, engines: {} } };
  if (settings.level === 'off') return result;

  const messages = canonical.messages.map((message) => ({ ...message, content: Array.isArray(message.content) ? [...message.content] : message.content }));
  const lastUser = messages.map((message) => message.role).lastIndexOf('user');
  const protectedIndex = (index) => messages[index].role === 'system' || index === lastUser;
  const lossyAllowed = before >= settings.minTokens;
  const toolIndexes = messages.map((message, index) => (message.role === 'tool' ? index : -1)).filter((index) => index >= 0);
  const recentFrom = messages.length - settings.keepRecent;

  // Runs one engine over the texts it may touch; keeps it only if it saved.
  const run = (name, select, transform) => {
    if (!engineEnabled(settings, name, lossyAllowed)) return;
    const snapshot = messages.map((message) => ({ ...message, content: Array.isArray(message.content) ? [...message.content] : message.content }));
    const start = totalTokens(messages);
    messages.forEach((message, index) => {
      if (!select(message, index)) return;
      for (const part of textParts(message)) {
        const next = transform(part.get(), message, index);
        if (typeof next === 'string' && next.length < part.get().length) part.set(next);
      }
    });
    const saved = start - totalTokens(messages);
    if (saved > 0) result.stats.engines[name] = saved;
    else messages.splice(0, messages.length, ...snapshot);
  };

  const any = () => true;
  const unprotected = (message, index) => !protectedIndex(index);
  run('whitespace', any, whitespace);
  run('jsonMinify', (message, index) => message.role === 'tool' || !protectedIndex(index), jsonMinify);
  if (engineEnabled(settings, 'dedupe', lossyAllowed)) {
    const lastSeen = new Map();
    messages.forEach((message, index) => {
      const text = messageText(message);
      if (text.length >= DEDUPE_MIN_CHARS) lastSeen.set(text, index);
    });
    run('dedupe', (message, index) => !protectedIndex(index) && lastSeen.get(messageText(message)) > index,
      () => '[Same content as a later message – omitted to save space.]');
  }
  run('repeatedLines', (message) => message.role === 'tool' || message.role === 'user', repeatedLines);
  run('jsonTables', (message) => message.role === 'tool', jsonTables);
  run('trimToolOutput', (message) => message.role === 'tool',
    (text, message, index) => headTail(text, index === toolIndexes[toolIndexes.length - 1] ? TOOL_LIMIT.latest : TOOL_LIMIT.older));
  const maskBefore = toolIndexes.length > MASK_KEEP_LAST ? toolIndexes[toolIndexes.length - MASK_KEEP_LAST] : -1;
  run('maskOldToolOutput', (message, index) => message.role === 'tool' && index < maskBefore,
    (text) => (text.length > 600 ? `${text.slice(0, 200)}\n[Older tool output omitted to save space. Run the tool again if you need it.]` : text));
  run('condenseHistory', (message, index) => unprotected(message, index) && index < recentFrom && (message.role === 'user' || message.role === 'assistant'),
    (text) => (text.length > CONDENSE_MIN_CHARS && !text.includes('```') ? headTail(text, 1000) : text));
  if (settings.targetTokens && totalTokens(messages) > settings.targetTokens) {
    run('tokenTarget', unprotected, (text, message, index) => (totalTokens(messages) > settings.targetTokens && index < recentFrom
      ? '[Earlier message omitted to fit the context budget.]'
      : text));
  }

  const after = totalTokens(messages);
  result.canonical = { ...canonical, messages };
  result.stats = { ...result.stats, afterTokens: after, savedTokens: Math.max(0, before - after) };
  return result;
}

module.exports = { compress, resolveSettings, LEVELS, ENGINES, DEFAULTS, headTail, repeatedLines, tabulate };
