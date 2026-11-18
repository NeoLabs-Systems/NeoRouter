'use strict';

// Stored alongside billing_mode='paid': every one is a paid subscription,
// even when its incremental request price is zero.
const PREPAID = Object.freeze({ plan: 1, plan_paid: 2, flat_rate: 3 });
const FLAT_RATE = PREPAID.flat_rate;
function storedBilling(mode) {
  return PREPAID[mode] ? { billing: 'paid', prepaid: PREPAID[mode] } : { billing: mode, prepaid: 0 };
}
function publicBilling(row) {
  return Object.keys(PREPAID).find((mode) => PREPAID[mode] === row.prepaid) || row.billing_mode;
}
module.exports = { FLAT_RATE, storedBilling, publicBilling };
