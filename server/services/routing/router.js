'use strict';

const catalog = require('../catalog/catalog_service');
const { canonicalId } = require('../catalog/model_identity');
const loadTracker = require('./load_tracker');
const concurrency = require('./concurrency');
const quota = require('../quota/quota_service');
const stats = require('./stats_store');
const scoring = require('./scoring');
const accountHealth = require('./account_health');
const profiles = require('./profiles');
const budget = require('../budget/budget_service');
const pacing = require('../budget/pacing');
const plans = require('./plans');
const priority = require('./priority');
const sharePolicy = require('../pools/share_policy');
const { isPeerEntry } = require('../pools/links');
const { classify } = require('./task/classifier');
const { neededIntelligence } = require('./task/skills');
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
const { FEATURE_FLOOR } = scoring;
// Speed and price can outweigh skill, but only so far: a model that is truly
// out of its depth counts for less. Kept low on purpose, so small fast models
// that are good enough stay in the running on a style that wants speed.
const WEAK_FOR_TASK_FACTOR = 0.6;
const weakBelow = (need) => Math.max(0.08, Math.min(0.12, (need ?? 0) * 0.3));
// A pool partner's copy of a model counts for a little less than an own
// connection, so partners' capacity is used when it helps, not by habit.
const PEER_FACTOR = 0.8;

function globToRegExp(pattern) {
  const escaped = String(pattern).replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.');
  return new RegExp(`^${escaped}$`, 'i');
}

// A key's allowed list holds model patterns ("groq/*") and routing entries
// ("auto", "auto:coding", "auto-free:*"). Model patterns bound which models
// the key reaches, routing included; routing entries bound which autos it
// may ask for.
const isRoutePattern = (pattern) => /^auto(?:$|[-:*?])/i.test(String(pattern).trim());

// A key's allowed list holds model patterns ("groq/*"), routings ("auto:fast")
// and kinds of model ("kind:speech").
const KIND_TOKEN = /^kind:([a-z_]+)$/i;

function keyPatterns(key) {
  const patterns = JSON.parse(key.allowed_models_json || '[]').map((pattern) => String(pattern).trim());
  const kinds = patterns.map((pattern) => KIND_TOKEN.exec(pattern)?.[1]?.toLowerCase()).filter(Boolean);
  const rest = patterns.filter((pattern) => !KIND_TOKEN.test(pattern));
  return { kinds, models: rest.filter((pattern) => !isRoutePattern(pattern)), routes: rest.filter(isRoutePattern) };
}

// Whether the model patterns name any model of this kind in the user's
// catalog, cached briefly (it is asked once per candidate).
const coverCache = new Map();
function patternsCoverKind(key, capability, models) {
  const id = `${key.user_id}|${capability}|${models.join('\n')}`;
  const hit = coverCache.get(id);
  if (hit && Date.now() - hit.at < 10_000) return hit.value;
  const regexes = models.map(globToRegExp);
  const value = catalog.listForUser(key.user_id, { capability, includeUnavailable: true }).some((entry) => regexes.some((regex) => regex.test(entry.id)));
  if (coverCache.size > 500) coverCache.clear();
  coverCache.set(id, { at: Date.now(), value });
  return value;
}

// With kinds listed, the key takes only those kinds, and model patterns
// narrow just the kinds they name a model of ("these chat models, and any
// speech model"). Without kinds, model patterns limit everything, as before.
function allowedByKey(entry, key) {
  const { kinds, models } = keyPatterns(key);
  const capability = entry.capability || 'chat';
  if (kinds.length && !kinds.includes(capability)) return false;
  if (!models.length) return true;
  if (kinds.length && !patternsCoverKind(key, capability, models)) return true;
  return models.some((pattern) => globToRegExp(pattern).test(entry.id));
}

// Whether the key may ask for this model name. With routing entries listed,
// an auto must match one of them (plain "auto" also matches as
// "auto:<key profile>"), and a model named directly needs model patterns too.
function routeAllowedByKey(requested, key) {
  const { models, routes } = keyPatterns(key);
  if (!routes.length) return true;
  const parsed = parseModel(requested);
  if (!parsed.auto) return models.length > 0;
  const base = parsed.kind ? `auto-${parsed.kind}` : 'auto';
  const named = parsed.profile === 'default' ? null : parsed.profile;
  const names = named ? [`${base}:${profiles.resolve(key.user_id, named)?.slug || named}`] : [base];
  if (!named) names.push(`${base}:${profiles.resolve(key.user_id, key.profile || 'balanced')?.slug || 'balanced'}`);
  return names.some((name) => routes.some((pattern) => globToRegExp(pattern).test(name)));
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

// How many failures one connection may cost a single request. A provider with
// many models must not use up every attempt before others get a turn.
const MAX_FAILURES_PER_ACCOUNT = 2;
// Tier offsets for a pool partner's request: free first, then subscription, then paid.
const PARTNER_KIND_TIER = Object.freeze({ free: 0, plan: 100, paid: 200 });
// Failures that say the connection is struggling, not that this request is odd.
const CONNECTION_FAILURES = new Set(['rate_limit', 'quota_exhausted', 'payment', 'auth', 'not_found', 'server', 'network', 'timeout']);

// Hard filters. Returns a reason string when the entry cannot serve.
function rejection(entry, ctx, now) {
  const { key, needs, inputTokens, request, constraints, freeOnly } = ctx;
  if (!entry.enabled) return 'model_disabled';
  if (!entry.available) return 'model_unavailable';
  if (!entry.accountEnabled || entry.accountStatus === 'disabled' || entry.accountStatus === 'invalid') return 'account_unavailable';
  if (!allowedByKey(entry, key)) return 'not_allowed_for_key';
  if ((ctx.accountFailures?.get(entry.accountId) || 0) >= MAX_FAILURES_PER_ACCOUNT && !ctx.primaryRefs.has(entry.ref)) return 'account_failing';
  if ((!ctx.longShot || !longShotDue(entry.ref, now)) && accountHealth.restingUntil(entry.accountId, now) && !ctx.primaryRefs.has(entry.ref)) return 'account_resting';
  // A partner's key has no cost choice of its own: what the owner shares decides (below).
  const kind = key.peer_link_id ? null : kindAllowed(entry, key, freeOnly);
  if (kind) return kind;
  // Pools: a partner's request only reaches what is shared with them, and is
  // never passed on; an own request reaches partners only with a pool key,
  // and not when it holds very sensitive data.
  if (key.peer_link_id) {
    const shared = sharePolicy.rejection(entry, { inputTokens, reserve: ctx.shareReserve ?? undefined, poolId: ctx.sharePoolId }, now);
    if (shared) return shared;
  } else if (isPeerEntry(entry)) {
    if (key.pool_access !== 'pool') return 'not_in_pool';
    if (ctx.keepPrivate) return 'kept_private';
  }
  if (ctx.tokenLimitReached && (entry.isFree || entry.isPlan)) return 'token_limit';
  // A connection on "Free" has no credits: its models that are not free fail.
  if (needsCredits(entry)) return 'needs_credits';
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
  if (key.max_price_per_million !== null && key.max_price_per_million !== undefined && !entry.isFree && !entry.isPlan && (entry.priceOut === null || entry.priceOut > key.max_price_per_million)) return 'key_too_expensive';
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
  if (!available.available && !(ctx.longShot && LONG_SHOT_REASONS.has(available.reason) && longShotDue(entry.ref, now))) return available.reason;
  const planReason = plans.rejection(entry, now);
  if (planReason) return planReason;
  // An account at its (learned or documented) concurrency limit takes no more work right now.
  const parallel = concurrency.limitFor(entry, now);
  if (parallel && loadTracker.inflight(entry).account >= parallel.limit) return 'account_busy';
  const quotaState = quota.evaluate(entry, inputTokens, now);
  if (quotaState.blocked) return `quota_${quotaState.blocked.metric}`;
  if (priority.heldBack(entry, key.priority)) return 'held_for_priority';
  return null;
}

// The same model on several accounts (two Groq keys, say): prefer the copy
// with more allowance left and more free capacity, so accounts wear evenly
// and a busy one hands work to its siblings. Only copies of one model at the
// same kind and price are compared (a paid copy of a subscription model, or
// its pricier "fast" variant, is not a sibling), so this never changes which
// model is chosen.
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
    const copy = `${item.entry.canonicalId}|${modelKind(item.entry)}|${scoring.blendedPrice(item.entry)}`;
    if (!groups.has(copy)) groups.set(copy, []);
    groups.get(copy).push(item);
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

function needsCredits(entry) {
  return entry.billingMode === 'free_tier' && !entry.isFree && !entry.isPlan && NEEDS_CREDITS.has(entry.freeReason);
}

// Why no model could take a request, in words: groups of rejection reasons
// with their counts, largest first.
const REJECTION_GROUPS = [
  ['tokens', ['token_limit'], "are past this key's token limit until it resets"],
  ['credits', ['needs_credits'], 'need credits, and their connection is set to free'],
  ['action', ['needs_action'], 'need something only you can do at the provider (accepting terms, a consent, a verification or a plan)'],
  ['payment', ['payment_required', 'no_free_quota'], 'were refused by the provider for payment or plan reasons'],
  ['auth', ['auth_failed'], 'have a key the provider rejects'],
  ['missing', ['not_available'], 'are listed but not served by the provider right now'],
  ['busy', ['scheduled_downtime', 'account_failing', 'account_resting', 'quota_exhausted', 'quota_rpd', 'quota_rpm', 'quota_tpm', 'quota_tpd', 'rate_limited', 'circuit_open', 'account_busy', 'held_for_priority', 'plan_used_up', 'kept_for_owner'], 'are used up, rate-limited or cooling down for now'],
  ['fit', ['no_tools', 'no_vision', 'context_too_small', 'context_unknown', 'needs_more_tokens', 'incompatible'], 'cannot handle this request (tools, images or its size)'],
  ['unsupported', ['unsupported'], 'cannot be used for this through NeoRouter yet (the provider speaks a different API for it)'],
  ['profile', ['below_min_intelligence', 'too_expensive', 'too_slow', 'provider_not_included', 'provider_excluded', 'model_excluded', 'kind_not_in_style'], "are left out by the routing style"],
  ['budget', ['price_unknown', 'budget_exhausted', 'key_too_expensive'], "have no known price or would go past this key's budget or price limit"],
  ['key', ['not_free', 'not_allowed_for_key', 'other_kind', 'not_in_pool', 'not_shared', 'peer_no_forward'], 'are not allowed for this key'],
  ['private', ['kept_private'], 'are pool partners, and this request holds very sensitive data that stays on your own connections'],
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

// Cooldowns that end by themselves; whether waiting helps depends on when.
const SHORT_COOLDOWNS = new Set(['rate_limited', 'quota_exhausted', 'circuit_open', 'cooldown']);
const BUSY_RECHECK_MS = 1000;
// When nothing else can serve, connections held back only by what the router
// believes about their health are tried anyway: a rate limit or outage may be
// over, a model that went missing may be back. Not the ones a person must fix
// (payment, key, a model that rejects this kind of request).
const LONG_SHOT_REASONS = new Set(['rate_limited', 'quota_exhausted', 'not_available', 'circuit_open', 'cooldown', 'account_resting']);
const MAX_LONG_SHOTS = 3;
// Each held-back model is a long shot at most this often, across all
// requests: a busy server must not send every request to models it already
// knows are refusing (once, that was most of all attempts).
const LONG_SHOT_GAP_MS = 10 * 60_000;
const lastLongShot = new Map();
const longShotDue = (ref, now = Date.now()) => now - (lastLongShot.get(ref) || 0) >= LONG_SHOT_GAP_MS;
function resetLongShotsForTests() {
  lastLongShot.clear();
}
const MINUTE_MS = 60_000;

class RoutingPlan {
  // taskHint: a task kind the client named (X-NeoRouter-Task).
  // keepPrivate: very sensitive content, kept off pool partners.
  // shareReserve: for a partner's request, the share of capacity kept for the owner.
  constructor({ userId, key, request, requestedModel, needs, inputTokens, freeOnly = false, random = Math.random, capability = 'chat', estimateCost = null, taskHint = null, keepPrivate = false, shareReserve = null, sharePoolId = null }) {
    this.capability = capability;
    this.keepPrivate = keepPrivate;
    this.shareReserve = shareReserve;
    this.sharePoolId = sharePoolId;
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
    if (!routeAllowedByKey(requestedModel, key)) {
      throw new HttpError(403, 'MODEL_NOT_ALLOWED', `This key may not use "${String(requestedModel || 'auto').trim() || 'auto'}". Ask for one of the models or routings it allows (GET /v1/models).`);
    }
    // What a chat request asks for (kind mix, skills, difficulty); other
    // capabilities have no prompt to read.
    this.task = capability === 'chat' && request?.messages?.length ? classify(request, { inputTokens, hint: taskHint }) : null;
    this.complexity = this.task?.difficulty ?? null;
    const adaptive = this.profile.constraints.adaptive !== false;
    this.weights = scoring.adaptWeights(this.profile.weights, this.complexity, adaptive);
    // Models above what the task needs compete on price, speed and reliability.
    this.need = adaptive ? neededIntelligence(this.complexity) : null;
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
    this.accountFailures = new Map();
    this.longShot = false;
    this.longShotsLeft = MAX_LONG_SHOTS;
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
    return { keepPrivate: this.keepPrivate, shareReserve: this.shareReserve, sharePoolId: this.sharePoolId, longShot: this.longShot, accountFailures: this.accountFailures, tokenLimitReached: this.tokenLimitReached, onlyKind: this.parsed.kind, key: this.key, needs: this.needs, inputTokens: this.inputTokens, request: this.request, constraints: this.profile.constraints, freeOnly: this.freeOnly, primaryRefs: this.primaryRefs, estimateCost: this.estimateCost };
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
      const result = scoring.score(entry, { weights: this.weights, quotaFactor: quotaState.factor, random: this.random, sample, now, task: this.task, need: this.need });
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
      if (isPeerEntry(item.entry)) value *= PEER_FACTOR;
      // Far too weak for this task: taken only when nothing better can serve.
      if (this.task && item.details.capability < weakBelow(this.need)) value *= WEAK_FOR_TASK_FACTOR;
      // A custom style's preference between free, subscription and paid models.
      item.tier = this.profile.constraints.kinds ? kindTier(this.profile.constraints.kinds, item.entry) : 0;
      // A partner's request uses shared subscription models only when no free one can serve, and paid ones last.
      if (this.key.peer_link_id) item.tier += PARTNER_KIND_TIER[modelKind(item.entry)];
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
    spreadAcrossAccounts(scored);
    preferCheaperEquals(scored, kinds ? (entry) => kindWeight(kinds, entry) : undefined);
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
    if (this.longShot && this.heldBack(choice.entry, now)) {
      choice.entry.tryingLuck = true;
      if (lastLongShot.size > 5000) lastLongShot.clear();
      lastLongShot.set(choice.entry.ref, now);
      this.longShotsLeft -= 1;
      if (this.longShotsLeft <= 0) this.longShot = false;
    }
    return choice;
  }

  // Held back only by its health record (not by a limit, a policy or a request it cannot serve).
  heldBack(entry, now = Date.now()) {
    return LONG_SHOT_REASONS.has(rejection(entry, { ...this.context(), longShot: false }, now));
  }

  // Nothing else can serve: allow a few tries of connections held back only
  // by their health record. False when there is none (or they were used up).
  enableLongShots({ filter = null, now = Date.now() } = {}) {
    if (this.longShot || this.longShotsLeft <= 0) return false;
    const ctx = { ...this.context(), longShot: true };
    const possible = this.entries.some((entry) => !this.tried.has(entry.ref) && (!filter || filter(entry))
      && this.heldBack(entry, now) && longShotDue(entry.ref, now) && !rejection(entry, ctx, now));
    this.longShot = possible;
    return possible;
  }

  // When the soonest candidate that is only blocked for now (rate-limited,
  // cooling down, at its concurrency or per-minute limit) may take work
  // again, or null when none will without outside help (a daily limit, a
  // key policy, a model that cannot handle the request).
  freesUpAt(now = Date.now(), filter = null) {
    const ctx = this.context();
    let soonest = null;
    for (const entry of this.entries) {
      if (this.tried.has(entry.ref) || (filter && !filter(entry))) continue;
      const reason = rejection(entry, ctx, now);
      let at = null;
      if (reason === 'account_busy' || reason === 'held_for_priority') at = now + BUSY_RECHECK_MS;
      else if (SHORT_COOLDOWNS.has(reason)) at = stats.availability(entry.ref, now).until || now + BUSY_RECHECK_MS;
      else if (reason && reason.startsWith('quota_')) {
        const blocked = quota.evaluate(entry, this.inputTokens, now).blocked;
        at = blocked?.resetAt || (/^quota_(rpm|tpm)$/.test(reason) ? now + MINUTE_MS : null);
      }
      if (at !== null && (soonest === null || at < soonest)) soonest = at;
    }
    return soonest;
  }

  // While this request waits for capacity, requests of a lower priority leave
  // what it waits for alone. Called on every pass, as candidates free up or
  // block; leaveQueue() ends it.
  joinQueue(now = Date.now(), filter = null) {
    const ctx = this.context();
    const targets = new Set();
    for (const entry of this.entries) {
      if (this.tried.has(entry.ref) || (filter && !filter(entry))) continue;
      const reason = rejection(entry, ctx, now);
      if (reason === 'account_busy') targets.add(`account:${entry.accountId}`);
      else if (reason === 'held_for_priority' || SHORT_COOLDOWNS.has(reason) || /^quota_(rpm|tpm)$/.test(reason || '')) targets.add(`model:${entry.ref}`);
    }
    this.leaveQueue();
    this.stopWaiting = priority.join([...targets], this.key.priority);
  }

  // A failed attempt, so this request moves on from a connection that keeps failing.
  noteFailure(entry, kind) {
    if (CONNECTION_FAILURES.has(kind)) this.accountFailures.set(entry.accountId, (this.accountFailures.get(entry.accountId) || 0) + 1);
  }

  leaveQueue() {
    this.stopWaiting?.();
    this.stopWaiting = null;
  }

  // filter: what the caller can actually use (for media, adapters that speak
  // the kind); the rest are counted as "unsupported", so none go unexplained.
  rejectionSummary(now = Date.now(), filter = null) {
    const counts = {};
    for (const item of this.rank({ includeRejected: true, sample: false, now })) {
      const reason = item.rejected || (filter && !filter(item.entry) ? 'unsupported' : null);
      if (reason) counts[reason] = (counts[reason] || 0) + 1;
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
    need: plan.need,
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

module.exports = { LONG_SHOT_REASONS, MAX_LONG_SHOTS, LONG_SHOT_GAP_MS, resetLongShotsForTests, needsCredits, RoutingPlan, explainNoModel, preview, parseModel, isAutoModel, modelKind, MODEL_KINDS, allowedByKey, routeAllowedByKey, kindAllowed, globToRegExp, rejection, maxOutputFor };
