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
const pacing = require('../budget/pacing');
const plans = require('./plans');
const { classify } = require('./task/classifier');
const { getConfig } = require('../../config/env');
const { HttpError } = require('../../middleware/error_handler');

const AUTO = 'auto';
const DEFAULT_OUTPUT_TOKENS = 1024;
const MIN_OUTPUT_RESERVE = 256;
const UNKNOWN_CAPABILITY_FACTOR = 0.85;
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

// The kinds of model an auto variant can be limited to.
const MODEL_KINDS = Object.freeze(['free', 'plan', 'paid']);
const AUTO_PATTERN = /^auto(?:-(free|plan|paid))?(?::(.+))?$/;

// "auto" and "auto:<profile>" route over every kind the key allows;
// "auto-free", "auto-plan:fast" or "auto-paid:best" only over that kind.
function parseModel(requested) {
  const value = String(requested || '').trim();
  if (!value) return { auto: true, profile: null, kind: null };
  const match = AUTO_PATTERN.exec(value);
  if (match) return { auto: true, kind: match[1] || null, profile: match[2] || null };
  return { auto: false, model: value };
}

const isAutoModel = (value) => parseModel(value).auto;

function modelKind(entry) {
  return entry.isFree ? 'free' : entry.isPlan ? 'plan' : 'paid';
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
  const kind = kindAllowed(entry, key, freeOnly);
  if (kind) return kind;
  if (ctx.tokenLimitReached && (entry.isFree || entry.isPlan)) return 'token_limit';
  // A connection on "Free" has no credits: its models that are not free fail.
  if (entry.billingMode === 'free_tier' && !entry.isFree && !entry.isPlan && NEEDS_CREDITS.has(entry.freeReason)) return 'needs_credits';
  if (ctx.onlyKind && modelKind(entry) !== ctx.onlyKind) return 'other_kind';
  if (constraints.kinds && !(constraints.kinds[modelKind(entry)] > 0)) return 'kind_not_in_style';
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
  if (constraints.maxPriceOut !== null && constraints.maxPriceOut !== undefined && !entry.isFree && !entry.isPlan && (entry.priceOut === null || entry.priceOut > constraints.maxPriceOut)) return 'too_expensive';
  if (constraints.maxTtftMs && entry.stats.ttftMs && entry.stats.ttftMs > constraints.maxTtftMs) return 'too_slow';
  if (!entry.isFree && !entry.isPlan) {
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
  const planReason = plans.rejection(entry, now);
  if (planReason) return planReason;
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
// Budget pacing (services/budget/pacing.js).
const PACED_COST_FLOOR = 0.1;
const OVER_PACE_FACTOR = 0.15;
const TYPICAL_OUTPUT_TOKENS = 1500;
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

/**
 * Free, plan and paid models are three switches on a key. freeOnly (a
 * used-up budget, the playground toggle) shuts paid models out on top.
 * Returns the rejection reason or null.
 */
function kindAllowed(entry, key, freeOnly = false) {
  if (entry.isFree) return key.allow_free === 0 ? 'not_allowed_for_key' : null;
  if (entry.isPlan) return key.allow_plan === 0 ? 'not_allowed_for_key' : null;
  return freeOnly || key.mode === 'free_only' ? 'not_free' : null;
}

// Among the leading candidates, a model that is clearly more expensive than
// another of about the same quality (intelligence, speed and reliability as
// the profile weighs them) drops behind it: similar quality goes to the
// cheaper model, whatever the profile.
const CHEAPER_CANDIDATES = 40;
const SIMILAR_QUALITY = 0.03;
const CLEARLY_CHEAPER = 0.05;
const PRICIER_EQUAL_FACTOR = 0.6;

// Relative to the kind the style likes most, so the favourite keeps its score.
function kindWeight(kinds, entry) {
  const top = Math.max(kinds.free, kinds.plan, kinds.paid) || 1;
  return (kinds[modelKind(entry)] || 0) / top;
}

// A custom style's kinds as tiers: the kinds it weighs most ("Prefer") are
// tier 0, a lower weight ("If needed") a later tier. A later tier is only
// drawn from once no model of an earlier one can take the request (none fits,
// or all were tried), however much better it scores.
function kindTier(kinds, entry) {
  const levels = [...new Set([kinds.free, kinds.plan, kinds.paid].filter((weight) => weight > 0))].sort((a, b) => b - a);
  return Math.max(0, levels.indexOf(kinds[modelKind(entry)]));
}

// A custom style that weighs a kind of model lower does not count it as the
// cheaper equal (`weightOf`).
function preferCheaperEquals(scored, weightOf = () => 1) {
  const leading = [...scored].sort((a, b) => b.score - a.score).slice(0, CHEAPER_CANDIDATES);
  const priced = leading.map((item) => ({ item, cost: item.ownCost, quality: item.quality, weight: weightOf(item.entry) }));
  for (const candidate of priced) {
    const cheaperEqual = priced.some((other) => other.cost >= candidate.cost + CLEARLY_CHEAPER && other.quality >= candidate.quality * (1 - SIMILAR_QUALITY)
      && other.weight >= candidate.weight);
    if (cheaperEqual) {
      candidate.item.score *= PRICIER_EQUAL_FACTOR;
      // Kept out of the random draw while a cheaper equal can answer.
      candidate.item.pricierEqual = true;
    }
  }
}

// Free-classifier reasons that mean the model is simply not free (as opposed
// to an unverified free tier, which may still work).
const NEEDS_CREDITS = new Set(['not_in_free_tier', 'paid', 'price_unknown']);

// Why no model could take a request, in words: groups of rejection reasons
// with their counts, largest first.
const REJECTION_GROUPS = [
  ['key', ['not_free', 'not_allowed_for_key', 'other_kind', 'price_unknown', 'budget_exhausted'], 'are not allowed for this key'],
  ['tokens', ['token_limit'], "are past this key's token limit until it resets"],
  ['credits', ['needs_credits'], 'need credits, and their connection is set to free'],
  ['busy', ['quota_exhausted', 'quota_rpd', 'quota_rpm', 'quota_tpm', 'quota_tpd', 'rate_limited', 'circuit_open', 'account_busy', 'plan_used_up', 'not_available'], 'are used up, rate-limited or cooling down for now'],
  ['fit', ['no_tools', 'no_vision', 'context_too_small', 'context_unknown', 'needs_more_tokens', 'incompatible'], 'cannot handle this request (tools, images or its size)'],
  ['profile', ['below_min_intelligence', 'too_expensive', 'too_slow', 'provider_not_included', 'provider_excluded', 'model_excluded', 'kind_not_in_style'], "are left out by the routing style"],
];

function explainNoModel(rejected, capability = 'chat') {
  const counts = new Map();
  for (const [reason, count] of Object.entries(rejected || {})) {
    const group = REJECTION_GROUPS.find(([, reasons]) => reasons.includes(reason) || (reason.startsWith('quota') && reasons.includes('quota_exhausted')));
    const label = group ? group[2] : 'are switched off or unavailable';
    counts.set(label, (counts.get(label) || 0) + count);
  }
  const kind = capability === 'chat' ? '' : `${capability} `;
  if (!counts.size) return `There is no ${kind}model in your catalog yet.`;
  const parts = [...counts].sort((a, b) => b[1] - a[1]).map(([label, count]) => `${count} ${label}`);
  return `No ${kind}model can take this request right now: ${parts.join('; ')}.`;
}

class RoutingPlan {
  // taskHint: a task kind the client named (X-NeoRouter-Task).
  constructor({ userId, key, request, requestedModel, needs, inputTokens, freeOnly = false, random = Math.random, capability = 'chat', estimateCost = null, taskHint = null }) {
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
    // "auto:default" is plain auto for apps that cannot send a bare "auto".
    const named = this.parsed.profile === 'default' ? null : this.parsed.profile;
    const profileRef = named || key.profile || 'balanced';
    this.profile = profiles.resolve(userId, profileRef);
    if (!this.profile) throw new HttpError(400, 'UNKNOWN_PROFILE', `Unknown routing profile "${profileRef}".`);
    // What a chat request asks for (kind mix, skills, difficulty); other
    // capabilities have no prompt to read.
    this.task = capability === 'chat' && request?.messages?.length ? classify(request, { inputTokens, hint: taskHint }) : null;
    this.complexity = this.task?.difficulty ?? null;
    this.weights = scoring.adaptWeights(this.profile.weights, this.complexity, this.profile.constraints.adaptive !== false);
    // A budget that would run out before the period ends shifts weight to
    // cost; one with plenty left lets quality and speed count for more.
    this.pacing = freeOnly ? null : pacing.pacing(key);
    if (this.pacing && this.pacing.costShift !== 1) {
      this.weights = { ...this.weights, cost: Math.max(this.weights.cost, PACED_COST_FLOOR) * this.pacing.costShift };
    }
    this.tokenLimitReached = budget.tokenLimitReached(key);
    this.entries = catalog.listForUser(userId, { capability }).map((entry) => plans.effective(entry));
    // Outcomes are learned per kind of task (learning.js).
    if (this.task) for (const entry of this.entries) entry.task = this.task.primary;
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
    return { tokenLimitReached: this.tokenLimitReached, onlyKind: this.parsed.kind, key: this.key, needs: this.needs, inputTokens: this.inputTokens, request: this.request, constraints: this.profile.constraints, freeOnly: this.freeOnly, primaryRefs: this.primaryRefs, estimateCost: this.estimateCost };
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
      const result = scoring.score(entry, { weights: this.weights, quotaFactor: quotaState.factor, random: this.random, sample, now, task: this.task });
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
    const qualityWeight = (totalWeight - (this.weights.cost || 0)) || 1;
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
      // What the model is worth apart from its price, from its own (not
      // field-relative) values, so small differences stay small.
      // Reliability as its average, not this request's random sample.
      const own = { ...item.details.features, reliability: scoring.reliability(stats.snapshot(item.entry.ref, now), { sample: false }) };
      item.quality = Math.exp(Object.entries(this.weights).filter(([feature]) => feature !== 'cost')
        .reduce((sum, [feature, weight]) => sum + (weight / qualityWeight) * Math.log(Math.max(own[feature], FEATURE_FLOOR)), 0));
      item.ownCost = own.cost;
      const { quota: quotaFactor, penalty, load } = item.details.factors;
      let value = base * quotaFactor * penalty * load;
      if (qualityProfile && features.intelligence < bestIntelligence - QUALITY_GAP) value *= QUALITY_GATE_FACTOR;
      value *= plans.factor(item.entry);
      // A custom style's preference between free, subscription and paid models.
      item.tier = this.profile.constraints.kinds ? kindTier(this.profile.constraints.kinds, item.entry) : 0;
      if ((this.needs.tools && item.entry.tools === null) || (this.needs.json && item.entry.json === false)) value *= UNKNOWN_CAPABILITY_FACTOR;
      // Far more than this request's fair share of the budget: last resort only.
      if (this.pacing && !item.entry.isFree && !item.entry.isPlan) {
        const typical = this.estimateCost ? this.estimateCost(item.entry) : budget.estimateMaxCost(item.entry, this.inputTokens, Math.min(maxOutputFor(this.request, item.entry), TYPICAL_OUTPUT_TOKENS));
        if (typical !== null && typical > this.pacing.perRequestCap) value *= OVER_PACE_FACTOR;
      }
      item.score = value;
      item.details = { ...item.details, base, features };
    }
    const kinds = this.profile.constraints.kinds;
    preferCheaperEquals(scored, kinds ? (entry) => kindWeight(kinds, entry) : undefined);
    spreadAcrossAccounts(scored);
    // The model (or account) the client asked for comes first.
    for (const item of scored) if (this.primaryRefs.has(item.entry.ref)) item.score += 10;
    ranked.sort((a, b) => (b.score ?? -1) - (a.score ?? -1));
    return ranked;
  }

  // Softmax draw among the top-k so load spreads across good candidates
  // instead of hammering the single best one.
  next({ filter = null, now = Date.now() } = {}) {
    const candidates = this.rank({ now }).filter((item) => !filter || filter(item.entry));
    if (!candidates.length) return null;
    // Only the style's first tier of model kinds that still has a candidate.
    const tier = Math.min(...candidates.map((item) => item.tier ?? 0));
    const ranked = candidates.filter((item) => (item.tier ?? 0) === tier);
    let choice = ranked[0];
    // Among copies of the requested model (several accounts) the draw stays
    // within those copies; otherwise among the top-k.
    const primary = this.primaryRefs.has(choice.entry.ref);
    const leading = primary ? ranked.filter((item) => this.primaryRefs.has(item.entry.ref)) : ranked.slice(0, getConfig().routing.topK);
    const cheaperFirst = leading.filter((item) => !item.pricierEqual);
    const pool = cheaperFirst.length ? cheaperFirst : leading;
    choice = pool[0];
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
  // In the order the router tries them: a style's preferred kinds first.
  const ranked = plan.rank({ includeRejected: true, sample: false })
    .sort((a, b) => (a.rejected ? 1 : 0) - (b.rejected ? 1 : 0) || (a.tier ?? 0) - (b.tier ?? 0) || (b.score ?? -1) - (a.score ?? -1));
  return {
    profile: plan.profile,
    weights: plan.weights,
    complexity: plan.complexity,
    task: plan.task,
    candidates: ranked.slice(0, limit * 3).map((item) => ({
      id: item.entry.id,
      ref: item.entry.ref,
      name: item.entry.name,
      isFree: item.entry.isFree,
      isPlan: item.entry.isPlan,
      rejected: item.rejected || null,
      score: item.score ?? null,
      features: item.details?.features || null,
      factors: item.details?.factors || null,
      // 0 = a kind the style prefers; higher = only if those cannot answer.
      tier: item.tier ?? 0,
    })).slice(0, limit),
  };
}

module.exports = { RoutingPlan, explainNoModel, preview, parseModel, isAutoModel, modelKind, MODEL_KINDS, allowedByKey, kindAllowed, globToRegExp, rejection, maxOutputFor };
