'use strict';

// Bun's runner shares modules across files. Separate processes preserve the
// isolation used by node --test, including each file's temporary runtime.
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { spawn } = require('node:child_process');
const root = path.resolve(__dirname, '../..');
const bun = process.env.BUN_BINARY;
if (!bun) throw new Error('Set BUN_BINARY.');
const files = ['test/unit', 'test/api'].flatMap((dir) => fs.readdirSync(path.join(root, dir)).filter((name) => name.endsWith('.test.js')).map((name) => `${dir}/${name}`)).sort();
const output = process.env.TEST_OUTPUT || path.join(__dirname, 'test-results.json');
const logDir = fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-bun-tests-'));
async function run(file) {
  const result = { file, exit: null, pass: 0, fail: 0 };
  const log = path.join(logDir, file.replaceAll('/', '_') + '.log');
  const child = spawn(bun, ['test', '--timeout', '45000', file], { cwd: root, stdio: ['ignore', 'pipe', 'pipe'] });
  let text = '';
  child.stdout.on('data', (data) => { text += data; });
  child.stderr.on('data', (data) => { text += data; });
  const timeout = setTimeout(() => child.kill('SIGKILL'), 90000);
  result.exit = await new Promise((resolve) => child.on('exit', (code, signal) => resolve(code ?? signal)));
  clearTimeout(timeout);
  fs.writeFileSync(log, text);
  result.pass = Number(text.match(/^\s*(\d+) pass$/m)?.[1] || 0);
  result.fail = Number(text.match(/^\s*(\d+) fail$/m)?.[1] || 0);
  if (result.exit !== 0) console.log(JSON.stringify({ file, exit: result.exit, log }));
  return result;
}
(async () => {
  const results = [];
  let next = 0;
  async function worker() { while (next < files.length) results.push(await run(files[next++])); }
  await Promise.all([worker(), worker()]);
  fs.writeFileSync(output, JSON.stringify(results.sort((a, b) => a.file.localeCompare(b.file)), null, 2));
  console.log(JSON.stringify({ files: results.length, passed: results.reduce((sum, row) => sum + row.pass, 0), failed: results.reduce((sum, row) => sum + row.fail, 0), failedFiles: results.filter((row) => row.exit !== 0).length }));
  if (results.some((row) => row.exit !== 0)) process.exitCode = 1;
})().catch((error) => { console.error(error); process.exitCode = 1; });
