'use strict';

module.exports = {
  id: 'nous',
  label: 'Nous Research',
  description: 'Hermes models via the Nous Portal inference API.',
  category: 'paid',
  website: 'https://portal.nousresearch.com',
  keyUrl: 'https://portal.nousresearch.com/api-keys',
  adapter: 'nous',
  // Overridable for tests only.
  baseUrl: process.env.NEOROUTER_NOUS_BASE_URL || 'https://inference-api.nousresearch.com/v1',
  // Signing in with the Portal account makes the subscription's models and
  // credits available without copying a key; a pasted API key works too.
  connect: 'nous_device',
  billing: { defaultMode: 'paid', alwaysFree: [':free$'], note: 'Paid models draw from prepaid Portal credits; only :free models are free.', source: 'https://portal.nousresearch.com/', checkedAt: '2026-10-28' },
};
