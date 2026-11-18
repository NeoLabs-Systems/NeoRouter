'use strict';

module.exports = Object.freeze({
  tickMs: 1000,
  parallel: 4,
  sampleLifetimeMs: 86_400_000,
  maxSamples: 512,
  maxSamplesPerUser: 32,
  maxPayloadBytes: 65_536,
  maxOutputTokens: 4096,
  judgeTokens: 2048,
  judgeConfidence: 0.85,
  retryMs: 30_000,
  maxRetryMs: 900_000,
  maxFailures: 4,
  minSamples: 8,
  priorSamples: 32,
  maxRoutingShare: 0.15,
  halfLifeMs: 30 * 86_400_000,
  evidenceLifetimeMs: 90 * 86_400_000,
  bucketMs: 86_400_000,
  maxDailySamplesPerGroup: 32,
});
