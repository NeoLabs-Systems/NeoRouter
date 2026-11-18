'use strict';

// Settings → Connect your apps: the app catalog, the right address for where
// an app runs, the live check of a key, and (admins) who may reach NeoRouter.
const express = require('express');
const { z } = require('zod');
const catalog = require('../services/apps/catalog');
const { places } = require('../services/apps/places');
const network = require('../services/apps/network');
const keyActivity = require('../services/apps/key_activity');
const { requireAdmin } = require('../middleware/auth');
const { requirePermission, requireOwnSpace } = require('../middleware/space');
const { validate } = require('../middleware/validate');
const { HttpError } = require('../middleware/error_handler');

const router = express.Router();

// The address the browser used, to offer it to apps on that same device.
const browserOrigin = (req) => `${req.protocol}://${req.get('host')}`;

router.get('/', (req, res) => res.json({
  apps: catalog.all(),
  places: places(browserOrigin(req)),
  network: network.status(),
}));

router.get('/keys/:id/activity', requirePermission('keys.view'), (req, res) => {
  const activity = keyActivity.activity(req.space.ownerId, req.params.id);
  if (!activity) throw new HttpError(404, 'KEY_NOT_FOUND', 'This key does not exist.');
  res.json(activity);
});

router.put('/network', requireOwnSpace, requireAdmin, validate(z.object({ scope: z.enum(['local', 'network']) })), (req, res) => {
  const result = network.setScope(req.body.scope);
  res.json({ ...result, places: places(browserOrigin(req)) });
});

module.exports = router;
