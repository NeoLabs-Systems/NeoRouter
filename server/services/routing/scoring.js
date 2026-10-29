'use strict';

const stats = require('./stats_store');
const loadTracker = require('./load_tracker');
const concurrency = require('./concurrency');
const plans = require('./plans');
const { taskIntelligence, enoughIntelligence } = require('./task/skills');
const experience = require('./task/experience');

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
const UNIT_PRICE_RANGES = { image: [0.002, 0.2], second: [0.01, 0.5], minute: [0.001, 0.1], mchar: [1, 100], search: [0.0005, 0.03] };

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
  if (entry.isFree || entry.isPlan) return 0;
  if (entry.priceIn === null || entry.priceOut === null) return null;
  return (entry.priceIn * 3 + entry.priceOut) / 4;
}

function cost(entry, now = Date.now()) {
  // Plan models spend an allowance instead of money (plans.js).
  if (entry.isPlan) return 1 - plans.allowanceCost(entry, now);
  // Media models priced per image, second, minute or million characters.
  if (!entry.isFree && !entry.isPlan && entry.priceUnit && UNIT_PRICE_RANGES[entry.priceUnit] && entry.unitPrice !== null && entry.unitPrice !== undefined) {
    return entry.unitPrice === 0 ? 1 : 1 - (logScale(entry.unitPrice, UNIT_PRICE_RANGES[entry.priceUnit]) ?? 0);
  }
  const price = blendedPrice(entry);
  if (price === 0) return 1;
  if (price === null) return UNKNOWN_PRICE_COST;
  return 1 - (logScale(price, PRICE_RANGE) ?? 0);
}

// Difficulty-aware weights (when the profile is adaptive): hard requests
// lean towards intelligence, easy ones ("hi", a quick translation) towards
// price, since an easy prompt is not worth paying more for. Between the two
// bands the profile's weights stand.
const EASY_BELOW = 0.3;
const HARD_ABOVE = 0.5;
const MAX_EASY_SHIFT = 0.5;

function adaptWeights(weights, difficulty, adaptive) {
  if (!adaptive || difficulty === null || difficulty === undefined) return weights;
  if (difficulty < EASY_BELOW) {
    const moved = weights.intelligence * MAX_EASY_SHIFT * ((EASY_BELOW - difficulty) / EASY_BELOW);
    return { ...weights, intelligence: weights.intelligence - moved, cost: weights.cost + moved };
  }
  if (difficulty <= HARD_ABOVE) return weights;
  const shift = 0.3 * (difficulty - HARD_ABOVE);
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

// `task` (classifier.js) makes intelligence task-aware (benchmark skills and
// feedback) and reliability lean on the model's track record for this kind
// of task. `need` is the intelligence the task calls for (skills.js): above
// it, more intelligence counts only as much as the style weighs it.
function score(entry, { weights, quotaFactor = 1, random, sample = true, now = Date.now(), task = null, need = null }) {
  const snapshot = stats.snapshot(entry.ref, now);
  const learned = experience.adjust(task ? entry.ref : null, task?.primary, reliability(snapshot, { sample, random }), now);
  const totalWeight = weights.intelligence + weights.speed + weights.reliability + weights.cost;
  const capability = Math.min(1, taskIntelligence(entry.intelligence ?? DEFAULT_INTELLIGENCE, entry.skills, task?.skills) * learned.quality);
  const features = {
    intelligence: enoughIntelligence(capability, need, weights.intelligence / totalWeight),
    speed: speed(entry, snapshot),
    reliability: learned.reliability,
    cost: cost(entry, now),
  };
  const base = (weights.intelligence * features.intelligence + weights.speed * features.speed
    + weights.reliability * features.reliability + weights.cost * features.cost) / totalWeight;
  const penaltyFactor = 1 - PENALTY_IMPACT * Math.min(snapshot.penalty, MAX_PENALTY) / MAX_PENALTY;
  const load = loadTracker.inflight(entry);
  // A known concurrency limit is the account's real capacity.
  const accountCapacity = concurrency.limitFor(entry)?.limit || DEFAULT_CAPACITY * 4;
  const loadFactor = 1 / (1 + load.model / DEFAULT_CAPACITY + load.account / accountCapacity);
  return {
    score: base * quotaFactor * penaltyFactor * loadFactor,
    base,
    features,
    factors: { quota: quotaFactor, penalty: penaltyFactor, load: loadFactor },
  };
}

module.exports = { score, reliability, speed, cost, blendedPrice, adaptWeights, sampleBeta, logScale };
