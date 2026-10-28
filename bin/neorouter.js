#!/usr/bin/env node
'use strict';

const { runCLI } = require('../lib/manager');

runCLI(process.argv.slice(2)).catch((err) => {
  console.error(`[neorouter] ${err.message}`);
  process.exit(1);
});
