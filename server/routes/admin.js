'use strict';

const express = require('express');
const { z } = require('zod');
const auth = require('../services/auth/auth_service');
const settings = require('../services/settings/settings_service');
const metadata = require('../services/catalog/metadata_service');
const privacyModel = require('../services/processing/privacy_model');
const teamAudit = require('../services/teams/audit');
const teamMembers = require('../services/teams/members');
const { requireAdmin } = require('../middleware/auth');
const { validate } = require('../middleware/validate');
const { asyncRoute } = require('./helpers');

const router = express.Router();
router.use(requireAdmin);

// Each user with the owner whose team they are in, if any.
const listUsers = () => auth.listUsers().map((user) => {
  const current = teamMembers.membership(user.id);
  return { ...user, teamOwner: current ? teamMembers.publicUser(current.ownerUserId) : null };
});

router.get('/users', (_req, res) => res.json({ users: listUsers() }));
router.patch('/users/:id', validate(z.object({ disabled: z.boolean().optional(), role: z.enum(['admin', 'user']).optional() })), (req, res) => {
  if (req.body.disabled !== undefined) auth.setUserDisabled(req.auth.userId, req.params.id, req.body.disabled);
  if (req.body.role) auth.setUserRole(req.auth.userId, req.params.id, req.body.role);
  res.json({ users: listUsers() });
});
router.get('/team-audit', (req, res) => res.json({ events: teamAudit.list(req.query.limit) }));
router.delete('/users/:id', (req, res) => { auth.deleteUser(req.auth.userId, req.params.id); res.status(204).end(); });

const adminSettings = () => ({
  registrationOpen: settings.registrationOpen(),
  metadata: metadata.status(),
  privacyModel: privacyModel.status(),
});

router.get('/settings', (_req, res) => res.json(adminSettings()));
router.patch('/settings', validate(z.object({
  registrationOpen: z.boolean().optional(),
  artificialAnalysisKey: z.string().max(512).nullable().optional(),
  privacyModel: z.boolean().optional(),
})), (req, res) => {
  if (req.body.registrationOpen !== undefined) settings.setGlobal('registration_open', req.body.registrationOpen);
  if (req.body.artificialAnalysisKey !== undefined) metadata.setArtificialAnalysisKey(req.body.artificialAnalysisKey);
  if (req.body.privacyModel !== undefined) privacyModel.setEnabled(req.body.privacyModel);
  res.json(adminSettings());
});
router.post('/metadata/refresh', asyncRoute(async (_req, res) => res.json({ results: await metadata.refreshSources(), metadata: metadata.status() })));

module.exports = router;
