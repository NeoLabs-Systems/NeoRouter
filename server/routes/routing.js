'use strict';

const express = require('express');
const { z } = require('zod');
const profiles = require('../services/routing/profiles');
const router_ = require('../services/routing/router');
const loadTracker = require('../services/routing/load_tracker');
const { requirements, estimateInputTokens, normalizeRequest } = require('../capabilities/chat/canonical');
const { validate } = require('../middleware/validate');

const router = express.Router();

router.get('/profiles', (req, res) => res.json({ profiles: profiles.list(req.auth.userId) }));
router.post('/profiles', validate(z.object({
  name: z.string().min(1).max(80),
  weights: profiles.weightsSchema,
  constraints: profiles.constraintsSchema.optional(),
})), (req, res) => res.status(201).json({ profile: profiles.create(req.auth.userId, req.body) }));
router.patch('/profiles/:id', validate(z.object({
  name: z.string().min(1).max(80).optional(),
  weights: profiles.weightsSchema.optional(),
  constraints: profiles.constraintsSchema.optional(),
})), (req, res) => res.json({ profile: profiles.update(req.auth.userId, req.params.id, req.body) }));
router.delete('/profiles/:id', (req, res) => { profiles.remove(req.auth.userId, req.params.id); res.status(204).end(); });

// Shows what `auto` would choose right now, without sending a request.
router.post('/preview', validate(z.object({
  profile: z.string().max(80).optional(),
  mode: z.enum(['free_only', 'paid', 'both']).optional(),
  model: z.string().max(300).optional(),
  prompt: z.string().max(200000).optional(),
  tools: z.boolean().optional(),
  vision: z.boolean().optional(),
})), (req, res) => {
  const request = normalizeRequest({
    messages: [{ role: 'user', content: req.body.vision ? [{ type: 'text', text: req.body.prompt || 'Hello' }, { type: 'image_url', image_url: { url: 'data:image/png;base64,' } }] : req.body.prompt || 'Hello' }],
    ...(req.body.tools ? { tools: [{ type: 'function', function: { name: 'probe', parameters: { type: 'object', properties: {} } } }] } : {}),
  });
  const key = { id: 'preview', user_id: req.auth.userId, mode: req.body.mode || 'both', budget_usd: null, budget_period: 'total', allowed_models_json: '[]', profile: req.body.profile || 'balanced' };
  res.json(router_.preview({
    userId: req.auth.userId, key, requestedModel: req.body.model || 'auto', request, needs: requirements(request), inputTokens: estimateInputTokens(request),
  }));
});

router.get('/load', (_req, res) => res.json(loadTracker.snapshot()));

module.exports = router;
