'use strict';

// Admin → Local models: what this server can run, installing, starting,
// stopping, repairing and removing models, and who may use them.
const express = require('express');
const { z } = require('zod');
const localModels = require('../services/local_models/local_model_service');
const access = require('../services/local_models/access');
const { validate } = require('../middleware/validate');
const { asyncRoute } = require('./helpers');

const router = express.Router();

router.get('/', asyncRoute(async (_req, res) => res.json(await localModels.overview())));
router.put('/access', validate(z.object({ level: z.enum(access.ACCESS) })), asyncRoute(async (req, res) => res.json(await localModels.setAccess(req.body.level))));
router.post('/starter', asyncRoute(async (req, res) => res.status(202).json(await localModels.installStarter(req.auth.userId))));
router.post('/:id/install', asyncRoute(async (req, res) => res.status(202).json(await localModels.install(req.auth.userId, [req.params.id]))));
router.post('/:id/start', asyncRoute(async (req, res) => res.json(await localModels.setEnabled(req.params.id, true))));
router.post('/:id/stop', asyncRoute(async (req, res) => res.json(await localModels.setEnabled(req.params.id, false))));
router.post('/:id/retry', asyncRoute(async (req, res) => res.json(await localModels.retry(req.params.id))));
router.post('/:id/repair', asyncRoute(async (req, res) => res.json(await localModels.repair(req.params.id))));
router.delete('/:id', asyncRoute(async (req, res) => res.json(await localModels.remove(req.params.id))));

module.exports = router;
