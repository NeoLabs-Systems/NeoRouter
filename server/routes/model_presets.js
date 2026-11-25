'use strict';

// Saved model selections ("DeepSeek + Qwen Max") that every model selector
// can apply in one tap. Personal: stored per user, whatever space is open.
const router = require('express').Router();
const { z } = require('zod');
const settings = require('../services/settings/settings_service');
const { validate } = require('../middleware/validate');

const KEY = 'model_presets';
const preset = z.object({
  id: z.string().min(1).max(64),
  name: z.string().trim().min(1).max(60),
  models: z.array(z.string().min(1).max(300)).min(1).max(500),
});

router.get('/', (req, res) => res.json({ presets: settings.getUserSetting(req.auth.userId, KEY, []) }));

router.put('/', validate(z.object({ presets: z.array(preset).max(50) })), (req, res) => {
  settings.setUserSetting(req.auth.userId, KEY, req.body.presets);
  res.json({ presets: req.body.presets });
});

module.exports = router;
