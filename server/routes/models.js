'use strict';

const express = require('express');
const { z } = require('zod');
const catalog = require('../services/catalog/catalog_service');
const stats = require('../services/routing/stats_store');
const scoring = require('../services/routing/scoring');
const { validate } = require('../middleware/validate');

const capabilities = require('../core/capability_registry');

const router = express.Router();

function present(entry) {
  const snapshot = stats.snapshot(entry.ref);
  const availability = stats.availability(entry.ref);
  return {
    ...entry,
    reliability: scoring.reliability(snapshot, { sample: false }),
    speedScore: scoring.speed(entry, snapshot),
    costScore: scoring.cost(entry),
    availability,
    stats: { ...entry.stats, penalty: snapshot.penalty, successes: snapshot.successes, failures: snapshot.failures },
  };
}

router.get('/', (req, res) => {
  const capability = typeof req.query.capability === 'string' ? req.query.capability : 'chat';
  res.json({ models: catalog.listForUser(req.auth.userId, { capability, includeUnavailable: req.query.all === '1' }).map(present) });
});

router.get('/capabilities', (req, res) => {
  const routable = new Set(capabilities.list().map((item) => item.id));
  res.json({ capabilities: catalog.capabilitySummary(req.auth.userId).map((item) => ({ ...item, routable: routable.has(item.id) })) });
});

router.get('/:ref', (req, res) => res.json({ model: present(catalog.getModel(req.auth.userId, Number(req.params.ref))) }));

router.patch('/:ref', validate(z.object({
  enabled: z.boolean().optional(),
  overrides: z.object({
    free: z.boolean().nullable().optional(),
    priceIn: z.number().min(0).nullable().optional(),
    priceOut: z.number().min(0).nullable().optional(),
    contextWindow: z.number().int().positive().nullable().optional(),
    tools: z.boolean().nullable().optional(),
    vision: z.boolean().nullable().optional(),
    intelligence: z.number().min(0).max(1).nullable().optional(),
    priceUnit: z.enum(['image', 'second', 'minute', 'mchar', 'search']).nullable().optional(),
    unitPrice: z.number().min(0).max(1000).nullable().optional(),
    // Evidence learned from traffic can only be cleared, never set by hand.
    billedAt: z.null().optional(),
    noFreeTier: z.null().optional(),
  }).optional(),
})), (req, res) => res.json({ model: present(catalog.updateModel(req.auth.userId, Number(req.params.ref), req.body)) }));

router.post('/:ref/reset-health', (req, res) => {
  const model = catalog.getModel(req.auth.userId, Number(req.params.ref));
  stats.clearCooldown(model.ref);
  res.json({ model: present(catalog.getModel(req.auth.userId, model.ref)) });
});

router.post('/bulk', validate(z.object({
  accountId: z.string().optional(),
  enabled: z.boolean(),
  onlyFree: z.boolean().optional(),
})), (req, res) => res.json(catalog.bulkSetEnabled(req.auth.userId, req.body)));

module.exports = router;
