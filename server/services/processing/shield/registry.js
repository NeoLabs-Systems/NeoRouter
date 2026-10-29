'use strict';

// Loads the shield's building blocks once:
//   categories.json   what can be hidden, its placeholder and default mode
//   rules/*.js|json   declarative pattern rules, one pack per file
//   detectors/*.js    code detectors for what one regex cannot express
//   data/*.txt        word lists the detectors read (names, wordlists)
// Every rule and detector is validated and ids must be unique, so a broken
// pack fails at start-up instead of silently letting data through.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { z } = require('zod');
const { VALIDATORS } = require('./validators');

const categoryList = require('./categories.json');
const CATEGORY_IDS = categoryList.map((category) => category.id);

const label = z.union([z.string().min(1), z.object({ en: z.string().min(1), de: z.string().min(1) })]);
const words = z.array(z.string().min(1)).default([]);

const ruleSchema = z.object({
  id: z.string().regex(/^[a-z0-9][a-z0-9_.-]*$/),
  label,
  category: z.enum(CATEGORY_IDS),
  pattern: z.union([z.instanceof(RegExp), z.string().min(1)]),
  flags: z.string().regex(/^[imsuv]*$/).default(''),
  // Which part of the match is the private value: a group number or name.
  group: z.union([z.number().int().min(0), z.string()]).default(0),
  // gitleaks style: with group 0, the first group that matched is the value.
  firstGroup: z.boolean().default(false),
  // Lowercase literals; the rule only runs when the text contains one.
  keywords: words,
  // A checksum: a failure drops the match, a pass adds VALIDATOR_BONUS.
  validator: z.enum(Object.keys(VALIDATORS)).optional(),
  // A plain gate: a failure drops the match, a pass adds nothing.
  accept: z.enum(Object.keys(VALIDATORS)).optional(),
  minEntropy: z.number().min(0).max(8).optional(),
  score: z.number().min(0).max(1).default(0.6),
  context: z.object({ positive: words, negative: words, window: z.number().int().min(1).max(200).default(40) }).optional(),
  allow: z.object({ regexes: z.array(z.string()).default([]), stopwords: words }).optional(),
  tests: z.object({
    match: z.array(z.union([z.string(), z.object({ text: z.string(), value: z.string() })])).default([]),
    ignore: z.array(z.string()).default([]),
  }).default({}),
});

const detectorSchema = z.object({
  id: z.string().regex(/^[a-z0-9][a-z0-9_.-]*$/),
  label,
  category: z.enum(CATEGORY_IDS),
  scan: z.function(),
  tests: z.object({
    match: z.array(z.union([z.string(), z.object({ text: z.string(), value: z.string() })])).default([]),
    ignore: z.array(z.string()).default([]),
  }).default({}),
});

const packSchema = z.object({
  source: z.string().optional(),
  license: z.string().optional(),
  allow: z.object({ regexes: z.array(z.string()).default([]), stopwords: words }).optional(),
  rules: z.array(z.unknown()),
});

const escapeRegExp = (value) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
// A word list as one regex that only matches whole words.
const wordRegex = (list) => (list.length ? new RegExp(`(?<![\\p{L}\\p{N}])(?:${list.map(escapeRegExp).join('|')})(?![\\p{L}\\p{N}])`, 'iu') : null);

function compileRule(raw, file, packAllow) {
  const parsed = ruleSchema.safeParse(raw);
  if (!parsed.success) {
    throw new Error(`Invalid shield rule in ${file} (${raw?.id || 'unknown'}): ${parsed.error.issues.map((issue) => `${issue.path.join('.')}: ${issue.message}`).join('; ')}`);
  }
  const rule = parsed.data;
  const source = rule.pattern instanceof RegExp ? rule.pattern.source : rule.pattern;
  const flags = new Set([...(rule.pattern instanceof RegExp ? rule.pattern.flags : rule.flags), 'g', 'd']);
  flags.delete('y');
  let regex;
  try {
    regex = new RegExp(source, [...flags].join(''));
  } catch (error) {
    throw new Error(`Shield rule ${rule.id} in ${file} has an invalid pattern: ${error.message}`);
  }
  const allowRegexes = [...(packAllow?.regexes || []), ...(rule.allow?.regexes || [])].map((pattern) => new RegExp(pattern, 'i'));
  const stopwords = [...(packAllow?.stopwords || []), ...(rule.allow?.stopwords || [])].map((word) => word.toLowerCase());
  const context = rule.context || { positive: [], negative: [], window: 40 };
  return Object.freeze({
    ...rule,
    kind: 'rule',
    file,
    regex,
    keywords: rule.keywords.map((word) => word.toLowerCase()),
    context: { ...context, positiveRegex: wordRegex(context.positive), negativeRegex: wordRegex(context.negative) },
    allowRegexes,
    stopwords,
  });
}

function readPacks(directory, hash) {
  const rules = [];
  for (const file of fs.readdirSync(directory).filter((name) => /\.(js|json)$/.test(name)).sort()) {
    const full = path.join(directory, file);
    hash.update(fs.readFileSync(full));
    const exported = require(full);
    const pack = Array.isArray(exported) ? { rules: exported } : exported;
    const parsedPack = packSchema.safeParse(pack);
    if (!parsedPack.success) throw new Error(`Invalid shield rule pack ${file}: ${parsedPack.error.issues[0].message}`);
    for (const raw of parsedPack.data.rules) rules.push(compileRule(raw, file, parsedPack.data.allow));
  }
  return rules;
}

function readDetectors(directory, hash) {
  const detectors = [];
  for (const file of fs.readdirSync(directory).filter((name) => name.endsWith('.js')).sort()) {
    const full = path.join(directory, file);
    hash.update(fs.readFileSync(full));
    const exported = require(full);
    for (const raw of Array.isArray(exported) ? exported : [exported]) {
      const parsed = detectorSchema.safeParse(raw);
      if (!parsed.success) throw new Error(`Invalid shield detector in ${file} (${raw?.id || 'unknown'}): ${parsed.error.issues.map((issue) => `${issue.path.join('.')}: ${issue.message}`).join('; ')}`);
      detectors.push(Object.freeze({ ...parsed.data, scan: raw.scan, kind: 'detector', file, keywords: (raw.keywords || []).map((word) => word.toLowerCase()) }));
    }
  }
  return detectors;
}

function build({ rulesDir = path.join(__dirname, 'rules'), detectorsDir = path.join(__dirname, 'detectors') } = {}) {
  const hash = crypto.createHash('sha256');
  hash.update(JSON.stringify(categoryList));
  const rules = readPacks(rulesDir, hash);
  const detectors = readDetectors(detectorsDir, hash);
  const ids = new Set();
  for (const item of [...rules, ...detectors]) {
    if (ids.has(item.id)) throw new Error(`Duplicate shield rule id ${item.id} in ${item.file}.`);
    ids.add(item.id);
  }
  const byId = new Map([...rules, ...detectors].map((item) => [item.id, item]));
  return Object.freeze({ version: hash.digest('hex').slice(0, 16), rules, detectors, byId });
}

let cache = null;
const get = () => {
  if (!cache) cache = build();
  return cache;
};

// Word lists from data/, loaded on first use. One entry per line; lines
// starting with "#" are comments (source and licence).
const lists = new Map();
function wordList(name) {
  if (!lists.has(name)) {
    const text = fs.readFileSync(path.join(__dirname, 'data', `${name}.txt`), 'utf8');
    lists.set(name, new Set(text.split('\n').map((line) => line.trim()).filter((line) => line && !line.startsWith('#')).map((line) => line.toLowerCase())));
  }
  return lists.get(name);
}

const CATEGORIES = Object.freeze(CATEGORY_IDS);
const DEFAULT_MODES = Object.freeze(Object.fromEntries(categoryList.map((category) => [category.id, category.defaultMode])));
const TOKEN_TYPES = Object.freeze(Object.fromEntries(categoryList.map((category) => [category.id, category.token])));
const PRIORITY = Object.freeze(Object.fromEntries(categoryList.map((category) => [category.id, category.priority])));

const labelOf = (item) => (typeof item?.label === 'string' ? { en: item.label, de: item.label } : item?.label || null);

module.exports = { get, build, wordList, wordRegex, escapeRegExp, labelOf, CATEGORIES, DEFAULT_MODES, TOKEN_TYPES, PRIORITY, categoryList };
