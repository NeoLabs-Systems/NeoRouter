'use strict';

// Keeping requests that may go to a pool partner as private as possible.
// A partner's server sees what it passes on to its provider; this cannot be
// prevented entirely, only reduced:
//   - the privacy shield runs with a minimum for these requests: secrets,
//     cards and wallet data are hidden for good; email addresses, phone
//     numbers, IBANs and ID numbers become placeholders that only this
//     instance can put back into the answer
//   - very sensitive requests (sensitivity.js) stay on the owner's own
//     connections when the owner wants that (on by default)
//   - identifying request fields never leave (definitions/pool_peer.js), and
//     the partner keeps no content and no per-request log (executor)
const settings = require('../settings/settings_service');
const store = require('./store');

const SETTINGS_KEY = 'pool_privacy';
const DEFAULT_SETTINGS = Object.freeze({ keepSensitivePrivate: true });

const ALWAYS_HIDDEN = ['secret', 'card', 'crypto'];
const AT_LEAST_PLACEHOLDER = ['email', 'phone', 'iban', 'id'];

function getSettings(userId) {
  return { ...DEFAULT_SETTINGS, ...(settings.getUserSetting(userId, SETTINGS_KEY, {}) || {}) };
}

function saveSettings(userId, input) {
  const next = { ...getSettings(userId), ...(typeof input.keepSensitivePrivate === 'boolean' ? { keepSensitivePrivate: input.keepSensitivePrivate } : {}) };
  settings.setUserSetting(userId, SETTINGS_KEY, next);
  return next;
}

// The processing settings with the pool minimum applied on top. With the
// shield switched off, only the minimum runs.
function withPoolMinimum(config) {
  const own = config.privacy;
  const modes = Object.fromEntries(Object.entries(own.modes).map(([category, mode]) => [category, own.enabled ? mode : 'off']));
  for (const category of ALWAYS_HIDDEN) if (category in modes) modes[category] = 'hide';
  for (const category of AT_LEAST_PLACEHOLDER) if (category in modes && modes[category] === 'off') modes[category] = 'restore';
  return {
    ...config,
    privacy: { ...own, enabled: true, modes, ai: own.enabled ? own.ai : { enabled: false }, customTerms: own.enabled ? own.customTerms : [], allowTerms: own.enabled ? own.allowTerms : [] },
  };
}

// Whether a request with this key may reach a pool partner at all.
function mayUsePool(key) {
  return key?.pool_access === 'pool' && !key.peer_link_id && Boolean(store.poolOf(key.user_id));
}

module.exports = { DEFAULT_SETTINGS, ALWAYS_HIDDEN, AT_LEAST_PLACEHOLDER, getSettings, saveSettings, withPoolMinimum, mayUsePool };
