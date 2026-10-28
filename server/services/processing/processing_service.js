'use strict';

// Request processing before routing: compression first (so everything after
// it, including AI detection, sees the smaller request), then the privacy
// shield. Settings are per user; both are off until the user turns them on.
const settings = require('../settings/settings_service');
const compression = require('./compression');
const redaction = require('./redaction');
const aiDetector = require('./ai_detector');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('processing');

const KEY = 'request_processing';

const DEFAULT_SETTINGS = Object.freeze({
  privacy: {
    enabled: false,
    modes: { ...redaction.DEFAULT_MODES },
    customTerms: [],
    ai: { enabled: false, model: 'auto:fast', freeOnly: true },
  },
  compression: { ...compression.DEFAULTS },
});

function merge(stored = {}) {
  const privacy = stored.privacy || {};
  return {
    privacy: {
      ...DEFAULT_SETTINGS.privacy,
      ...privacy,
      modes: { ...DEFAULT_SETTINGS.privacy.modes, ...(privacy.modes || {}) },
      customTerms: Array.isArray(privacy.customTerms) ? privacy.customTerms : [],
      ai: { ...DEFAULT_SETTINGS.privacy.ai, ...(privacy.ai || {}) },
    },
    compression: { ...DEFAULT_SETTINGS.compression, ...(stored.compression || {}), engines: { ...(stored.compression?.engines || {}) } },
  };
}

function getSettings(userId) {
  return merge(settings.getUserSetting(userId, KEY, {}));
}

function saveSettings(userId, next) {
  const merged = merge(next);
  settings.setUserSetting(userId, KEY, merged);
  return merged;
}

// Runs the pipeline. `options.compressionLevel` (from a request header) may
// only lower the configured level.
async function apply(userId, canonical, options = {}) {
  const config = options.settings || getSettings(userId);
  // Compression is an optimisation: if it fails, the request goes out as is.
  // The privacy shield is a promise: if it fails, the request fails.
  let compressed;
  try {
    compressed = compression.compress(canonical, config.compression, { headerLevel: options.compressionLevel || null });
  } catch (error) {
    logger.warn('Compression skipped after an error', { error: error.message });
    const tokens = Math.ceil(JSON.stringify(canonical.messages).length / 4);
    compressed = { canonical, stats: { level: 'off', beforeTokens: tokens, afterTokens: tokens, savedTokens: 0, engines: {} } };
  }
  let result = compressed.canonical;
  let vault = null;
  let aiUsed = false;
  if (config.privacy.enabled) {
    let entitiesFor = () => [];
    if (config.privacy.ai.enabled) {
      entitiesFor = await aiDetector.detectEntities(userId, config.privacy.ai, result);
      aiUsed = true;
    }
    const redacted = redaction.redactRequest(result, { modes: config.privacy.modes, customTerms: config.privacy.customTerms, entitiesFor });
    if (redacted) {
      result = redacted.canonical;
      vault = redacted.vault;
    }
  }
  return {
    canonical: result,
    vault,
    stats: {
      compression: compressed.stats,
      redacted: vault ? vault.size : 0,
      redactedByCategory: vault ? vault.counts : {},
      aiDetection: aiUsed,
    },
  };
}

// The same pipeline for the settings page: shows what would be sent.
async function preview(userId, { text, messages, role = 'user', settings: draft }) {
  const config = draft ? merge(draft) : getSettings(userId);
  // A sample shown as tool output is not "the latest user message", so the
  // lossy compression steps can be previewed on it.
  const canonical = {
    messages: Array.isArray(messages) && messages.length
      ? messages
      : role === 'tool'
        ? [{ role: 'user', content: 'Summarize the tool output.' }, { role: 'tool', content: String(text || '') }]
        : [{ role: 'user', content: String(text || '') }],
  };
  const result = await apply(userId, canonical, { settings: config });
  return { messages: result.canonical.messages.filter((message) => !(message.role === 'system' && !canonical.messages.some((original) => original.role === 'system'))), stats: result.stats };
}

module.exports = { getSettings, saveSettings, apply, preview, DEFAULT_SETTINGS };
