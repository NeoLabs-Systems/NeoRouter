'use strict';

const catalog = require('../catalog/catalog_service');
const { canonicalId } = require('../catalog/model_identity');
const loadTracker = require('./load_tracker');
const concurrency = require('./concurrency');
const quota = require('../quota/quota_service');
const stats = require('./stats_store');
const scoring = require('./scoring');
const profiles = require('./profiles');
const budget = require('../budget/budget_service');
const { getConfig } = require('../../config/env');
const { HttpError } = require('../../middleware/error_handler');

const AUTO = 'auto';
const DEFAULT_OUTPUT_TOKENS = 1024;
const MIN_OUTPUT_RESERVE = 256;
const UNKNOWN_CAPABILITY_FACTOR = 0.85;
const COMPLEXITY_TOKEN_RANGE = [200, 30000];
const UNKNOWN_CONTEXT_LIMIT = 131072;
const RELATIVE_FEATURES = ['intelligence', 'speed', 'cost'];
const RELATIVE_BLEND = 0.5;
// Profiles that care about quality (intelligence weight at least this share)
// push candidates that are clearly weaker than the best one down hard, so
// a tiny model that is merely fast and reliable is not picked for them.
const QUALITY_PROFILE_SHARE = 0.25;
const QUALITY_GAP = 0.3;
const QUALITY_GATE_FACTOR = 0.45;
const FEATURE_FLOOR = 0.02;

function globToRegExp(pattern) {
  const escaped = String(pattern).replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.');
  return new RegExp(`^${escaped}$`, 'i');
}

function allowedByKey(entry, key) {
  const patterns = JSON.parse(key.allowed_models_json || '[]');
  if (!patterns.length) return true;
  return patterns.some((pattern) => globToRegExp(pattern).test(entry.id));
}

// Rough request difficulty in [0, 1]; only used to shift weights when the
// profile is adaptive.
function complexityOf(request, inputTokens) {
  const tokenSignal = scoring.logScale(inputTokens, COMPLEXITY_TOKEN_RANGE) ?? 0;
  const text = JSON.stringify(request.messages).slice(-20000);
  const codeSignal = /```|function\s|class\s|def\s|SELECT\s|=>/.test(text) ? 1 : 0;
  const toolSignal = request.tools?.length ? 1 : 0;
  return Math.min(1, 0.5 * tokenSignal + 0.25 * codeSignal + 0.25 * toolSignal);
}

function parseModel(requested) {
  const value = String(requested || '').trim();
  if (!value || value === AUTO) return { auto: true, profile: null };
  if (value.startsWith(`${AUTO}:`)) return { auto: true, profile: value.slice(AUTO.length + 1) };
  return { auto: false, model: value };
}

function maxOutputFor(request, entry) {
  return request.max_tokens || Math.min(entry.maxOutput || DEFAULT_OUTPUT_TOKENS, DEFAULT_OUTPUT_TOKENS);
}

// Hard filters. Returns a reason string when the entry cannot serve.
function rejection(entry, ctx, now) {
  const { key, needs, inputTokens, request, constraints, freeOnly } = ctx;
  if (!entry.enabled) return 'model_disabled';
  if (!entry.available) return 'model_unavailable';
  if (!entry.accountEnabled || entry.accountStatus === 'disabled' || entry.accountStatus === 'invalid') return 'account_unavailable';
  if (!allowedByKey(entry, key)) return 'not_allowed_for_key';
  if ((freeOnly || key.mode === 'free_only') && !entry.isFree) return 'not_free';
  if (needs.tools && entry.tools === false) return 'no_tools';
  if (needs.vision && entry.vision !== true) return 'no_vision';
  const reserve = Math.max(MIN_OUTPUT_RESERVE, Math.min(request.max_tokens || MIN_OUTPUT_RESERVE, 32000));
  if (entry.contextWindow && inputTokens + reserve > entry.contextWindow) return 'context_too_small';
  // An unknown window is not an infinite one: beyond this size only models
  // that publish their limit are tried.
  if (!entry.contextWindow && inputTokens + reserve > UNKNOWN_CONTEXT_LIMIT) return 'context_unknown';
  if (constraints.includeProviders?.length && !constraints.includeProviders.includes(entry.providerSlug) && !constraints.includeProviders.includes(entry.definitionId)) return 'provider_not_included';
  if (constraints.excludeProviders?.includes(entry.providerSlug) || constraints.excludeProviders?.includes(entry.definitionId)) return 'provider_excluded';
  if (constraints.excludeModels?.some((pattern) => globToRegExp(pattern).test(entry.id))) return 'model_excluded';
  if (constraints.minIntelligence !== null && constraints.minIntelligence !== undefined && (entry.intelligence ?? 0) < constraints.minIntelligence) return 'below_min_intelligence';
  if (constraints.maxPriceOut !== null && constraints.maxPriceOut !== undefined && !entry.isFree && (entry.priceOut === null || entry.priceOut > constraints.maxPriceOut)) return 'too_expensive';
  if (constraints.maxTtftMs && entry.stats.ttftMs && entry.stats.ttftMs > constraints.maxTtftMs) return 'too_slow';
  if (!entry.isFree) {
    const hasBudget = key.budget_usd !== null && key.budget_usd !== undefined;
    const maxCost = ctx.estimateCost ? ctx.estimateCost(entry) : budget.estimateMaxCost(entry, inputTokens, maxOutputFor(request, entry));
    if (hasBudget && maxCost === null) return 'price_unknown';
    if (hasBudget && maxCost > budget.remaining(key, now)) return 'budget_exhausted';
  }
  // Reasoning models that already ran out of budget at this size are skipped.
  const minOutput = stats.snapshot(entry.ref, now).min_output_tokens;
  if (request.max_tokens && minOutput && request.max_tokens < minOutput && !ctx.primaryRefs.has(entry.ref)) return 'needs_more_tokens';
  const available = stats.availability(entry.ref, now);
  if (!available.available) return available.reason;
  // An account at its (learned or documented) concurrency limit takes no more work right now.
  const parallel = concurrency.limitFor(entry, now);
  if (parallel && loadTracker.inflight(entry).account >= parallel.limit) return 'account_busy';
  const quotaState = quota.evaluate(entry, inputTokens, now);
  if (quotaState.blocked) return `quota_${quotaState.blocked.metric}`;
  return null;
}

// The same model on several accounts (two Groq keys, say): prefer the copy
// with more allowance left and more free capacity, so accounts wear evenly
// and a busy one hands work to its siblings. Only copies of one model are
// compared, so this never changes which model is chosen.
const SIBLING_FLOOR = 0.55;
const ACCOUNT_SOFT_CAPACITY = 8;

function freeCapacity(entry) {
  const inflight = loadTracker.inflight(entry).account;
  const parallel = concurrency.limitFor(entry);
  if (parallel) return Math.max(0, 1 - inflight / parallel.limit);
  return 1 / (1 + inflight / ACCOUNT_SOFT_CAPACITY);
}

function spreadAcrossAccounts(scored) {
  const groups = new Map();
  for (const item of scored) {
    if (!groups.has(item.entry.canonicalId)) groups.set(item.entry.canonicalId, []);
    groups.get(item.entry.canonicalId).push(item);
  }
  for (const group of groups.values()) {
    if (new Set(group.map((item) => item.entry.accountId)).size < 2) continue;
    // Small speed differences between copies are noise; health (penalties),
    // allowance left and free capacity are what tell them apart.
    const top = Math.max(...group.map((item) => item.score));
    const healthiest = Math.max(...group.map((item) => item.details.factors.penalty));
    const share = (item) => (item.quota?.headroom ?? 1) * freeCapacity(item.entry);
    const best = Math.max(...group.map(share));
    if (best <= 0) continue;
    for (const item of group) {
      const factor = SIBLING_FLOOR + (1 - SIBLING_FLOOR) * (share(item) / best);
      item.score = top * (item.details.factors.penalty / healthiest) * factor;
      item.details = { ...item.details, factors: { ...item.details.factors, sibling: factor } };
    }
  }
}

class RoutingPlan {
  constructor({ userId, key, request, requestedModel, needs, inputTokens, freeOnly = false, random = Math.random, capability = 'chat', estimateCost = null }) {
    this.capability = capability;
    // Non-chat kinds price per image, second or character; they pass their own estimate.
    this.estimateCost = estimateCost;
    this.key = key;
    this.request = request;
    this.needs = needs;
    this.inputTokens = inputTokens;
    this.random = random;
    this.freeOnly = freeOnly;
    this.tried = new Set();
    this.parsed = parseModel(requestedModel);
    const profileRef = this.parsed.profile || key.profile || 'balanced';
    this.profile = profiles.resolve(userId, profileRef);
    if (!this.profile) throw new HttpError(400, 'UNKNOWN_PROFILE', `Unknown routing profile "${profileRef}".`);
    this.complexity = complexityOf(request, inputTokens);
    this.weights = scoring.adaptWeights(this.profile.weights, this.complexity, this.profile.constraints.adaptive !== false);
    this.entries = catalog.listForUser(userId, { capability });
    this.primaryRefs = new Set();
    if (!this.parsed.auto) this.scopeToRequestedModel();
  }

  // Explicit models route to that exact entry first; the same model on the
  // user's other providers serves as fallback.
  scopeToRequestedModel() {
    const requested = this.parsed.model;
    let primary = this.entries.filter((entry) => entry.id === requested);
    if (!primary.length) primary = this.entries.filter((entry) => entry.modelId === requested);
    const slug = requested.split('/')[0];
    const modelPart = requested.includes('/') && this.entries.some((entry) => entry.providerSlug === slug)
      ? requested.slice(slug.length + 1)
      : requested;
    const canonical = primary[0]?.canonicalId || canonicalId(modelPart);
    const sameModel = this.entries.filter((entry) => entry.canonicalId === canonical);
    if (!primary.length && !sameModel.length) {
      throw new HttpError(404, 'MODEL_NOT_FOUND', `The model "${requested}" is not a ${this.capability} model in your catalog. Use GET /v1/models or "auto".`);
    }
    for (const entry of primary) this.primaryRefs.add(entry.ref);
    const refs = new Set([...primary, ...sameModel].map((entry) => entry.ref));
    this.entries = this.entries.filter((entry) => refs.has(entry.ref));
  }

  context() {
    return { key: this.key, needs: this.needs, inputTokens: this.inputTokens, request: this.request, constraints: this.profile.constraints, freeOnly: this.freeOnly, primaryRefs: this.primaryRefs, estimateCost: this.estimateCost };
  }

  rank({ includeRejected = false, sample = true, now = Date.now() } = {}) {
    const ctx = this.context();
    const ranked = [];
    for (const entry of this.entries) {
      if (this.tried.has(entry.ref)) continue;
      const reason = rejection(entry, ctx, now);
      if (reason) {
        if (includeRejected) ranked.push({ entry, rejected: reason });
        continue;
      }
      const quotaState = quota.evaluate(entry, this.inputTokens, now);
      const result = scoring.score(entry, { weights: this.weights, quotaFactor: quotaState.factor, random: this.random, sample, now });
      ranked.push({ entry, details: result, quota: quotaState });
    }
    // Absolute intelligence, speed and cost estimates sit close together for
    // most models. Blending in each candidate's position within this field
    // lets a profile's preference actually separate the candidates.
    const scored = ranked.filter((item) => !item.rejected);
    const span = {};
    for (const feature of RELATIVE_FEATURES) {
      const values = scored.map((item) => item.details.features[feature]);
      span[feature] = { min: Math.min(...values), max: Math.max(...values) };
    }
    const totalWeight = Object.values(this.weights).reduce((sum, value) => sum + value, 0) || 1;
    const qualityProfile = this.weights.intelligence / totalWeight >= QUALITY_PROFILE_SHARE;
    let bestIntelligence = 0;
    for (const item of scored) {
      const { min, max } = span.intelligence;
      const value = item.details.features.intelligence;
      const relative = max > min ? (value - min) / (max - min) : 0.5;
      bestIntelligence = Math.max(bestIntelligence, (1 - RELATIVE_BLEND) * value + RELATIVE_BLEND * relative);
    }
    for (const item of scored) {
      const features = { ...item.details.features };
      for (const feature of RELATIVE_FEATURES) {
        const { min, max } = span[feature];
        const relative = max > min ? (features[feature] - min) / (max - min) : 0.5;
        features[feature] = (1 - RELATIVE_BLEND) * features[feature] + RELATIVE_BLEND * relative;
      }
      // Weighted geometric mean: a model has to be decent on every axis that
      // matters; one very weak axis cannot be made up by the others.
      const base = Math.exp(Object.entries(this.weights)
        .reduce((sum, [feature, weight]) => sum + (weight / totalWeight) * Math.log(Math.max(features[feature], FEATURE_FLOOR)), 0));
      const { quota: quotaFactor, penalty, load } = item.details.factors;
      let value = base * quotaFactor * penalty * load;
      if (qualityProfile && features.intelligence < bestIntelligence - QUALITY_GAP) value *= QUALITY_GATE_FACTOR;
      if ((this.needs.tools && item.entry.tools === null) || (this.needs.json && item.entry.json === false)) value *= UNKNOWN_CAPABILITY_FACTOR;
      item.score = value;
      item.details = { ...item.details, base, features };
    }
    spreadAcrossAccounts(scored);
    // The model (or account) the client asked for comes first.
    for (const item of scored) if (this.primaryRefs.has(item.entry.ref)) item.score += 10;
    ranked.sort((a, b) => (b.score ?? -1) - (a.score ?? -1));
    return ranked;
  }

  // Softmax draw among the top-k so load spreads across good candidates
  // instead of hammering the single best one.
  next({ filter = null, now = Date.now() } = {}) {
    const ranked = this.rank({ now }).filter((item) => !filter || filter(item.entry));
    if (!ranked.length) return null;
    let choice = ranked[0];
    // Among copies of the requested model (several accounts) the draw stays
    // within those copies; otherwise among the top-k.
    const primary = this.primaryRefs.has(choice.entry.ref);
    const pool = primary ? ranked.filter((item) => this.primaryRefs.has(item.entry.ref)) : ranked.slice(0, getConfig().routing.topK);
    if (pool.length > 1) {
      const temperature = Math.max(0.01, this.profile.constraints.exploration ?? 0.08);
      const top = pool[0].score;
      const weights = pool.map((item) => Math.exp((item.score - top) / temperature));
      const total = weights.reduce((sum, value) => sum + value, 0);
      let draw = this.random() * total;
      for (let index = 0; index < pool.length; index += 1) {
        draw -= weights[index];
        if (draw <= 0) {
          choice = pool[index];
          break;
        }
      }
    }
    this.tried.add(choice.entry.ref);
    return choice;
  }

  rejectionSummary(now = Date.now()) {
    const counts = {};
    for (const item of this.rank({ includeRejected: true, sample: false, now })) {
      if (item.rejected) counts[item.rejected] = (counts[item.rejected] || 0) + 1;
    }
    return counts;
  }
}

function preview({ userId, key, requestedModel = 'auto', request, needs, inputTokens, limit = 20 }) {
  const plan = new RoutingPlan({ userId, key, request, requestedModel, needs, inputTokens });
  const ranked = plan.rank({ includeRejected: true, sample: false });
  return {
    profile: plan.profile,
    weights: plan.weights,
    complexity: plan.complexity,
    candidates: ranked.slice(0, limit * 3).map((item) => ({
      id: item.entry.id,
      ref: item.entry.ref,
      name: item.entry.name,
      isFree: item.entry.isFree,
      rejected: item.rejected || null,
      score: item.score ?? null,
      features: item.details?.features || null,
      factors: item.details?.factors || null,
    })).sort((a, b) => (a.rejected ? 1 : 0) - (b.rejected ? 1 : 0)).slice(0, limit),
  };
}

module.exports = { RoutingPlan, preview, parseModel, allowedByKey, globToRegExp, rejection, maxOutputFor };
