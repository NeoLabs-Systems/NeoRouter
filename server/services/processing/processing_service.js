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
    allowTerms: [],
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
      allowTerms: Array.isArray(privacy.allowTerms) ? privacy.allowTerms : [],
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
    const redacted = redaction.redactRequest(result, { modes: config.privacy.modes, customTerms: config.privacy.customTerms, allowTerms: config.privacy.allowTerms, entitiesFor });
    if (redacted) {
      result = redacted.canonical;
      vault = redacted.vault;
    }
  }
  return {
    canonical: result,
    vault,
    findings: vault ? vault.findings() : [],
    stats: {
      compression: compressed.stats,
      redacted: vault ? vault.size : 0,
      redactedByCategory: vault ? vault.counts : {},
      aiDetection: aiUsed,
    },
  };
}

// Texts that leave for a provider without an answer to put values back into
// (embeddings, image and video prompts, direct web searches). Only the "hide"
// categories apply: values that must never leave. Hiding names there would
// break what the caller asked for, and nothing could be restored.
function shieldTexts(userId, texts) {
  const config = getSettings(userId);
  if (!config.privacy.enabled) return { texts, redacted: 0 };
  const modes = Object.fromEntries(Object.entries(config.privacy.modes).map(([category, mode]) => [category, mode === 'hide' ? 'hide' : 'off']));
  const vault = new redaction.Vault(modes);
  const shielded = texts.map((text) => vault.redactText(text, { customTerms: config.privacy.customTerms, allowTerms: config.privacy.allowTerms }));
  return { texts: shielded, redacted: vault.size };
}

// The same pipeline for the settings page: shows what would be sent. Only
// someone who may edit the settings can preview a draft of them; everyone
// else sees the saved ones, with the AI pass kept to free models, so a
// view-only member cannot spend on the owner's paid models.
async function preview(userId, { text, messages, role = 'user', settings: draft, canEdit = true }) {
  const saved = getSettings(userId);
  const config = canEdit
    ? (draft ? merge(draft) : saved)
    : { ...saved, privacy: { ...saved.privacy, ai: { ...saved.privacy.ai, freeOnly: true } } };
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
  return { messages: result.canonical.messages.filter((message) => !(message.role === 'system' && !canonical.messages.some((original) => original.role === 'system'))), findings: result.findings, stats: result.stats };
}

module.exports = { getSettings, saveSettings, apply, shieldTexts, preview, DEFAULT_SETTINGS };
