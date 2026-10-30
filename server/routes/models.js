'use strict';

const express = require('express');
const { z } = require('zod');
const catalog = require('../services/catalog/catalog_service');
const stats = require('../services/routing/stats_store');
const scoring = require('../services/routing/scoring');
const accounts = require('../services/providers/account_service');
const { validate } = require('../middleware/validate');
const { requirePermission } = require('../middleware/space');

const capabilities = require('../core/capability_registry');

const router = express.Router();

// Live models: the WebSocket protocols that reach them (capabilities/live).
function liveProtocols(entry) {
  if (entry.capability !== 'live') return undefined;
  const runtime = accounts.getRuntime(entry.accountId);
  return runtime ? accounts.adapterFor(runtime).definition.live : [];
}

function present(entry) {
  const snapshot = stats.snapshot(entry.ref);
  const availability = stats.availability(entry.ref);
  return {
    ...entry,
    liveProtocols: liveProtocols(entry),
    reliability: scoring.reliability(snapshot, { sample: false }),
    speedScore: scoring.speed(entry, snapshot),
    costScore: scoring.cost(entry),
    availability,
    stats: { ...entry.stats, penalty: snapshot.penalty, successes: snapshot.successes, failures: snapshot.failures },
  };
}

router.get('/', (req, res) => {
  const capability = typeof req.query.capability === 'string' ? req.query.capability : 'chat';
  res.json({ models: catalog.listForUser(req.space.ownerId, { capability, includeUnavailable: req.query.all === '1' }).map(present) });
});

router.get('/capabilities', (req, res) => {
  const routable = new Set(capabilities.list().map((item) => item.id));
  res.json({ capabilities: catalog.capabilitySummary(req.space.ownerId).map((item) => ({ ...item, routable: routable.has(item.id) })) });
});

router.get('/:ref', (req, res) => res.json({ model: present(catalog.getModel(req.space.ownerId, Number(req.params.ref))) }));

router.patch('/:ref', requirePermission('models.manage'), validate(z.object({
  enabled: z.boolean().optional(),
  overrides: z.object({
    free: z.boolean().nullable().optional(),
    priceIn: z.number().min(0).nullable().optional(),
    priceOut: z.number().min(0).nullable().optional(),
    contextWindow: z.number().int().positive().nullable().optional(),
    tools: z.boolean().nullable().optional(),
    vision: z.boolean().nullable().optional(),
    intelligence: z.number().min(0).max(1).nullable().optional(),
    priceUnit: z.enum(['image', 'second', 'minute', 'mchar', 'search', 'call']).nullable().optional(),
    unitPrice: z.number().min(0).max(1000).nullable().optional(),
    // Evidence learned from traffic can only be cleared, never set by hand.
    billedAt: z.null().optional(),
    noFreeTier: z.null().optional(),
  }).optional(),
})), (req, res) => res.json({ model: present(catalog.updateModel(req.space.ownerId, Number(req.params.ref), req.body)) }));

router.post('/:ref/reset-health', requirePermission('models.manage'), (req, res) => {
  const model = catalog.getModel(req.space.ownerId, Number(req.params.ref));
  stats.clearCooldown(model.ref);
  res.json({ model: present(catalog.getModel(req.space.ownerId, model.ref)) });
});

router.post('/bulk', requirePermission('models.manage'), validate(z.object({
  accountId: z.string().optional(),
  enabled: z.boolean(),
  onlyFree: z.boolean().optional(),
})), (req, res) => res.json(catalog.bulkSetEnabled(req.space.ownerId, req.body)));

module.exports = router;
