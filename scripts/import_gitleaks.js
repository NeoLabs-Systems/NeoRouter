#!/usr/bin/env node
'use strict';

// Converts the gitleaks secret rules (MIT, https://github.com/gitleaks/gitleaks)
// into a privacy shield rule pack:
//   node scripts/import_gitleaks.js [tag]
// writes server/services/processing/shield/rules/secrets_gitleaks.json. The
// output is committed; bump TAG to take newer upstream rules. Go RE2 syntax
// is rewritten for JavaScript; rules that still don't compile, or that only
// match file paths, are skipped and listed.
const fs = require('node:fs');
const path = require('node:path');
const { parse } = require('smol-toml');

const TAG = process.argv[2] || 'v8.30.1';
const SOURCE = `https://raw.githubusercontent.com/gitleaks/gitleaks/${TAG}/config/gitleaks.toml`;
const OUT = path.join(__dirname, '..', 'server', 'services', 'processing', 'shield', 'rules', 'secrets_gitleaks.json');

const POSIX = {
  alnum: 'A-Za-z0-9', alpha: 'A-Za-z', digit: '0-9', space: '\\s', upper: 'A-Z', lower: 'a-z', xdigit: '0-9A-Fa-f', word: '\\w', punct: '!-\\/:-@\\[-`{-~',
};

// RE2 → JavaScript. Inline flags become flags on the whole pattern, which is
// at most a little broader than upstream.
function convert(source) {
  const flags = new Set();
  const pattern = source
    .replace(/\(\?([imsU]+)\)/g, (match, letters) => {
      for (const letter of letters) if (letter !== 'U') flags.add(letter);
      return '';
    })
    .replace(/\(\?([imsU]+):/g, (match, letters) => {
      for (const letter of letters) if (letter !== 'U') flags.add(letter);
      return '(?:';
    })
    .replace(/\(\?-[imsU]+:/g, '(?:')
    .replace(/\(\?P</g, '(?<')
    .replace(/\[:(\w+):\]/g, (match, name) => POSIX[name] || match)
    .replace(/\\z/g, '$')
    .replace(/\\A/g, '^');
  if (/\\x\{|\\p\{|\\Q/.test(pattern)) throw new Error('unsupported escape');
  new RegExp(pattern, [...flags, 'g', 'd'].join(''));
  return { pattern, flags: [...flags].join('') };
}

const convertAllow = (regex) => {
  const { pattern } = convert(regex);
  new RegExp(pattern, 'i');
  return pattern;
};

async function main() {
  const response = await fetch(SOURCE);
  if (!response.ok) throw new Error(`${SOURCE}: HTTP ${response.status}`);
  const config = parse(await response.text());
  const skipped = [];
  const rules = [];
  for (const rule of config.rules || []) {
    if (!rule.regex || !rule.keywords?.length) {
      skipped.push(`${rule.id} (no regex or keywords)`);
      continue;
    }
    try {
      const { pattern, flags } = convert(rule.regex);
      const allow = { regexes: [], stopwords: [] };
      for (const list of rule.allowlists || []) {
        // Path-based and combined ("AND") allowlists need file context we don't have.
        if (list.paths?.length || list.condition === 'AND') continue;
        for (const regex of list.regexes || []) allow.regexes.push(convertAllow(regex));
        allow.stopwords.push(...(list.stopwords || []));
      }
      rules.push({
        id: `gitleaks.${rule.id}`,
        label: rule.description || rule.id,
        category: 'secret',
        pattern,
        flags,
        ...(rule.secretGroup ? { group: rule.secretGroup } : { firstGroup: true }),
        keywords: rule.keywords.map((word) => word.toLowerCase()),
        ...(rule.entropy ? { minEntropy: rule.entropy } : {}),
        accept: 'notPlaceholder',
        score: 0.9,
        ...(allow.regexes.length || allow.stopwords.length ? { allow } : {}),
      });
    } catch (error) {
      skipped.push(`${rule.id} (${error.message})`);
    }
  }
  const global = config.allowlist || {};
  const allow = { regexes: [], stopwords: global.stopwords || [] };
  for (const regex of global.regexes || []) {
    try {
      allow.regexes.push(convertAllow(regex));
    } catch {
      skipped.push(`global allowlist regex ${regex}`);
    }
  }
  const pack = {
    source: `gitleaks ${TAG} (${SOURCE}), converted by scripts/import_gitleaks.js`,
    license: 'MIT, Copyright (c) 2019 Zachary Rice. See NOTICE.',
    allow,
    rules,
  };
  fs.writeFileSync(OUT, `${JSON.stringify(pack, null, 1)}\n`);
  console.log(`${rules.length} rules written to ${path.relative(process.cwd(), OUT)}`);
  if (skipped.length) console.log(`Skipped ${skipped.length}:\n  ${skipped.join('\n  ')}`);
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
