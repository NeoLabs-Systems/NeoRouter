'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { renderSystemdUnit } = require('../../lib/manager');

// systemd refuses a unit with a quoted WorkingDirectory= ("path is not
// absolute") and skips a quoted EnvironmentFile=.
test('the systemd unit only quotes the settings that unquote their values', () => {
  const lines = renderSystemdUnit().split('\n');
  const value = (key) => lines.find((line) => line.startsWith(`${key}=`)).slice(key.length + 1);
  assert.match(value('WorkingDirectory'), /^\//);
  assert.match(value('EnvironmentFile'), /^-\//);
  assert.match(value('StandardOutput'), /^append:\//);
  assert.match(value('ExecStart'), /^"\/.+" "\/.+index\.js"$/);
  assert.ok(lines.includes(`Environment="NEOROUTER_HOME=${process.env.NEOROUTER_HOME}"`));
});
