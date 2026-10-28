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
  billing: { defaultMode: 'paid', freeTierAll: true, note: 'Draws from Portal credits; choose "free" only if this account cannot be charged.' },
};
