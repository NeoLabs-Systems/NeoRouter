'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { choosePreferredBranchForChannel } = require('../../runtime/release_channel');

test('the beta channel stays on the beta branch unless a stable release is newer', () => {
  assert.equal(choosePreferredBranchForChannel('beta', {}), 'beta', 'no tags at all');
  assert.equal(choosePreferredBranchForChannel('beta', { stable: '0.1.0' }), 'beta', 'no beta tag yet');
  assert.equal(choosePreferredBranchForChannel('beta', { beta: '0.2.0-beta.1', stable: '0.1.0' }), 'beta');
  assert.equal(choosePreferredBranchForChannel('beta', { beta: '0.2.0-beta.1', stable: '0.2.0' }), 'main', 'the stable release caught up');
  assert.equal(choosePreferredBranchForChannel('stable', { beta: '0.3.0-beta.1' }), 'main');
});
