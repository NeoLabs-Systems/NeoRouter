'use strict';

// Strict "100% free" classification. A model is free only when one of these
// is true; an unknown price is never treated as free:
//   1. the user explicitly marked it free (manual override)
//   2. the provider definition lists it as always free (e.g. ":free" models)
//   3. the provider's own listing reports every price field as exactly 0
//   4. the account is on the provider's free tier (billing_mode=free_tier),
//      the model is covered by that free tier, and, where the provider can
//      bill a card, an automatic check verified the account is not billed
// Evidence from real traffic overrides 2–4: a model that reported a cost or
// a free-tier limit of 0 is never free again until the user says otherwise.
const REASONS = Object.freeze({
  MANUAL_FREE: 'manual_free',
  MANUAL_PAID: 'manual_paid',
  BILLED_OBSERVED: 'billed_observed',
  NO_FREE_TIER: 'no_free_tier',
  PROVIDER_FREE_MODEL: 'provider_free_model',
  LISTED_ZERO_PRICE: 'listed_zero_price',
  FREE_TIER_ACCOUNT: 'free_tier_account',
  BILLING_ACTIVE: 'billing_active',
  BILLING_UNVERIFIED: 'billing_unverified',
  NOT_IN_FREE_TIER: 'not_in_free_tier',
  PAID: 'paid',
  PRICE_UNKNOWN: 'price_unknown',
});

function matchesAny(patterns, modelId) {
  return (patterns || []).some((pattern) => new RegExp(pattern, 'i').test(modelId));
}

function coveredByFreeTier(billing, modelId) {
  return billing.freeTierAll === true || matchesAny(billing.freeTierModels, modelId);
}

function hasFreeTier(billing) {
  return billing.freeTierAll === true || Boolean(billing.freeTierModels?.length);
}

// Whether the provider is known to have free models: a free tier, or models
// that are always free (":free"). An account can also turn out to have free
// models from its listing (zero prices), see account_service.offersFree.
function declaresFreeModels(billing) {
  return hasFreeTier(billing) || Boolean(billing.alwaysFree?.length);
}

// Loopback and private-network hosts (RFC 1918, link-local, .local).
function isPrivateUrl(url) {
  let host;
  try { host = new URL(url).hostname.replace(/^\[|\]$/g, ''); } catch { return false; }
  return host === 'localhost' || host.endsWith('.local') || host.endsWith('.localhost') || host === '::1'
    || /^127\./.test(host) || /^10\./.test(host) || /^192\.168\./.test(host) || /^172\.(1[6-9]|2\d|3[01])\./.test(host)
    || /^169\.254\./.test(host) || /^f[cd][0-9a-f]{2}:/i.test(host);
}

function classify({ definition, billingMode, billingState = 'unknown', modelId, listing = {}, overrides = {}, baseUrl = null, hasSecret = false }) {
  const billing = definition.billing;
  if (overrides.free === true) return { isFree: true, reason: REASONS.MANUAL_FREE };
  if (overrides.free === false) return { isFree: false, reason: REASONS.MANUAL_PAID };
  if (overrides.billedAt) return { isFree: false, reason: REASONS.BILLED_OBSERVED };
  if (matchesAny(billing.alwaysFree, modelId)) return { isFree: true, reason: REASONS.PROVIDER_FREE_MODEL };
  if (listing.freeHint === true && billing.trustListingFree) return { isFree: true, reason: REASONS.LISTED_ZERO_PRICE };
  if (billingMode === 'free_tier' && hasFreeTier(billing)) {
    if (overrides.noFreeTier) return { isFree: false, reason: REASONS.NO_FREE_TIER };
    if (billing.localOnly && !isPrivateUrl(baseUrl || definition.baseUrl)) return { isFree: false, reason: REASONS.NOT_IN_FREE_TIER };
    if (billing.keylessOnly && hasSecret) return { isFree: false, reason: REASONS.NOT_IN_FREE_TIER };
    if (!coveredByFreeTier(billing, modelId)) return { isFree: false, reason: REASONS.NOT_IN_FREE_TIER };
    if (billingState === 'paid') return { isFree: false, reason: REASONS.BILLING_ACTIVE };
    if (billing.verify && billingState !== 'free') return { isFree: false, reason: REASONS.BILLING_UNVERIFIED };
    return { isFree: true, reason: REASONS.FREE_TIER_ACCOUNT };
  }
  return { isFree: false, reason: listing.priceKnown ? REASONS.PAID : REASONS.PRICE_UNKNOWN };
}

module.exports = { classify, hasFreeTier, declaresFreeModels, isPrivateUrl, REASONS };
