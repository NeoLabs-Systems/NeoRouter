'use strict';

// How sensitive a request is, judged locally and without any model call:
//   high  very sensitive; it stays on the owner's own connections and never
//         goes to a pool partner (services/pools), when the owner wants that
//   low   ordinary personal data (an email address, a name), which the
//         privacy shield already hides from partners
//   none  nothing found
// Built on the privacy shield's detectors, so the same checksums and
// structure tests that keep its false alarms low apply here. "high" is
// deliberately strict: only what is unmistakably secret, or bulk personal
// data such as a customer list. Topics (health, legal) are not judged by
// keywords: that would flag far too many harmless requests.
const redaction = require('./redaction');

// Shield rules that find a value next to a word such as "password". The
// shield hides those values generously; to count as very sensitive, the value
// must also look like a real credential.
const WORD_TRIGGERED = new Set(['password_assignment', 'password_in_sentence', 'env_secret']);
// One-time codes expire within minutes: worth hiding, not worth keeping home.
const NOT_HIGH = new Set(['one_time_code']);
// A variable or placeholder in code ("process.env.PASSWORD", "${TOKEN}", "DB_PASS").
const CODE_REFERENCE = /^(?:[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)+|\$\{?[\w.]+\}?|\{\{?[\w.\s]+\}?\}|[A-Z][A-Z0-9_]+|<[^>]+>)$/;

// Long enough, and more than one plain word: mixes letters with digits,
// symbols or upper and lower case.
function credentialLike(value) {
  const text = String(value || '');
  if (text.length < 6 || CODE_REFERENCE.test(text)) return false;
  const classes = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter((pattern) => pattern.test(text)).length;
  return classes >= 2;
}

function strongSecret(item) {
  if (item.category !== 'secret' || NOT_HIGH.has(item.ruleId) || item.ruleId === 'seed_phrase') return false;
  return WORD_TRIGGERED.has(item.ruleId) ? credentialLike(item.value) : true;
}

// Each rule looks at the shield's findings and names a reason when it applies.
// Rules are independent; a new one is one more entry.
const HIGH_RULES = [
  // API keys, real passwords, tokens, private key blocks, connection strings.
  { reason: 'secret', test: (found) => found.some(strongSecret) },
  // Card numbers that pass Luhn and match an issuer.
  { reason: 'card', test: (found) => found.some((item) => item.category === 'card') },
  // A wallet seed phrase (not a wallet address, which is public by design).
  { reason: 'seed_phrase', test: (found) => found.some((item) => item.ruleId === 'seed_phrase') },
  // Government and tax identifiers with valid check digits.
  { reason: 'government_id', test: (found) => found.some((item) => item.category === 'id') },
  // Bulk personal data: many people's details in one request.
  {
    reason: 'personal_records',
    test: (found) => {
      const distinct = (category) => new Set(found.filter((item) => item.category === category).map((item) => item.value.toLowerCase())).size;
      return distinct('birthdate') >= 3 || distinct('address') >= 5 || distinct('email') + distinct('phone') >= 15;
    },
  },
];

const LOW_CATEGORIES = new Set(['email', 'phone', 'person', 'address', 'birthdate', 'iban', 'ip', 'location', 'vehicle', 'crypto']);
const MAX_SCANNED_CHARS = 200_000;

// Every category on, so the scan sees everything the shield can find.
const ALL_MODES = Object.fromEntries(redaction.CATEGORIES.map((category) => [category.id || category, 'hide']));

function textsOf(canonical) {
  const texts = [];
  const add = (value) => { if (typeof value === 'string' && value) texts.push(value); };
  for (const message of canonical?.messages || []) {
    if (typeof message.content === 'string') add(message.content);
    else if (Array.isArray(message.content)) {
      for (const part of message.content) add(typeof part === 'string' ? part : part?.text);
    }
    for (const call of message.tool_calls || []) add(call?.function?.arguments);
  }
  return texts;
}

/**
 * assess(canonical) → { level: 'none' | 'low' | 'high', reasons: string[] }
 * reasons name what made it high (never the values themselves).
 */
function assess(canonical) {
  let budget = MAX_SCANNED_CHARS;
  const found = [];
  for (const text of textsOf(canonical)) {
    if (budget <= 0) break;
    const slice = text.length > budget ? text.slice(0, budget) : text;
    budget -= slice.length;
    for (const span of redaction.detect(slice, { modes: ALL_MODES })) found.push({ category: span.category, ruleId: span.ruleId, value: span.value });
  }
  const reasons = HIGH_RULES.filter((rule) => rule.test(found)).map((rule) => rule.reason);
  if (reasons.length) return { level: 'high', reasons };
  return { level: found.some((item) => LOW_CATEGORIES.has(item.category)) ? 'low' : 'none', reasons: [] };
}

module.exports = { assess, credentialLike, HIGH_RULES };
