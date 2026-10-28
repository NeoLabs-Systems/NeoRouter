'use strict';

const express = require('express');
const { z } = require('zod');
const routerKeys = require('../services/router_keys/router_key_service');
const { execute } = require('../capabilities/chat/executor');
const { HttpError } = require('../middleware/error_handler');
const { validate } = require('../middleware/validate');
const { asyncRoute } = require('./helpers');

const router = express.Router();

// Runs a chat through the real gateway pipeline with one of the user's keys,
// so the dashboard can test routing without ever holding a key secret.
router.post('/chat', validate(z.object({
  keyId: z.string().min(1),
  model: z.string().max(300).default('auto'),
  messages: z.array(z.object({ role: z.enum(['system', 'user', 'assistant']), content: z.string().max(200000) })).min(1).max(200),
  max_tokens: z.number().int().positive().max(32000).optional(),
  temperature: z.number().min(0).max(2).optional(),
})), asyncRoute(async (req, res) => {
  const key = routerKeys.getRow(req.auth.userId, req.body.keyId);
  if (key.revoked_at) throw new HttpError(400, 'KEY_REVOKED', 'This router key is revoked.');
  const { keyId, ...body } = req.body;
  await execute({ req, res, key, format: 'openai_chat', body: { ...body, stream: false } });
}));

module.exports = router;
