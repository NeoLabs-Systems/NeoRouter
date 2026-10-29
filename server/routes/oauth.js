'use strict';

const express = require('express');
const { z } = require('zod');
const openrouter = require('../services/oauth/openrouter_oauth');
const { requireSession } = require('../middleware/auth');
const { resolveSpace, requirePermission } = require('../middleware/space');
const { validate } = require('../middleware/validate');
const { getConfig } = require('../config/env');
const { asyncRoute } = require('./helpers');

const router = express.Router();

// The browser returns here from OpenRouter without a session header; the
// one-time state identifies the user who started the flow.
router.get('/openrouter/callback/:state', asyncRoute(async (req, res) => {
  res.redirect(await openrouter.complete({ state: req.params.state, code: typeof req.query.code === 'string' ? req.query.code : null }));
}));

router.post('/openrouter/start', requireSession, resolveSpace, requirePermission('providers.manage'), validate(z.object({ returnTo: z.string().url().max(2048) })), (req, res) => {
  const origin = getConfig().publicUrl || `${req.protocol}://${req.get('host')}`;
  res.json(openrouter.start({ userId: req.space.ownerId, createdBy: req.space.actorId, origin, requestHost: req.get('host'), returnTo: req.body.returnTo }));
});

// Approving an app that asked to connect needs the right to create keys.
router.use('/apps', requireSession, resolveSpace, requirePermission('keys.create'), require('./connect').approval);

module.exports = router;
