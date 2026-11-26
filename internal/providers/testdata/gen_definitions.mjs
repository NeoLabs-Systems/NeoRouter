// Generates the provider definition data the Go port embeds.
//
//   node internal/providers/testdata/gen_definitions.mjs
//
// The JS definition modules (server/providers/definitions/*.js) are the source
// of truth; registry.js parses them with its zod schema, which fills in the
// documented defaults and drops unknown keys. This script dumps that parsed
// result, so the Go data is byte-for-byte the JS data and needs no defaults of
// its own.
//
// Two files are written, with identical content:
//   internal/providers/definitions.json         embedded in the binary
//   internal/providers/testdata/js-definitions.json   fixture the Go test
//                                               compares the Go types against
//
// The only value JSON cannot carry is `quota.probe.parse`, a JavaScript
// function per definition. It is written as the name of the Go function that
// ports it (providers.Load resolves the names through quotaParsers); the map
// below is checked so a new definition with a parser cannot be generated
// silently unnamed.
//
// The definition modules read two base URLs from the environment at load time
// (NEOROUTER_NOUS_BASE_URL, NEOROUTER_CODEX_BASE_URL). They must be unset when
// this runs, so the dumped data is the documented default; providers.Load
// applies the overrides at startup.

import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import fs from 'node:fs';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..', '..', '..');
const require = createRequire(import.meta.url);

for (const name of ['NEOROUTER_NOUS_BASE_URL', 'NEOROUTER_CODEX_BASE_URL']) {
  if (process.env[name]) {
    console.error(`${name} is set; unset it so the dumped base URLs are the defaults.`);
    process.exit(1);
  }
}

// definition id -> name of the Go port of its quota probe parser. These mirror
// the functions next to the definitions (plans.js exports two of them).
const quotaParsers = {
  openrouter: 'openrouter_credits',
  deepseek: 'deepseek_balance',
  siliconflow: 'siliconflow_balance',
  synthetic: 'synthetic_plan',
  minimax_plan: 'minimax_windows',
  opencode_go: 'opencode_go_windows',
};

const registry = require(path.join(root, 'server', 'providers', 'registry'));
const definitions = registry.list();

const out = definitions.map((definition) => {
  const copy = JSON.parse(JSON.stringify(definition));
  const parse = definition.quota?.probe?.parse;
  if (typeof parse === 'function') {
    const name = quotaParsers[definition.id];
    if (!name) {
      console.error(`No Go parser name for ${definition.id}'s quota probe; add it to quotaParsers.`);
      process.exit(1);
    }
    copy.quota.probe.parse = name;
  }
  return copy;
});

const text = `${JSON.stringify(out, null, 2)}\n`;
for (const target of [
  path.join(root, 'internal', 'providers', 'definitions.json'),
  path.join(here, 'js-definitions.json'),
]) {
  fs.writeFileSync(target, text);
  console.log(`wrote ${path.relative(root, target)} (${out.length} definitions, ${Buffer.byteLength(text)} bytes)`);
}