'use strict';

// The free capacity estimate must not treat a minute limit stretched over a
// day as a daily cap.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const quota = require('../../server/services/quota/quota_service');
const { limitsOf } = require('../../server/services/quota/capacity');

test.before(() => migrate(getDatabase(), { quiet: true }));
test.beforeEach(() => quota.resetForTests());

const scope = (key) => ({ key, curated: {}, kind: 'model' });

test('a daily limit that is only the minute limit times 1440 counts per minute', () => {
  quota.setLimit('m:a', 'rpm', 450, 'header');
  quota.setLimit('m:a', 'rpd', 450 * 1440, 'header');
  quota.setLimit('m:a', 'tpm', 150_000, 'header');
  quota.setLimit('m:a', 'tpd', 150_000 * 1440, 'header');
  const limits = limitsOf(scope('m:a'), 3000);
  assert.equal(limits.perDay, null);
  assert.equal(limits.tokensPerDay, null);
  assert.equal(limits.tokensPerMinute, 150_000);
});

test('a real daily limit still counts per day', () => {
  quota.setLimit('m:b', 'rpm', 30, 'header');
  quota.setLimit('m:b', 'rpd', 14_400, 'header');
  quota.setLimit('m:b', 'tpd', 500_000, 'header');
  const limits = limitsOf(scope('m:b'), 3000);
  assert.equal(limits.perDay, 14_400);
  assert.equal(limits.tokensPerDay, 500_000);
});

test('a daily request limit is capped by what the minute token limit allows', () => {
  quota.setLimit('m:c', 'rpm', 30, 'header');
  quota.setLimit('m:c', 'rpd', 14_400, 'header');
  quota.setLimit('m:c', 'tpm', 15_000, 'header');
  const limits = limitsOf(scope('m:c'), 3079);
  assert.equal(limits.tokensPerDay, 15_000 * 1440);
});
