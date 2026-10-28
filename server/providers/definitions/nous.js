'use strict';

module.exports = {
  id: 'nous',
  label: 'Nous Research',
  description: 'Hermes models via the Nous Portal inference API.',
  category: 'paid',
  website: 'https://portal.nousresearch.com',
  keyUrl: 'https://portal.nousresearch.com/api-keys',
  adapter: 'openai_compatible',
  baseUrl: 'https://inference-api.nousresearch.com/v1',
  billing: { defaultMode: 'paid', alwaysFree: [':free$'], note: 'Paid models draw from prepaid Portal credits; only :free models are free.', source: 'https://portal.nousresearch.com/', checkedAt: '2026-10-28' },
};
