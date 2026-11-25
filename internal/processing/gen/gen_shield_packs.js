#!/usr/bin/env node
'use strict';

// Converts the JavaScript shield rule packs (server/services/processing/shield/
// rules/*.js) into the JSON packs the Go port embeds, so the Go data is the
// same data the Node server loads — written mechanically, never by hand.
//
//   node internal/processing/gen/gen_shield_packs.js     (from the repo root)
//
// categories.json, data/*.txt and rules/secrets_gitleaks.json are copied as
// they are (see the Makefile-free `cp` commands in the README of this folder);
// this script only rewrites the packs that are JavaScript modules:
//
//   rules/contact.js  -> internal/processing/shield/data/pack_contact.json
//   ... one pack_<name>.json per source file, in the same shape the JS
//   registry reads ({ source?, license?, allow?, rules: [...] }), except that
//   a RegExp pattern becomes {"source","flags"} so nothing is lost.
const fs = require('node:fs');
const path = require('node:path');

const ROOT = path.join(__dirname, '..', '..', '..');
const RULES = path.join(ROOT, 'server', 'services', 'processing', 'shield', 'rules');
const OUT = path.join(ROOT, 'internal', 'processing', 'shield', 'data');

const patternOf = (value) => (value instanceof RegExp ? { source: value.source, flags: value.flags } : value);

// Go's regexp engine has no backreferences. The one rule that uses one is a
// repeated separator, which two alternatives express exactly: a MAC address is
// written with colons or with dashes, never with both.
const PATTERN_REWRITES = {
  mac_address: {
    from: /\b[0-9A-Fa-f]{2}([:-])(?:[0-9A-Fa-f]{2}\1){4}[0-9A-Fa-f]{2}\b|\b[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}\b/g,
    to: /\b[0-9A-Fa-f]{2}(?::[0-9A-Fa-f]{2}){5}\b|\b[0-9A-Fa-f]{2}(?:-[0-9A-Fa-f]{2}){5}\b|\b[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}\b/g,
    why: 'backreference (Go RE2 has none): a MAC address never mixes separators, so spell out both',
  },
};

function convertRule(rule) {
  const out = {};
  for (const [key, value] of Object.entries(rule)) {
    out[key] = key === 'pattern' ? patternOf(value) : value;
  }
  const rewrite = PATTERN_REWRITES[rule.id];
  if (rewrite) {
    if (!(rule.pattern instanceof RegExp) || rule.pattern.source !== rewrite.from.source) {
      throw new Error(`rewrite for ${rule.id} no longer matches the JS pattern`);
    }
    out.pattern = { source: rewrite.to.source, flags: rewrite.to.flags };
    out.rewrite = rewrite.why;
  }
  return out;
}

function convertPack(exported) {
  const pack = Array.isArray(exported) ? { rules: exported } : exported;
  return {
    ...(pack.source ? { source: pack.source } : {}),
    ...(pack.license ? { license: pack.license } : {}),
    ...(pack.allow ? { allow: pack.allow } : {}),
    rules: pack.rules.map(convertRule),
  };
}

fs.mkdirSync(OUT, { recursive: true });
let packs = 0;
let rules = 0;
for (const file of fs.readdirSync(RULES).filter((name) => name.endsWith('.js')).sort()) {
  const name = file.replace(/\.js$/, '');
  const pack = convertPack(require(path.join(RULES, file)));
  fs.writeFileSync(path.join(OUT, `pack_${name}.json`), `${JSON.stringify(pack, null, 1)}\n`);
  packs += 1;
  rules += pack.rules.length;
  console.log(`pack_${name}.json: ${pack.rules.length} rules`);
}
console.log(`${packs} packs, ${rules} rules`);