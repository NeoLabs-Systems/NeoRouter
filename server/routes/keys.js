'use strict';

const express = require('express');
const { z } = require('zod');
const keys = require('../services/router_keys/router_key_service');
const { validate } = require('../middleware/validate');
const { requirePermission } = require('../middleware/space');

const router = express.Router();

const keyInput = z.object({
  name: z.string().min(1).max(80),
  mode: z.enum(['free_only', 'paid', 'both']).optional(),
  // The three kinds of models, chosen independently (preferred over mode).
  allowFree: z.boolean().optional(),
  allowPlan: z.boolean().optional(),
  allowPaid: z.boolean().optional(),
  budgetUsd: z.number().min(0).nullable().optional(),
  budgetPeriod: z.enum(['total', 'monthly', 'daily']).optional(),
  rpmLimit: z.number().int().positive().nullable().optional(),
  // Tokens on free and subscription models per period (null = no limit).
  tokenLimit: z.number().int().positive().nullable().optional(),
  tokenPeriod: z.enum(['total', 'monthly', 'daily']).optional(),
  allowedModels: z.array(z.string().min(1).max(200)).max(200).optional(),
  profile: z.string().max(80).optional(),
  expiresAt: z.string().datetime().nullable().optional(),
});

router.get('/', requirePermission('keys.view'), (req, res) => res.json({ keys: keys.list(req.space.ownerId) }));
router.post('/', requirePermission('keys.create'), validate(keyInput), (req, res) => res.status(201).json(keys.create(req.space.ownerId, req.body, req.space.actorId)));
router.patch('/:id', requirePermission('keys.edit'), validate(keyInput.partial()), (req, res) => res.json({ key: keys.update(req.space.ownerId, req.params.id, req.body) }));
router.post('/:id/revoke', requirePermission('keys.edit'), (req, res) => res.json({ key: keys.revoke(req.space.ownerId, req.params.id) }));
router.delete('/:id', requirePermission('keys.delete'), (req, res) => { keys.remove(req.space.ownerId, req.params.id); res.status(204).end(); });

module.exports = router;
