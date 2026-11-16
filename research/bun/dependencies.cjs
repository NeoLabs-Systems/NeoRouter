'use strict';

const { spawnSync } = require('node:child_process');
const bcrypt = require('bcrypt');
const bun = process.env.BUN_BINARY;
const passwords = ['existing-short-password', 'Grüße-東京', 'a'.repeat(100)];
const fixtures = passwords.map((password) => ({ password, hash: bcrypt.hashSync(password, 4) }));
const code = `
const fixtures = JSON.parse(await Bun.stdin.text());
const bcrypt = require('bcrypt');
const results = [];
for (const fixture of fixtures) results.push({ bytes: Buffer.byteLength(fixture.password), bcrypt: await bcrypt.compare(fixture.password, fixture.hash), bunPassword: await Bun.password.verify(fixture.password, fixture.hash) });
console.log(JSON.stringify(results));`;
const result = spawnSync(bun, ['-e', code], { input: JSON.stringify(fixtures), encoding: 'utf8', cwd: process.cwd() });
if (result.status !== 0) throw new Error(result.stderr);
console.log(result.stdout.trim());
