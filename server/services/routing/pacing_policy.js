'use strict';

// Shared pacing policy. Traffic shape, token size and plan consumption rates
// are measured; these only bound responsiveness and burst tolerance.
module.exports = Object.freeze({
  historyMs: 14 * 86_400_000,
  historyHalfLifeMs: 7 * 86_400_000,
  cacheMs: 60_000,
  profileBlend: 0.6,
  peakRoom: 1.6,
  minHourShare: 1 / 48,
  dailyCushion: 0.03,
  planCushion: 0.03,
  planRateAlpha: 0.2,
  softPressureStart: 0.5,
  softPressureWeight: 2,
  outputPriorSamples: 10,
  inflightRecheckMs: 250,
  retrySearchMs: 60_000,
});
