'use strict';

const express = require('express');
const { z } = require('zod');
const groups = require('../services/routing/groups');
const profiles = require('../services/routing/profiles');
const router_ = require('../services/routing/router');
const loadTracker = require('../services/routing/load_tracker');
const smoothing = require('../services/routing/smoothing');
const { requirements, estimateInputTokens, normalizeRequest } = require('../capabilities/chat/canonical');
const { validate } = require('../middleware/validate');
const { requirePermission } = require('../middleware/space');

const router = express.Router();

router.get('/groups', requirePermission('routing.view'), (req, res) => res.json({ groups: groups.status(req.space.ownerId) }));
router.post('/groups', requirePermission('routing.edit'), validate(groups.schema), (req, res) => res.status(201).json({ group: groups.save(req.space.ownerId, req.body) }));
router.put('/groups/:id', requirePermission('routing.edit'), validate(groups.schema), (req, res) => res.json({ group: groups.save(req.space.ownerId, req.body, req.params.id) }));
router.delete('/groups/:id', requirePermission('routing.edit'), (req, res) => { groups.remove(req.space.ownerId, req.params.id); res.status(204).end(); });

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
  workload: z.enum(['subagent']).optional(),
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
    userId: req.space.ownerId, key, workload: req.body.workload, requestedModel: req.body.model || 'auto', request, needs: requirements(request), inputTokens: estimateInputTokens(request),
  }));
});

// Settings → Usage: even out usage, once there is enough data to go by.
router.get('/smoothing', requirePermission('routing.view'), (req, res) => res.json(smoothing.status(req.space.ownerId)));
router.put('/smoothing', requirePermission('routing.edit'), validate(z.object({ enabled: z.boolean() })),
  (req, res) => res.json(smoothing.setEnabled(req.space.ownerId, req.body.enabled)));
// The home screen's live view: what this account's router is doing, by long polling.
router.get('/live', requirePermission('routing.view'), async (req, res) => {
  const since = Math.max(0, Number.parseInt(req.query.since, 10) || 0);
  const waitMs = Math.min(25_000, Math.max(0, Number.parseInt(req.query.wait, 10) || 0));
  res.set('Cache-Control', 'no-store');
  const result = await require('../services/routing/live_events').read(req.space.ownerId, since, waitMs);
  // A snapshot also brings the cards to draw, busiest of the last day first,
  // so the view needs nothing else to start.
  if (result.snapshot) result.cards = liveCards(req.space.ownerId);
  res.json(result);
});

function liveCards(userId) {
  const db = require('../db/database').getDatabase();
  const since = Date.now() - 86_400_000;
  const byAccount = new Map(db.prepare(`SELECT account_id id, COUNT(*) n FROM requests WHERE user_id = ? AND created_at >= ? AND account_id IS NOT NULL GROUP BY account_id`).all(userId, since).map((row) => [row.id, row.n]));
  const providers = new Map();
  for (const account of require('../services/providers/account_service').list(userId).filter((item) => item.enabled)) {
    const card = providers.get(account.providerKey) || { key: account.providerKey, name: account.providerName, slugs: [], requests: 0 };
    card.slugs.push(account.slug);
    card.requests += byAccount.get(account.id) || 0;
    providers.set(account.providerKey, card);
  }
  const keys = db.prepare(`SELECT k.name, COUNT(r.id) n FROM router_keys k LEFT JOIN requests r ON r.router_key_id = k.id AND r.created_at >= ?
    WHERE k.user_id = ? AND k.revoked_at IS NULL AND k.peer_link_id IS NULL GROUP BY k.id ORDER BY n DESC, k.created_at`).all(since, userId);
  return {
    keys: keys.map((row) => ({ name: row.name, requests: row.n })),
    providers: [...providers.values()].sort((a, b) => b.requests - a.requests),
  };
}
router.get('/load', requirePermission('routing.view'), (_req, res) => res.json(loadTracker.snapshot()));

module.exports = router;
