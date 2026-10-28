'use strict';

// Strict "100% free" classification. A model is free only when one of these
// is true; an unknown price is never treated as free:
//   1. the user explicitly marked it free (manual override)
//   2. the provider definition lists it as always free (e.g. ":free" models)
//   3. the provider's own listing reports every price field as exactly 0
//   4. the account has no payment method (billing_mode=free_tier) on a
//      provider whose free tier cannot charge anything
const REASONS = Object.freeze({
  MANUAL_FREE: 'manual_free',
  MANUAL_PAID: 'manual_paid',
  PROVIDER_FREE_MODEL: 'provider_free_model',
  LISTED_ZERO_PRICE: 'listed_zero_price',
  FREE_TIER_ACCOUNT: 'free_tier_account',
  PAID: 'paid',
  PRICE_UNKNOWN: 'price_unknown',
});

function matchesAny(patterns, modelId) {
  return patterns.some((pattern) => new RegExp(pattern, 'i').test(modelId));
}

function classify({ definition, billingMode, modelId, listing = {}, overrides = {} }) {
  if (overrides.free === true) return { isFree: true, reason: REASONS.MANUAL_FREE };
  if (overrides.free === false) return { isFree: false, reason: REASONS.MANUAL_PAID };
  if (matchesAny(definition.billing.alwaysFree, modelId)) return { isFree: true, reason: REASONS.PROVIDER_FREE_MODEL };
  if (listing.freeHint === true && definition.billing.trustListingFree) return { isFree: true, reason: REASONS.LISTED_ZERO_PRICE };
  if (billingMode === 'free_tier' && definition.billing.freeTierAll) return { isFree: true, reason: REASONS.FREE_TIER_ACCOUNT };
  return { isFree: false, reason: listing.priceKnown ? REASONS.PAID : REASONS.PRICE_UNKNOWN };
}

module.exports = { classify, REASONS };
