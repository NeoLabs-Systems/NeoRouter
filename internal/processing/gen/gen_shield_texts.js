#!/usr/bin/env node
'use strict';

// Copies the texts whose exact bytes matter out of the JavaScript sources
// (server/services/processing/redaction.js and compression.js) into
// internal/processing/shield/data/texts.json, so the Go port repeats them
// character for character instead of retyping them:
//
//   node internal/processing/gen/gen_shield_texts.js      (from the repo root)
//
// Each extraction is checked, so a change in the JavaScript fails here instead
// of quietly drifting.
const fs = require('node:fs');
const path = require('node:path');

const ROOT = path.join(__dirname, '..', '..', '..');
const OUT = path.join(ROOT, 'internal', 'processing', 'shield', 'data', 'texts.json');

const redactionSource = fs.readFileSync(path.join(ROOT, 'server', 'services', 'processing', 'redaction.js'), 'utf8');
const compressionSource = fs.readFileSync(path.join(ROOT, 'server', 'services', 'processing', 'compression.js'), 'utf8');

function extract(source, pattern, what) {
  const match = source.match(pattern);
  if (!match) throw new Error(`${what}: pattern not found`);
  return match[1];
}

// The instruction appended to a redacted conversation: a concatenation of string
// literals.
const instructionExpr = extract(redactionSource, /const INSTRUCTION = ([\s\S]*?);\n/, 'INSTRUCTION');
const instruction = new Function(`return ${instructionExpr};`)();
if (!instruction.includes('placeholders') || instruction.length < 200) throw new Error('INSTRUCTION looks wrong');

// The annotations the compression engines insert.
const repeatedLine = extract(compressionSource, /output\.push\(`(\[… previous line repeated [^`]*)`\)/, 'repeated line')
  .replace('${run - 1}', '%d');
const omittedChars = extract(compressionSource, /(\[… \$\{omitted\.toLocaleString\('en'\)\} characters omitted to save space …\])/, 'omitted characters')
  .replace('${omitted.toLocaleString(\'en\')}', '%s');
const duplicateBlock = extract(compressionSource, /'(\[Same content as a later message[^']*\])'/, 'duplicate block');
const maskedTool = extract(compressionSource, /(\[Older tool output omitted to save space\.[^\]]*\])/, 'masked tool output');
const earlierMessage = extract(compressionSource, /'(\[Earlier message omitted to fit the context budget\.\])'/, 'earlier message');

const texts = {
  // redaction.js INSTRUCTION
  redactionInstruction: instruction,
  // compression.js annotations
  repeatedLine,
  omittedChars,
  duplicateBlock,
  maskedTool,
  earlierMessage,
};

for (const [name, value] of Object.entries(texts)) {
  if (!value || typeof value !== 'string') throw new Error(`${name} is empty`);
}
fs.writeFileSync(OUT, `${JSON.stringify(texts, null, 1)}\n`);
console.log(`texts.json: ${Object.keys(texts).join(', ')}`);