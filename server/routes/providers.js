'use strict';

const express = require('express');
const { z } = require('zod');
const registry = require('../providers/registry');
const accounts = require('../services/providers/account_service');
const catalog = require('../services/catalog/catalog_service');
const quota = require('../services/quota/quota_service');
const { validate } = require('../middleware/validate');
const { asyncRoute } = require('./helpers');
const { createLogger } = require('../utils/logger');

const logger = createLogger('providers');
const router = express.Router();

router.get('/definitions', (_req, res) => res.json({ definitions: registry.list().map(registry.publicDefinition) }));

router.get('/accounts', (req, res) => res.json({ accounts: accounts.list(req.auth.userId) }));

const accountInput = z.object({
  definitionId: z.string().min(1),
  label: z.string().max(80).optional(),
  slug: z.string().max(40).optional(),
  secret: z.string().max(4096).optional(),
  baseUrl: z.string().max(2048).optional(),
  options: z.object({
    manualModels: z.array(z.string().min(1).max(300)).max(500).optional(),
    authHeader: z.string().regex(/^[A-Za-z0-9-]{1,64}$/).optional(),
    modelsPath: z.string().max(512).optional(),
  }).catchall(z.string().max(512)).optional(),
  headers: z.record(z.string().max(2048)).optional(),
  billingMode: z.enum(['free_tier', 'paid']).optional(),
});

router.post('/probe', validate(accountInput), asyncRoute(async (req, res) => res.json(await accounts.probe(req.body))));

router.post('/accounts', validate(accountInput), asyncRoute(async (req, res) => {
  const { account, validation } = await accounts.create(req.auth.userId, req.body);
  let sync = null;
  try {
    sync = await catalog.syncAccount(account.id);
  } catch (error) {
    sync = { error: error.message };
    logger.warn('Initial model sync failed', { accountId: account.id, error: error.message });
  }
  res.status(201).json({ account: accounts.get(req.auth.userId, account.id), validation, sync });
}));

router.patch('/accounts/:id', validate(accountInput.omit({ definitionId: true }).extend({ enabled: z.boolean().optional() })), asyncRoute(async (req, res) => {
  const account = await accounts.update(req.auth.userId, req.params.id, req.body);
  catalog.reclassifyAccount(req.params.id);
  res.json({ account: accounts.get(req.auth.userId, account.id) });
}));

router.delete('/accounts/:id', (req, res) => { accounts.remove(req.auth.userId, req.params.id); res.status(204).end(); });

router.post('/accounts/:id/validate', asyncRoute(async (req, res) => {
  accounts.getRow(req.auth.userId, req.params.id);
  const validation = await accounts.validateAccount(req.params.id);
  res.json({ validation, account: accounts.get(req.auth.userId, req.params.id) });
}));

router.post('/accounts/:id/sync', asyncRoute(async (req, res) => {
  accounts.getRow(req.auth.userId, req.params.id);
  const sync = await catalog.syncAccount(req.params.id);
  res.json({ sync, account: accounts.get(req.auth.userId, req.params.id) });
}));

router.post('/accounts/:id/quota', asyncRoute(async (req, res) => {
  accounts.getRow(req.auth.userId, req.params.id);
  const adapter = accounts.adapterFor(accounts.getRuntime(req.params.id));
  const results = (await adapter.fetchQuota()) || [];
  for (const item of results) quota.setState(`a:${req.params.id}`, item.metric, { limit: item.limit, remaining: item.remaining, resetAt: item.resetAt || null }, 'probe');
  res.json({ quota: results });
}));

module.exports = router;
