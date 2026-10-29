'use strict';

const express = require('express');
const usage = require('../services/usage/usage_service');
const catalog = require('../services/catalog/catalog_service');
const accounts = require('../services/providers/account_service');
const quota = require('../services/quota/quota_service');
const { requirePermission } = require('../middleware/space');

const router = express.Router();
router.use(requirePermission('activity.view'));

router.get('/summary', (req, res) => res.json(usage.summary(req.space.ownerId, String(req.query.range || '24h'))));

router.get('/analytics', (req, res) => res.json(usage.analytics(req.space.ownerId, String(req.query.range || '24h'))));

router.get('/logs', (req, res) => res.json({
  logs: usage.logs(req.space.ownerId, {
    limit: Number(req.query.limit) || 100,
    before: req.query.before ? Number(req.query.before) : null,
    keyId: req.query.keyId || null,
    status: req.query.status || null,
  }),
}));

// Quota overview: per provider account, shared pool windows plus every model
// that has a known limit, with usage, remaining and the limit's source.
router.get('/quotas', (req, res) => {
  const entries = catalog.listForUser(req.space.ownerId);
  const byAccount = new Map(accounts.list(req.space.ownerId).map((account) => [account.id, { account, pool: [], models: [], probes: quota.accountState(account.id) }]));
  for (const entry of entries) {
    const group = byAccount.get(entry.accountId);
    if (!group) continue;
    const { details } = quota.evaluate(entry, 0);
    const pool = details.filter((item) => item.kind === 'pool');
    const own = details.filter((item) => item.kind === 'model');
    for (const item of pool) if (!group.pool.some((existing) => existing.metric === item.metric)) group.pool.push(item);
    if (own.length || entry.isFree) group.models.push({ ref: entry.ref, id: entry.id, name: entry.name, isFree: entry.isFree, windows: own });
  }
  res.json({ accounts: [...byAccount.values()] });
});

module.exports = router;
