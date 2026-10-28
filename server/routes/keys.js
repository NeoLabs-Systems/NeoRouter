'use strict';

const express = require('express');
const { z } = require('zod');
const keys = require('../services/router_keys/router_key_service');
const { validate } = require('../middleware/validate');

const router = express.Router();

const keyInput = z.object({
  name: z.string().min(1).max(80),
  mode: z.enum(['free_only', 'paid', 'both']).optional(),
  budgetUsd: z.number().min(0).nullable().optional(),
  budgetPeriod: z.enum(['total', 'monthly', 'daily']).optional(),
  rpmLimit: z.number().int().positive().nullable().optional(),
  allowedModels: z.array(z.string().min(1).max(200)).max(200).optional(),
  profile: z.string().max(80).optional(),
  expiresAt: z.string().datetime().nullable().optional(),
});

router.get('/', (req, res) => res.json({ keys: keys.list(req.auth.userId) }));
router.post('/', validate(keyInput), (req, res) => res.status(201).json(keys.create(req.auth.userId, req.body)));
router.patch('/:id', validate(keyInput.partial()), (req, res) => res.json({ key: keys.update(req.auth.userId, req.params.id, req.body) }));
router.post('/:id/revoke', (req, res) => res.json({ key: keys.revoke(req.auth.userId, req.params.id) }));
router.delete('/:id', (req, res) => { keys.remove(req.auth.userId, req.params.id); res.status(204).end(); });

module.exports = router;
