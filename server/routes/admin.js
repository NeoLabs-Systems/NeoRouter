'use strict';

const express = require('express');
const { z } = require('zod');
const auth = require('../services/auth/auth_service');
const settings = require('../services/settings/settings_service');
const metadata = require('../services/catalog/metadata_service');
const { requireAdmin } = require('../middleware/auth');
const { validate } = require('../middleware/validate');
const { asyncRoute } = require('./helpers');

const router = express.Router();
router.use(requireAdmin);

router.get('/users', (_req, res) => res.json({ users: auth.listUsers() }));
router.patch('/users/:id', validate(z.object({ disabled: z.boolean().optional(), role: z.enum(['admin', 'user']).optional() })), (req, res) => {
  if (req.body.disabled !== undefined) auth.setUserDisabled(req.auth.userId, req.params.id, req.body.disabled);
  if (req.body.role) auth.setUserRole(req.auth.userId, req.params.id, req.body.role);
  res.json({ users: auth.listUsers() });
});
router.delete('/users/:id', (req, res) => { auth.deleteUser(req.auth.userId, req.params.id); res.status(204).end(); });

router.get('/settings', (_req, res) => res.json({
  registrationOpen: settings.registrationOpen(),
  metadata: metadata.status(),
}));
router.patch('/settings', validate(z.object({
  registrationOpen: z.boolean().optional(),
  artificialAnalysisKey: z.string().max(512).nullable().optional(),
})), (req, res) => {
  if (req.body.registrationOpen !== undefined) settings.setGlobal('registration_open', req.body.registrationOpen);
  if (req.body.artificialAnalysisKey !== undefined) metadata.setArtificialAnalysisKey(req.body.artificialAnalysisKey);
  res.json({ registrationOpen: settings.registrationOpen(), metadata: metadata.status() });
});
router.post('/metadata/refresh', asyncRoute(async (_req, res) => res.json({ results: await metadata.refreshSources(), metadata: metadata.status() })));

module.exports = router;
