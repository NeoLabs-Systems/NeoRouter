'use strict';

// Privacy shield: replaces sensitive values with typed, numbered placeholders
// ("[EMAIL_1]") before a request leaves the server, and puts the real values
// back into the answer. The same value always gets the same placeholder in a
// request, so the model can still tell people and accounts apart. The mapping
// lives only in memory for the duration of one request.
//
// Detection lives in shield/: declarative rule packs (patterns with
// checksums and context words), code detectors (last names, addresses, seed
// phrases, the user's own terms) and, optionally, entities found by a fast
// AI model (see ai_detector.js). Categories and their placeholders come from
// shield/categories.json.
const registry = require('./shield/registry');
const engine = require('./shield/engine');
const { luhn, ibanValid, cardValid } = require('./shield/validators');

const { CATEGORIES, DEFAULT_MODES, TOKEN_TYPES } = registry;
const PLACEHOLDER = /\[\s*([A-Za-z]{2,12})[ _-]?(\d{1,4})\s*\]/g;

// All sensitive spans in a text, in order.
function detect(text, { modes, customTerms = [], allowTerms = [], entities = [] }) {
  return engine.scan(text, { modes, customTerms, allowTerms, entities });
}

/// One request's mapping between real values and placeholders.
class Vault {
  constructor(modes) {
    this.modes = modes;
    this.byValue = new Map();
    this.byToken = new Map();
    this.counters = new Map();
    this.counts = {};
  }

  tokenFor(category, value, ruleId = null) {
    const key = `${category}\u0000${value}`;
    if (this.byValue.has(key)) return this.byValue.get(key);
    const type = TOKEN_TYPES[category];
    const next = (this.counters.get(type) || 0) + 1;
    this.counters.set(type, next);
    const token = `[${type}_${next}]`;
    this.byValue.set(key, token);
    this.byToken.set(`${type}_${next}`, { value, category, ruleId, restore: this.modes[category] === 'restore' });
    this.counts[category] = (this.counts[category] || 0) + 1;
    return token;
  }

  get size() {
    return this.byToken.size;
  }

  redactText(text, options) {
    const spans = detect(text, { modes: this.modes, ...options });
    if (!spans.length) return text;
    let output = '';
    let cursor = 0;
    for (const span of spans) {
      output += text.slice(cursor, span.start) + this.tokenFor(span.category, span.value, span.ruleId);
      cursor = span.end;
    }
    return output + text.slice(cursor);
  }

  // What was hidden and which rule found it, for the settings preview.
  findings() {
    return [...this.byToken].map(([token, entry]) => ({ token: `[${token}]`, category: entry.category, label: registry.labelOf(registry.get().byId.get(entry.ruleId)) }));
  }

  // Tolerates small changes models make ("[EMAIL 1]", "[ email_1 ]").
  restoreText(text) {
    if (!text || !this.byToken.size) return text;
    return text.replace(PLACEHOLDER, (match, type, number) => {
      const entry = this.byToken.get(`${type.toUpperCase()}_${Number(number)}`);
      return entry && entry.restore ? entry.value : match;
    });
  }
}

function mapMessageText(message, transform) {
  const next = { ...message };
  if (typeof message.content === 'string') next.content = transform(message.content);
  else if (Array.isArray(message.content)) {
    next.content = message.content.map((part) => (part.type === 'text' && typeof part.text === 'string' ? { ...part, text: transform(part.text) } : part));
  }
  if (Array.isArray(message.tool_calls)) {
    next.tool_calls = message.tool_calls.map((call) => (call.function?.arguments
      ? { ...call, function: { ...call.function, arguments: transform(call.function.arguments) } }
      : call));
  }
  return next;
}

const INSTRUCTION = 'Some private values in this conversation were replaced by placeholders in square brackets, such as [EMAIL_1] or [LASTNAME_2]. '
  + 'Treat each placeholder as the real value it stands for and write it exactly as given whenever you refer to it. '
  + 'Do not invent new placeholders and do not try to guess the original values.';

// Redacts every text in a canonical request. Returns null when nothing was found.
function redactRequest(canonical, { modes, customTerms = [], allowTerms = [], entitiesFor = () => [] }) {
  const vault = new Vault(modes);
  const messages = canonical.messages.map((message, index) => mapMessageText(message, (text) => vault.redactText(text, { customTerms, allowTerms, entities: entitiesFor(index) })));
  if (!vault.size) return null;
  const systemIndex = messages.findIndex((message) => message.role === 'system');
  if (systemIndex >= 0 && typeof messages[systemIndex].content === 'string') {
    messages[systemIndex] = { ...messages[systemIndex], content: `${messages[systemIndex].content}\n\n${INSTRUCTION}` };
  } else {
    messages.unshift({ role: 'system', content: INSTRUCTION });
  }
  return { canonical: { ...canonical, messages }, vault };
}

function restoreResponse(response, vault) {
  return {
    ...response,
    choices: (response.choices || []).map((choice) => ({ ...choice, message: choice.message ? mapMessageText(choice.message, (text) => vault.restoreText(text)) : choice.message })),
  };
}

// A placeholder may arrive split across stream chunks ("[EMA" + "IL_1]"), so
// text from an unfinished "[" is held back until it can be decided.
const PARTIAL_PLACEHOLDER = /\[\s*[A-Za-z]{0,12}(?:[ _-]?\d{0,4})?\s*$/;

class StreamRestorer {
  constructor(vault) {
    this.vault = vault;
    this.pending = new Map();
  }

  take(key, text, final) {
    const buffer = (this.pending.get(key) || '') + (text || '');
    if (final) {
      this.pending.delete(key);
      return this.vault.restoreText(buffer);
    }
    const partial = buffer.match(PARTIAL_PLACEHOLDER);
    const cut = partial && buffer.length - partial.index <= 24 ? partial.index : buffer.length;
    this.pending.set(key, buffer.slice(cut));
    return this.vault.restoreText(buffer.slice(0, cut));
  }

  // Restores one canonical chunk; the final chunk (finish_reason) flushes.
  process(chunk) {
    if (!chunk?.choices?.length) return chunk;
    const choices = chunk.choices.map((choice) => {
      const final = Boolean(choice.finish_reason);
      const delta = { ...(choice.delta || {}) };
      for (const field of ['content', 'reasoning', 'reasoning_content']) {
        if (typeof delta[field] === 'string' || (final && this.pending.has(`${choice.index}:${field}`))) {
          delta[field] = this.take(`${choice.index}:${field}`, delta[field], final);
        }
      }
      if (Array.isArray(delta.tool_calls)) {
        delta.tool_calls = delta.tool_calls.map((call) => (call.function?.arguments !== undefined
          ? { ...call, function: { ...call.function, arguments: this.take(`${choice.index}:tool:${call.index}`, call.function.arguments, final) } }
          : call));
      }
      return { ...choice, delta };
    });
    return { ...chunk, choices };
  }

  // Whatever is still held when the stream ends without a finish chunk.
  flush(template) {
    if (!this.pending.size) return null;
    const delta = {};
    for (const [key, value] of this.pending) {
      const [, field] = key.split(':');
      if (field !== 'tool' && value) delta[field] = this.vault.restoreText(value);
    }
    this.pending.clear();
    if (!Object.keys(delta).length) return null;
    return { ...template, choices: [{ index: 0, delta, finish_reason: null }] };
  }
}

module.exports = { CATEGORIES, DEFAULT_MODES, detect, Vault, redactRequest, restoreResponse, StreamRestorer, luhn, ibanValid, cardValid };
