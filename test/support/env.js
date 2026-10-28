'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

// Every test file gets an isolated runtime home and database.
const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-test-'));
process.env.NODE_ENV = 'test';
process.env.NEOROUTER_HOME = home;
process.env.NEOROUTER_WORKERS_ENABLED = 'false';
process.env.NEOROUTER_METADATA_SOURCES_ENABLED = 'false';
process.env.NEOROUTER_LOG_LEVEL = 'error';

module.exports = { home };
