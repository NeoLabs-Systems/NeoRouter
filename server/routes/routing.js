'use strict';

const express = require('express');
const { z } = require('zod');
const profiles = require('../services/routing/profiles');
const router_ = require('../services/routing/router');
const loadTracker = require('../services/routing/load_tracker');
const smoothing = require('../services/routing/smoothing');
const { requirements, estimateInputTokens, normalizeRequest } = require('../capabilities/chat/canonical');
const { validate } = require('../middleware/validate');
const { requirePermission } = require('../middleware/space');

const router = express.Router();

// Profile names are readable by every member, since key forms pick from them.
router.get('/profiles', (req, res) => res.json({ profiles: profiles.list(req.space.ownerId) }));
router.post('/profiles', requirePermission('routing.edit'), validate(z.object({
  name: z.string().min(1).max(80),
  weights: profiles.weightsSchema,
  constraints: profiles.constraintsSchema.optional(),
})), (req, res) => res.status(201).json({ profile: profiles.create(req.space.ownerId, req.body, req.space.actorId) }));
router.patch('/profiles/:id', requirePermission('routing.edit'), validate(z.object({
  name: z.string().min(1).max(80).optional(),
  weights: profiles.weightsSchema.optional(),
  constraints: profiles.constraintsSchema.optional(),
})), (req, res) => res.json({ profile: profiles.update(req.space.ownerId, req.params.id, req.body) }));
router.delete('/profiles/:id', requirePermission('routing.edit'), (req, res) => { profiles.remove(req.space.ownerId, req.params.id); res.status(204).end(); });

// Shows what `auto` would choose right now, without sending a request.
router.post('/preview', requirePermission('routing.view'), validate(z.object({
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
  const key = { id: 'preview', user_id: req.space.ownerId, mode: req.body.mode || 'both', budget_usd: null, budget_period: 'total', allowed_models_json: '[]', profile: req.body.profile || 'balanced' };
  res.json(router_.preview({
    userId: req.space.ownerId, key, requestedModel: req.body.model || 'auto', request, needs: requirements(request), inputTokens: estimateInputTokens(request),
  }));
});

// Settings → Usage: even out usage, once there is enough data to go by.
router.get('/smoothing', requirePermission('routing.view'), (req, res) => res.json(smoothing.status(req.space.ownerId)));
router.put('/smoothing', requirePermission('routing.edit'), validate(z.object({ enabled: z.boolean() })),
  (req, res) => res.json(smoothing.setEnabled(req.space.ownerId, req.body.enabled)));
router.get('/load', requirePermission('routing.view'), (_req, res) => res.json(loadTracker.snapshot()));

module.exports = router;
