'use strict';

const express = require('express');
const { z } = require('zod');
const processing = require('../services/processing/processing_service');
const { CATEGORIES } = require('../services/processing/redaction');
const { LEVELS, ENGINES } = require('../services/processing/compression');
const { getDatabase } = require('../db/database');
const { validate } = require('../middleware/validate');
const { requirePermission } = require('../middleware/space');
const { asyncRoute } = require('./helpers');

const router = express.Router();

const settingsSchema = z.object({
  privacy: z.object({
    enabled: z.boolean(),
    modes: z.record(z.enum(['off', 'restore', 'hide'])).refine((modes) => Object.keys(modes).every((key) => CATEGORIES.includes(key)), 'Unknown category'),
    customTerms: z.array(z.string().trim().min(2).max(200)).max(500),
    ai: z.object({ enabled: z.boolean(), model: z.string().min(1).max(300), freeOnly: z.boolean() }),
  }),
  compression: z.object({
    level: z.enum(LEVELS),
    engines: z.record(z.boolean()).refine((engines) => Object.keys(engines).every((key) => key in ENGINES), 'Unknown engine'),
    minTokens: z.number().int().min(0).max(1_000_000),
    targetTokens: z.number().int().min(500).max(2_000_000).nullable(),
    keepRecent: z.number().int().min(1).max(50),
  }),
});

router.get('/settings', requirePermission('processing.view'), (req, res) => res.json({
  settings: processing.getSettings(req.space.ownerId),
  categories: CATEGORIES,
  levels: LEVELS,
  engines: ENGINES,
}));

router.put('/settings', requirePermission('processing.edit'), validate(settingsSchema), (req, res) => res.json({ settings: processing.saveSettings(req.space.ownerId, req.body) }));

// Shows what would be sent for a sample text, without sending anything.
router.post('/preview', requirePermission('processing.view'), validate(z.object({
  text: z.string().max(400_000).optional(),
  role: z.enum(['user', 'tool']).optional(),
  messages: z.array(z.object({ role: z.enum(['system', 'user', 'assistant', 'tool']), content: z.string().max(400_000) })).max(200).optional(),
  settings: settingsSchema.optional(),
})), asyncRoute(async (req, res) => res.json(await processing.preview(req.space.ownerId, req.body))));

router.get('/stats', requirePermission('processing.view'), (req, res) => {
  const since = Date.now() - 30 * 86_400_000;
  const row = getDatabase().prepare(`SELECT COUNT(*) requests, SUM(redacted_count > 0) protected_requests, SUM(redacted_count) redacted,
      SUM(compression_saved_tokens > 0) compressed_requests, SUM(compression_saved_tokens) saved_tokens
    FROM requests WHERE user_id = ? AND created_at >= ?`).get(req.space.ownerId, since);
  res.json({
    days: 30,
    requests: row.requests || 0,
    protectedRequests: row.protected_requests || 0,
    redacted: row.redacted || 0,
    compressedRequests: row.compressed_requests || 0,
    savedTokens: row.saved_tokens || 0,
  });
});

module.exports = router;
