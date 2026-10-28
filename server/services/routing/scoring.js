'use strict';

const stats = require('./stats_store');
const loadTracker = require('./load_tracker');

// Feature scaling ranges (log scale). Centralized so they can be tuned.
const TPS_RANGE = [10, 1500];
const TTFT_RANGE = [150, 15000];
const PRICE_RANGE = [0.05, 60];
const DEFAULT_INTELLIGENCE = 0.4;
const UNKNOWN_PRICE_COST = 0.3;
const RELIABILITY_PRIOR = { alpha: 2, beta: 1 };
const MAX_PENALTY = 10;
const PENALTY_IMPACT = 0.6;
const DEFAULT_CAPACITY = 4;

function logScale(value, [low, high]) {
  if (!Number.isFinite(value) || value <= 0) return null;
  return Math.min(1, Math.max(0, (Math.log(value) - Math.log(low)) / (Math.log(high) - Math.log(low))));
}

// Marsaglia–Tsang gamma sampler, used for Beta sampling below.
function sampleGamma(shape, random = Math.random) {
  if (shape < 1) return sampleGamma(shape + 1, random) * random() ** (1 / shape);
  const d = shape - 1 / 3;
  const c = 1 / Math.sqrt(9 * d);
  for (;;) {
    let x;
    let v;
    do {
      const u1 = random() || 1e-12;
      const u2 = random();
      x = Math.sqrt(-2 * Math.log(u1)) * Math.cos(2 * Math.PI * u2);
      v = 1 + c * x;
    } while (v <= 0);
    v = v ** 3;
    const u = random();
    if (u < 1 - 0.0331 * x ** 4 || Math.log(u) < 0.5 * x * x + d * (1 - v + Math.log(v))) return d * v;
  }
}

function sampleBeta(alpha, beta, random = Math.random) {
  const x = sampleGamma(alpha, random);
  const y = sampleGamma(beta, random);
  return x / (x + y);
}

// Thompson sampling: models with little evidence get a wide distribution and
// are explored now and then; proven models get a tight, high estimate.
function reliability(snapshot, { sample = true, random } = {}) {
  const alpha = RELIABILITY_PRIOR.alpha + snapshot.successes;
  const beta = RELIABILITY_PRIOR.beta + snapshot.failures;
  return sample ? sampleBeta(alpha, beta, random) : alpha / (alpha + beta);
}

const LATENCY_RANGE = [200, 120_000];
// Cheap to expensive, in USD per unit.
const UNIT_PRICE_RANGES = { image: [0.002, 0.2], second: [0.01, 0.5], minute: [0.001, 0.1], mchar: [1, 100] };

function speed(entry, snapshot) {
  const tps = logScale(snapshot.tps ?? entry.speedPriorTps, TPS_RANGE);
  const ttft = snapshot.ttft_ms ? 1 - logScale(snapshot.ttft_ms, TTFT_RANGE) : null;
  if (tps === null && ttft === null) {
    // Media calls (embeddings, images, audio) have no token stream: total latency it is.
    return snapshot.latency_ms ? 1 - (logScale(snapshot.latency_ms, LATENCY_RANGE) ?? 0) : 0.5;
  }
  if (tps === null) return ttft;
  if (ttft === null) return tps;
  return 0.6 * tps + 0.4 * ttft;
}

function blendedPrice(entry) {
  if (entry.isFree) return 0;
  if (entry.priceIn === null || entry.priceOut === null) return null;
  return (entry.priceIn * 3 + entry.priceOut) / 4;
}

function cost(entry) {
  // Media models priced per image, second, minute or million characters.
  if (!entry.isFree && entry.priceUnit && UNIT_PRICE_RANGES[entry.priceUnit] && entry.unitPrice !== null && entry.unitPrice !== undefined) {
    return entry.unitPrice === 0 ? 1 : 1 - (logScale(entry.unitPrice, UNIT_PRICE_RANGES[entry.priceUnit]) ?? 0);
  }
  const price = blendedPrice(entry);
  if (price === 0) return 1;
  if (price === null) return UNKNOWN_PRICE_COST;
  return 1 - (logScale(price, PRICE_RANGE) ?? 0);
}

// Shifts weight toward intelligence for demanding requests (long context,
// tools, code) when the profile allows adaptive routing.
function adaptWeights(weights, complexity, adaptive) {
  if (!adaptive || complexity < 0.5) return weights;
  const shift = 0.3 * (complexity - 0.5);
  const others = weights.speed + weights.reliability + weights.cost;
  if (others === 0) return weights;
  const scale = Math.max(0, 1 - shift / others);
  return {
    intelligence: weights.intelligence + shift,
    speed: weights.speed * scale,
    reliability: weights.reliability * scale,
    cost: weights.cost * scale,
  };
}

function score(entry, { weights, quotaFactor = 1, random, sample = true, now = Date.now() }) {
  const snapshot = stats.snapshot(entry.ref, now);
  const features = {
    intelligence: entry.intelligence ?? DEFAULT_INTELLIGENCE,
    speed: speed(entry, snapshot),
    reliability: reliability(snapshot, { sample, random }),
    cost: cost(entry),
  };
  const totalWeight = weights.intelligence + weights.speed + weights.reliability + weights.cost;
  const base = (weights.intelligence * features.intelligence + weights.speed * features.speed
    + weights.reliability * features.reliability + weights.cost * features.cost) / totalWeight;
  const penaltyFactor = 1 - PENALTY_IMPACT * Math.min(snapshot.penalty, MAX_PENALTY) / MAX_PENALTY;
  const load = loadTracker.inflight(entry);
  const loadFactor = 1 / (1 + load.model / DEFAULT_CAPACITY + load.account / (DEFAULT_CAPACITY * 4));
  return {
    score: base * quotaFactor * penaltyFactor * loadFactor,
    base,
    features,
    factors: { quota: quotaFactor, penalty: penaltyFactor, load: loadFactor },
  };
}

module.exports = { score, reliability, speed, cost, blendedPrice, adaptWeights, sampleBeta, logScale };
