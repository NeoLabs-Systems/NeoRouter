'use strict';

// Privacy shield: replaces sensitive values with typed, numbered placeholders
// ("[EMAIL_1]") before a request leaves the server, and puts the real values
// back into the answer. The same value always gets the same placeholder in a
// request, so the model can still tell people and accounts apart. The mapping
// lives only in memory for the duration of one request.
//
// Detection is layered:
//   1. deterministic patterns with validation (IBAN checksum, Luhn for cards),
//   2. known secret formats, "password=…" assignments, URL credentials and
//      high-entropy tokens,
//   3. the user's own terms (names, company, address),
//   4. optionally entities found by a fast AI model (see ai_detector.js).

const CATEGORIES = Object.freeze(['email', 'phone', 'iban', 'card', 'ip', 'secret', 'custom', 'person', 'address', 'organization', 'id']);

// "hide" never restores the value: card numbers and secrets stay masked in
// the answer too, the safe default for values a model never needs.
const DEFAULT_MODES = Object.freeze({
  email: 'restore', phone: 'restore', iban: 'restore', card: 'hide', ip: 'restore', secret: 'hide',
  custom: 'restore', person: 'restore', address: 'restore', organization: 'restore', id: 'restore',
});

const TOKEN_TYPES = Object.freeze({
  email: 'EMAIL', phone: 'PHONE', iban: 'IBAN', card: 'CARD', ip: 'IP', secret: 'SECRET',
  custom: 'PRIVATE', person: 'PERSON', address: 'ADDRESS', organization: 'ORG', id: 'ID',
});

const SECRET_PATTERNS = [
  /-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]+?-----END [A-Z ]*PRIVATE KEY-----/g,
  /\bsk-(?:ant-|or-v1-|proj-)?[A-Za-z0-9_-]{20,}/g,
  /\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,})\b/g,
  /\bxox[abprs]-[A-Za-z0-9-]{10,}/g,
  /\b(?:AKIA|ASIA)[0-9A-Z]{16}\b/g,
  /\bAIza[0-9A-Za-z_-]{35}\b/g,
  /\b(?:gsk|nvapi|hf|glpat|nrt|csk)[_-][A-Za-z0-9_-]{20,}/g,
  /\b(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{16,}/g,
  /\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/g,
  /\bBearer\s+([A-Za-z0-9._~+/-]{20,}=*)/g,
];
// "password: hunter2" – only the value is hidden. Covers the common words in
// English, German, Dutch, French, Spanish, Portuguese and Italian.
const ASSIGNMENT = /(?<![\p{L}\p{N}])(?:password|passwort|passwd|pwd|passphrase|kennwort|wachtwoord|mot de passe|contrase(?:ñ|n)a|senha|parola d'ordine|pin|secret|geheimnis|token|api[_-]?key|access[_-]?key|client[_-]?secret|auth)(?![\p{L}\p{N}])["']?\s*(?:[:=]|ist|is|lautet|est|es|é)\s*["']?(?!(?:Bearer|Basic)\b)([^\s"',;}{]{3,}[^\s"',;}{.!?:)\]])/giu;
const URL_CREDENTIALS = /\b[a-z][a-z0-9+.-]*:\/\/[^\s:/@]+:([^\s@/]+)@/gi;
const ENTROPY_CANDIDATE = /\b[A-Za-z0-9_+/=-]{32,}\b/g;

const EMAIL = /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b/g;
const PHONE = /(?<![\w+])(?:\+|00)?\d[\d\s()./-]{6,20}\d(?!\w)/g;
const IBAN = /\b[A-Z]{2}\d{2}(?:\s?[A-Z0-9]{4}){2,7}(?:\s?[A-Z0-9]{1,4})?\b/g;
const CARD = /\b\d(?:[ -]?\d){12,18}\b/g;
const IPV4 = /\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}\b/g;
const PLACEHOLDER = /\[\s*([A-Za-z]{2,12})[ _-]?(\d{1,4})\s*\]/g;
// Where matches overlap, the more sensitive kind wins (a password inside a URL
// is a secret even though the URL also looks like an email address).
const PRIORITY = { secret: 0, card: 1, iban: 2, custom: 3, email: 4 };

function luhn(digits) {
  let sum = 0;
  for (let index = 0; index < digits.length; index += 1) {
    let digit = Number(digits[digits.length - 1 - index]);
    if (index % 2 === 1) {
      digit *= 2;
      if (digit > 9) digit -= 9;
    }
    sum += digit;
  }
  return sum % 10 === 0;
}

// Luhn plus a known issuer prefix with that issuer's length, so random long
// numbers (order ids, account parts) are not mistaken for cards.
function cardValid(value) {
  const digits = value.replace(/\D/g, '');
  const brand = [
    [/^4/, [13, 16, 19]],
    [/^(5[1-5]|222[1-9]|22[3-9]\d|2[3-6]\d{2}|27[01]\d|2720)/, [16]],
    [/^3[47]/, [15]],
    [/^(6011|65|64[4-9]|622)/, [16, 17, 18, 19]],
    [/^3(0[0-5]|[689])/, [14, 15, 16, 17, 18, 19]],
    [/^35(2[89]|[3-8])/, [16, 17, 18, 19]],
    [/^(50|5[6-9]|6\d)/, [12, 13, 14, 15, 16, 17, 18, 19]],
  ].find(([prefix]) => prefix.test(digits));
  return Boolean(brand && brand[1].includes(digits.length) && luhn(digits));
}

function ibanValid(value) {
  const compact = value.replace(/\s/g, '');
  if (compact.length < 15 || compact.length > 34) return false;
  const rearranged = compact.slice(4) + compact.slice(0, 4);
  let remainder = 0;
  for (const char of rearranged) {
    const code = /[A-Z]/.test(char) ? String(char.charCodeAt(0) - 55) : char;
    for (const digit of code) remainder = (remainder * 10 + Number(digit)) % 97;
  }
  return remainder === 1;
}

function entropy(value) {
  const counts = new Map();
  for (const char of value) counts.set(char, (counts.get(char) || 0) + 1);
  let bits = 0;
  for (const count of counts.values()) {
    const p = count / value.length;
    bits -= p * Math.log2(p);
  }
  return bits;
}

function looksLikePhone(value) {
  const digits = value.replace(/\D/g, '');
  if (digits.length < 8 || digits.length > 15) return false;
  // Dates, versions and plain amounts are not phone numbers.
  if (/^\d{4}[-./]\d{1,2}[-./]\d{1,2}$/.test(value.trim())) return false;
  return /^(\+|00|0|\()/.test(value.trim()) || /[\s/-]/.test(value);
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function collect(text, pattern, category, { group = 0, accept = null } = {}) {
  const spans = [];
  for (const match of text.matchAll(pattern)) {
    const value = match[group];
    if (!value || (accept && !accept(value))) continue;
    const start = match.index + (group ? match[0].indexOf(value) : 0);
    spans.push({ start, end: start + value.length, category, value });
  }
  return spans;
}

// All sensitive spans in a text, longest first where they overlap.
function detect(text, { modes, customTerms = [], entities = [] }) {
  if (!text) return [];
  const enabled = (category) => modes[category] && modes[category] !== 'off';
  const spans = [];
  if (enabled('secret')) {
    for (const pattern of SECRET_PATTERNS) spans.push(...collect(text, pattern, 'secret', { group: pattern.source.startsWith('\\bBearer') ? 1 : 0 }));
    spans.push(...collect(text, ASSIGNMENT, 'secret', { group: 1, accept: (value) => !/^\[[A-Z]+_\d+\]$/.test(value) }));
    spans.push(...collect(text, URL_CREDENTIALS, 'secret', { group: 1 }));
    spans.push(...collect(text, ENTROPY_CANDIDATE, 'secret', {
      // Git hashes and other pure hex ids are not secrets.
      accept: (value) => !/^[0-9a-f]+$/i.test(value) && /\d/.test(value) && /[a-z]/.test(value) && /[A-Z]/.test(value) && entropy(value) >= 4.2,
    }));
  }
  if (enabled('email')) spans.push(...collect(text, EMAIL, 'email'));
  if (enabled('iban')) spans.push(...collect(text, IBAN, 'iban', { accept: ibanValid }));
  if (enabled('card')) spans.push(...collect(text, CARD, 'card', { accept: cardValid }));
  if (enabled('ip')) spans.push(...collect(text, IPV4, 'ip', { accept: (value) => !/^(127\.|0\.0\.0\.0)/.test(value) }));
  if (enabled('phone')) spans.push(...collect(text, PHONE, 'phone', { accept: looksLikePhone }));
  if (enabled('custom')) {
    for (const term of customTerms) {
      if (term.trim().length < 2) continue;
      spans.push(...collect(text, new RegExp(`(?<![\\p{L}\\p{N}])${escapeRegExp(term.trim())}(?![\\p{L}\\p{N}])`, 'giu'), 'custom'));
    }
  }
  for (const entity of entities) {
    if (!enabled(entity.category) || entity.text.length < 2) continue;
    spans.push(...collect(text, new RegExp(escapeRegExp(entity.text), 'g'), entity.category));
  }
  // Keep the longest span where several overlap; never touch existing placeholders.
  const placeholders = [...text.matchAll(PLACEHOLDER)].map((match) => [match.index, match.index + match[0].length]);
  const rank = (span) => PRIORITY[span.category] ?? 5;
  spans.sort((a, b) => rank(a) - rank(b) || (b.end - b.start) - (a.end - a.start));
  const chosen = [];
  for (const span of spans) {
    if (placeholders.some(([start, end]) => span.start < end && span.end > start)) continue;
    if (chosen.some((other) => span.start < other.end && span.end > other.start)) continue;
    chosen.push(span);
  }
  return chosen.sort((a, b) => a.start - b.start);
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

  tokenFor(category, value) {
    const key = `${category}\u0000${value}`;
    if (this.byValue.has(key)) return this.byValue.get(key);
    const type = TOKEN_TYPES[category];
    const next = (this.counters.get(type) || 0) + 1;
    this.counters.set(type, next);
    const token = `[${type}_${next}]`;
    this.byValue.set(key, token);
    this.byToken.set(`${type}_${next}`, { value, restore: this.modes[category] === 'restore' });
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
      output += text.slice(cursor, span.start) + this.tokenFor(span.category, span.value);
      cursor = span.end;
    }
    return output + text.slice(cursor);
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

const INSTRUCTION = 'Some private values in this conversation were replaced by placeholders in square brackets, such as [EMAIL_1] or [PERSON_2]. '
  + 'Treat each placeholder as the real value it stands for and write it exactly as given whenever you refer to it. '
  + 'Do not invent new placeholders and do not try to guess the original values.';

// Redacts every text in a canonical request. Returns null when nothing was found.
function redactRequest(canonical, { modes, customTerms = [], entitiesFor = () => [] }) {
  const vault = new Vault(modes);
  const messages = canonical.messages.map((message, index) => mapMessageText(message, (text) => vault.redactText(text, { customTerms, entities: entitiesFor(index) })));
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
