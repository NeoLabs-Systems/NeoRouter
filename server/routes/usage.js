'use strict';

const express = require('express');
const usage = require('../services/usage/usage_service');
const evaluatorTraffic = require('../services/benchmarks/live_traffic');
const capabilities = require('../core/capability_registry');
const concurrency = require('../services/routing/concurrency');
const catalog = require('../services/catalog/catalog_service');
const accounts = require('../services/providers/account_service');
const quota = require('../services/quota/quota_service');
const { requirePermission } = require('../middleware/space');

const { summarizeLimits } = require('../services/quota/limit_summary');
const router = express.Router();
router.use(requirePermission('activity.view'));

// ?only=totals skips the series and breakdowns.
router.get('/summary', (req, res) => res.json(usage.summary(req.space.ownerId, String(req.query.range || '24h'), Date.now(), { onlyTotals: req.query.only === 'totals' })));

// ?keyIds=a,b limits the numbers to those keys.
const keyIdsOf = (req) => String(req.query.keyIds || '').split(',').map((id) => id.trim()).filter(Boolean);

router.get('/analytics', (req, res) => res.json(usage.analytics(req.space.ownerId, String(req.query.range || '24h'), Date.now(), { keyIds: keyIdsOf(req) })));

// How connections really did over the last day, and whether requests go unanswered.
router.get('/health', (req, res) => res.json(require('../services/usage/connection_health').health(req.space.ownerId)));

// What routing learned from this space's traffic.
router.get('/learning', (req, res) => res.json(require('../services/usage/learning_stats').learning(req.space.ownerId)));

router.get('/logs', (req, res) => {
  const ownerId = req.space.ownerId;
  const live = req.query.live === '1';
  const requestedLimit = Number(req.query.limit) || 100;
  const limit = live ? Math.min(100, Math.max(1, Math.trunc(requestedLimit))) : requestedLimit;
  const logs = usage.logs(ownerId, {
    limit,
    before: req.query.before ? Number(req.query.before) : null,
    keyId: req.query.keyId || null,
    keyIds: keyIdsOf(req),
    status: req.query.status || null,
  });
  if (!live) return res.json({ logs });
  res.set('Cache-Control', 'no-store').json({
    logs,
    ...(req.space.isOwner ? evaluatorTraffic.recent(ownerId, limit) : {}),
  });
});

// What the free models could take around the clock (home screen).
router.get('/capacity', (req, res) => res.json(require('../services/quota/capacity').freeCapacity(req.space.ownerId)));

// Quota overview: per provider account, shared pool windows plus every model
// that has a known limit, with usage, remaining and the limit's source.
router.get('/quotas', (req, res) => {
  // Every kind counts against a provider's allowance, not only chat.
  const entries = capabilities.list().flatMap((kind) => catalog.listForUser(req.space.ownerId, { capability: kind.id }));
  const byAccount = new Map(accounts.list(req.space.ownerId).map((account) => [account.id, {
    account, pool: [], models: [], probes: quota.accountStateNow(account.id),
    concurrency: concurrency.limitFor({ accountId: account.id, definitionId: account.definitionId }),
  }]));
  for (const entry of entries) {
    const group = byAccount.get(entry.accountId);
    if (!group) continue;
    if (entry.isFlatRate && !group.slots) group.slots = require('../services/routing/slot_utilization').snapshot(entry.accountId);
    if (entry.isPlan && !entry.isFlatRate && group.allowancePace == null) group.allowancePace = require('../services/routing/plans').pace(entry);
    const { details } = quota.evaluate(entry, 0);
    const pool = details.filter((item) => item.kind === 'pool');
    const own = details.filter((item) => item.kind === 'model');
    for (const item of pool) if (!group.pool.some((existing) => existing.metric === item.metric)) group.pool.push(item);
    if (own.length || entry.isFree) group.models.push({ ref: entry.ref, id: entry.id, name: entry.name, isFree: entry.isFree, windows: own });
  }
  res.json({ accounts: [...byAccount.values()].map((group) => ({ ...group, limits: summarizeLimits(group) })) });
});

module.exports = router;
