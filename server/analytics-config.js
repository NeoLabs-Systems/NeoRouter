'use strict';

const COUNTLY_ENDPOINT = new URL('https://data.neo.eu.com/i');

const config = Object.freeze({
  endpoint: COUNTLY_ENDPOINT.href,
  origin: COUNTLY_ENDPOINT.origin,
  appKey: 'fd1d80746c13491c9744ecc96bf096da4f10cffd',
});

function analyticsConfig() {
  const enabled = (process.env.ANALYTICS_ENABLED || '').trim().toLowerCase();
  return ['false', '0', 'off', 'no'].includes(enabled) ? null : config;
}

module.exports = { analyticsConfig };
