'use strict';
// Resource and evidence bounds, not production traffic reserves or user quota sliders.
module.exports = Object.freeze({
  aggregationBatchSize: 128, pruneMs: 60000, maxEvidencePerGroup: 512,
  tickMs: 1000, retryMs: 30000, maxRetryMs: 3600000, leaseMs: 360000,
  retentionDays: 7, maxSamplesPerUser: 128, maxSamples: 2048, pendingCaptures: 8, pendingCaptureMs: 30000,
  maxPayloadBytes: 65536, maxOutputTokens: 4096, judgeTokens: 768, timeoutMs: 45000,
  maxAttempts: 6, minSamples: 8, priorSamples: 32, maxRoutingShare: 0.15,
  minConfidence: 0.85, halfLifeMs: 30 * 86400000, evidenceRetentionMs: 90 * 86400000,
});
