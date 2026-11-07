'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { summarizeLimits } = require('../../server/services/quota/limit_summary');

const win = (metric, limit, source = 'curated') => ({ metric, limit, source });

test('a shared account limit and a per-model range are reported separately', () => {
  const rows = summarizeLimits({
    pool: [win('rpm', 20)],
    models: [{ windows: [win('rpm', 3, 'learned'), win('rpd', 250)] }, { windows: [win('rpm', 100, 'header'), win('rpd', 1000)] }],
  });
  assert.deepEqual(rows.map((row) => row.metric), ['rpm', 'rpd']);
  assert.deepEqual(rows[0].shared, { limit: 20, source: 'curated' });
  assert.deepEqual(rows[0].perModel, { min: 3, max: 100, models: 2, sources: ['header', 'learned'] });
  assert.equal(rows[1].shared, null);
  assert.deepEqual(rows[1].perModel, { min: 250, max: 1000, models: 2, sources: ['curated'] });
});

test('used-up markers and empty limits are not limits', () => {
  const rows = summarizeLimits({
    pool: [{ metric: 'rpd', limit: null, source: 'error' }],
    models: [{ windows: [{ metric: 'rpm', limit: 0, source: 'error' }, { metric: 'tpm', limit: null, source: 'curated' }] }],
  });
  assert.deepEqual(rows, []);
});

test('metrics come out in a fixed order', () => {
  const rows = summarizeLimits({ pool: [win('tpd', 5), win('rpm', 1)], models: [] });
  assert.deepEqual(rows.map((row) => row.metric), ['rpm', 'tpd']);
});
