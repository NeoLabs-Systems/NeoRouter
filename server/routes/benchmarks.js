'use strict';
const router = require('express').Router();
const { z } = require('zod');
const { validate } = require('../middleware/validate');
const { requireOwnSpace } = require('../middleware/space');
const consent = require('../services/benchmarks/consent');
const store = require('../services/benchmarks/private_store');
const { getDatabase } = require('../db/database');
router.use(requireOwnSpace);
function status(userId) {
  const saved = consent.userSettings(userId);
  return { adminEnabled: consent.globalSettings().enabled, enabled: saved.enabled, allowReplay: saved.allowReplay,
    retentionDays: saved.retentionDays, counts: store.counts(userId),
    contributions: getDatabase().prepare('SELECT COUNT(*) count FROM benchmark_contributions WHERE user_id = ?').get(userId).count };
}
router.get('/settings', (req, res) => { res.set('Cache-Control', 'no-store'); res.json(status(req.auth.userId)); });
router.put('/settings', validate(z.object({ enabled: z.boolean(), allowReplay: z.boolean(), retentionDays: z.union([z.literal(1), z.literal(7), z.literal(30)]).optional() }).strict()), (req, res) => {
  consent.saveUser(req.auth.userId, req.body);
  res.json(status(req.auth.userId));
});
router.delete('/collection', (req, res) => { store.purge(req.auth.userId); res.status(204).end(); });
module.exports = router;
