'use strict';

const router = require('express').Router();
const { z } = require('zod');
const { validate } = require('../middleware/validate');
const { requireOwnSpace } = require('../middleware/space');
const consent = require('../services/benchmarks/consent');
const capture = require('../services/benchmarks/capture');
const evidence = require('../services/benchmarks/evidence');

router.use(requireOwnSpace);
function status(userId) {
  return {
    adminEnabled: consent.globalSettings().enabled,
    enabled: consent.userSettings(userId).enabled,
    counts: { pending: capture.count(userId) },
    contributions: evidence.countScores(userId),
  };
}

router.get('/settings', (req, res) => res.set('Cache-Control', 'no-store').json(status(req.auth.userId)));
router.put('/settings', validate(z.object({ enabled: z.boolean() }).strict()), (req, res) => {
  consent.saveUser(req.auth.userId, req.body.enabled);
  res.json(status(req.auth.userId));
});
router.delete('/collection', (req, res) => {
  evidence.purge(req.auth.userId);
  res.status(204).end();
});

module.exports = router;
