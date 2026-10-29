'use strict';

// Runs the rules and detectors over one text, cheapest work first:
//   1. keyword prefilter: a rule with keywords only runs when the lowercased
//      text contains one (rules that need context use their context words),
//   2. the regex itself,
//   3. validators: a failed checksum drops the match, a passed one adds
//      VALIDATOR_BONUS,
//   4. context words near the match add or subtract CONTEXT_BONUS / PENALTY,
//   5. only matches scoring at least THRESHOLD are kept,
//   6. allow-lists, then overlaps (the more sensitive category wins, then
//      the longer match).
// Results are cached per text, because chat clients re-send the whole
// history every turn. With about 300 rules a plain `includes` prefilter is
// enough; an Aho-Corasick automaton only pays off with thousands of keywords.
const crypto = require('node:crypto');
const registry = require('./registry');
const { VALIDATORS, entropy } = require('./validators');

const THRESHOLD = 0.5;
const VALIDATOR_BONUS = 0.3;
const CONTEXT_BONUS = 0.3;
const CONTEXT_PENALTY = 0.4;
const CACHE_SIZE = 2000;
const PLACEHOLDER = /\[\s*([A-Za-z]{2,12})[ _-]?(\d{1,4})\s*\]/g;

const cache = new Map();

function needsContext(rule) {
  return rule.score + (rule.validator ? VALIDATOR_BONUS : 0) < THRESHOLD;
}

function triggered(item, lower) {
  const keywords = item.keywords.length ? item.keywords : item.kind === 'rule' && needsContext(item) ? item.context.positive.map((word) => word.toLowerCase()) : [];
  return !keywords.length || keywords.some((word) => lower.includes(word));
}

function contextScore(rule, text, start, end) {
  const { positiveRegex, negativeRegex, window } = rule.context;
  if (!positiveRegex && !negativeRegex) return 0;
  const around = `${text.slice(Math.max(0, start - window), start)} \u0000 ${text.slice(end, end + window)}`;
  let score = 0;
  if (positiveRegex?.test(around)) score += CONTEXT_BONUS;
  if (negativeRegex?.test(around)) score -= CONTEXT_PENALTY;
  return score;
}

function valueOf(rule, match) {
  if (rule.group === 0) {
    // gitleaks style: the first group that matched is the secret.
    const index = rule.firstGroup ? match.findIndex((part, position) => position > 0 && part) : 0;
    return index > 0 ? { value: match[index], start: match.indices[index][0] } : { value: match[0], start: match.index };
  }
  if (typeof rule.group === 'string') {
    const value = match.groups?.[rule.group];
    return value ? { value, start: match.indices.groups[rule.group][0] } : null;
  }
  return match[rule.group] ? { value: match[rule.group], start: match.indices[rule.group][0] } : null;
}

// Every match of one rule with its score; unscored matches below the
// threshold are kept out.
function runRule(rule, text) {
  const spans = [];
  rule.regex.lastIndex = 0;
  for (const match of text.matchAll(rule.regex)) {
    const found = valueOf(rule, match);
    if (!found || !found.value.trim()) continue;
    const { value, start } = found;
    if (rule.accept && !VALIDATORS[rule.accept](value)) continue;
    if (rule.minEntropy && entropy(value) < rule.minEntropy) continue;
    if (rule.stopwords.length && rule.stopwords.some((word) => value.toLowerCase().includes(word))) continue;
    if (rule.allowRegexes.some((pattern) => pattern.test(value))) continue;
    let score = rule.score;
    if (rule.validator) {
      if (!VALIDATORS[rule.validator](value)) continue;
      score += VALIDATOR_BONUS;
    }
    score += contextScore(rule, text, start, start + value.length);
    if (score < THRESHOLD) continue;
    spans.push({ start, end: start + value.length, category: rule.category, value, ruleId: rule.id, score: Math.min(1, score) });
  }
  return spans;
}

function runDetector(detector, text, ctx) {
  return detector.scan(text, ctx)
    .filter((span) => (span.score ?? 1) >= THRESHOLD && span.end > span.start)
    .map((span) => ({ category: detector.category, ruleId: detector.id, score: 1, ...span, value: span.value ?? text.slice(span.start, span.end) }));
}

function allowed(value, allowTerms) {
  const lower = value.toLowerCase();
  return allowTerms.some((term) => lower === term || ((term.startsWith('@') || term.startsWith('.')) && lower.endsWith(term)));
}

// Keeps the most sensitive, then longest span where several overlap, and
// never touches existing placeholders.
function resolve(text, spans) {
  const placeholders = [...text.matchAll(PLACEHOLDER)].map((match) => [match.index, match.index + match[0].length]);
  const rank = (span) => registry.PRIORITY[span.category] ?? 5;
  const sorted = [...spans].sort((a, b) => rank(a) - rank(b) || (b.end - b.start) - (a.end - a.start) || b.score - a.score);
  const chosen = [];
  for (const span of sorted) {
    if (placeholders.some(([start, end]) => span.start < end && span.end > start)) continue;
    if (chosen.some((other) => span.start < other.end && span.end > other.start)) continue;
    chosen.push(span);
  }
  return chosen.sort((a, b) => a.start - b.start);
}

function enabledSet(modes) {
  return new Set(Object.entries(modes || {}).filter(([, mode]) => mode && mode !== 'off').map(([category]) => category));
}

// Spans from rules and detectors, before AI entities are added.
function baseScan(text, { enabled, customTerms, allowTerms }, built) {
  const lower = text.toLowerCase();
  const ctx = { lower, customTerms, enabled };
  const spans = [];
  for (const rule of built.rules) {
    if (enabled.has(rule.category) && triggered(rule, lower)) spans.push(...runRule(rule, text));
  }
  for (const detector of built.detectors) {
    if (detector.id === 'ai_entities') continue;
    if (enabled.has(detector.category) && triggered(detector, lower)) spans.push(...runDetector(detector, text, ctx));
  }
  return spans.filter((span) => !allowed(span.value, allowTerms));
}

function cacheGet(key) {
  const hit = cache.get(key);
  if (hit) {
    cache.delete(key);
    cache.set(key, hit);
  }
  return hit;
}

function cachePut(key, value) {
  cache.set(key, value);
  if (cache.size > CACHE_SIZE) cache.delete(cache.keys().next().value);
}

/**
 * All private spans in a text: { start, end, category, value, ruleId }.
 * options: modes, customTerms, allowTerms, entities (from the AI pass, each
 * { text, category }; matched exactly), built (a registry build, for tests).
 */
function scan(text, { modes, customTerms = [], allowTerms = [], entities = [], built = registry.get() } = {}) {
  if (!text) return [];
  const enabled = enabledSet(modes);
  const terms = customTerms.map((term) => term.trim()).filter((term) => term.length >= 2);
  const allow = allowTerms.map((term) => term.trim().toLowerCase()).filter(Boolean);
  const key = crypto.createHash('sha256').update(`${built.version}\u0000${[...enabled].sort().join(',')}\u0000${terms.join('\u0001')}\u0000${allow.join('\u0001')}\u0000${text}`).digest('hex');
  let spans = cacheGet(key);
  if (!spans) {
    spans = baseScan(text, { enabled, customTerms: terms, allowTerms: allow }, built);
    cachePut(key, spans);
  }
  const fromEntities = [];
  const entityDetector = built.detectors.find((detector) => detector.id === 'ai_entities');
  if (entities.length && entityDetector) {
    for (const span of runDetector(entityDetector, text, { entities, enabled })) {
      if (enabled.has(span.category) && !allowed(span.value, allow)) fromEntities.push(span);
    }
  }
  return resolve(text, fromEntities.length ? [...spans, ...fromEntities] : spans);
}

// One rule or detector on its own, all categories on: used by the rule tests.
function scanOne(item, text) {
  if (item.kind === 'rule') return runRule(item, text);
  return runDetector(item, text, { lower: text.toLowerCase(), customTerms: [], entities: [], enabled: new Set(registry.CATEGORIES) });
}

module.exports = { scan, scanOne, resolve, THRESHOLD, clearCache: () => cache.clear() };
