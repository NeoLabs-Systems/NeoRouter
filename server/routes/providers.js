'use strict';

const express = require('express');
const { z } = require('zod');
const registry = require('../providers/registry');
const billing = require('../services/providers/billing_check');
const accounts = require('../services/providers/account_service');
const codexOAuth = require('../services/providers/codex_oauth');
const catalog = require('../services/catalog/catalog_service');
const { validate } = require('../middleware/validate');
const { requirePermission } = require('../middleware/space');
const { asyncRoute } = require('./helpers');
const { createLogger } = require('../utils/logger');

const logger = createLogger('providers');
const router = express.Router();

router.get('/definitions', (_req, res) => res.json({ definitions: registry.list().map(registry.publicDefinition) }));

router.get('/accounts', requirePermission('providers.view'), (req, res) => res.json({ accounts: accounts.list(req.space.ownerId) }));

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
  // 'plan': a prepaid subscription; 'plan_paid': the plan, then pay per use.
  billingMode: z.enum(['free_tier', 'paid', 'plan', 'plan_paid']).optional(),
});

// ChatGPT (Codex) sign-in with a device code: start, then poll until the
// user approved it at auth.openai.com/codex/device. Pending sign-ins belong to
// the space that started them. With accountId, an existing connection is
// signed in again instead of a new one being created.
const chatgptLogins = new Map();

router.post('/chatgpt/login', requirePermission('providers.manage'), asyncRoute(async (req, res) => {
  const login = await codexOAuth.startDeviceLogin();
  const billingMode = ['plan', 'plan_paid', 'paid'].includes(req.body?.billingMode) ? req.body.billingMode : 'plan';
  chatgptLogins.set(login.loginId, { ownerId: req.space.ownerId, actorId: req.space.actorId, accountId: req.body?.accountId || null, billingMode });
  res.json(login);
}));

router.post('/chatgpt/login/:loginId', requirePermission('providers.manage'), asyncRoute(async (req, res) => {
  const pending = chatgptLogins.get(req.params.loginId);
  if (!pending || pending.ownerId !== req.space.ownerId) return res.status(404).json({ error: { code: 'LOGIN_NOT_FOUND', message: 'This sign-in expired. Start again.' } });
  const result = await codexOAuth.pollDeviceLogin(req.params.loginId);
  if (result.status !== 'done') {
    if (result.status === 'expired') chatgptLogins.delete(req.params.loginId);
    return res.json({ status: result.status });
  }
  chatgptLogins.delete(req.params.loginId);
  let account;
  if (pending.accountId) {
    account = await accounts.update(pending.ownerId, pending.accountId, { secret: result.secret });
  } else {
    ({ account } = await accounts.create(pending.ownerId, { definitionId: 'chatgpt', secret: result.secret, billingMode: pending.billingMode }, pending.actorId));
  }
  try {
    await catalog.syncAccount(account.id);
  } catch (error) {
    logger.warn('ChatGPT model sync failed', { accountId: account.id, error: error.message });
  }
  return res.json({ status: 'done', plan: result.plan, account: accounts.get(pending.ownerId, account.id) });
}));

router.post('/probe', requirePermission('providers.manage'), validate(accountInput), asyncRoute(async (req, res) => res.json(await accounts.probe(req.body))));

router.post('/accounts', requirePermission('providers.manage'), validate(accountInput), asyncRoute(async (req, res) => {
  const { account, validation } = await accounts.create(req.space.ownerId, req.body, req.space.actorId);
  let sync = null;
  try {
    sync = await catalog.syncAccount(account.id);
  } catch (error) {
    sync = { error: error.message };
    logger.warn('Initial model sync failed', { accountId: account.id, error: error.message });
  }
  await billing.verifyAccount(account.id);
  // A plan's allowance should be known before the first request.
  await accounts.probeQuota(account.id).catch(() => {});
  res.status(201).json({ account: accounts.get(req.space.ownerId, account.id), validation, sync });
}));

router.patch('/accounts/:id', requirePermission('providers.manage'), validate(accountInput.omit({ definitionId: true }).extend({ enabled: z.boolean().optional() })), asyncRoute(async (req, res) => {
  const account = await accounts.update(req.space.ownerId, req.params.id, req.body);
  catalog.reclassifyAccount(req.params.id);
  if (req.body.billingMode !== undefined || req.body.secret !== undefined) await billing.verifyAccount(req.params.id);
  res.json({ account: accounts.get(req.space.ownerId, account.id) });
}));

router.delete('/accounts/:id', requirePermission('providers.delete'), (req, res) => { accounts.remove(req.space.ownerId, req.params.id); res.status(204).end(); });

router.post('/accounts/:id/validate', requirePermission('providers.manage'), asyncRoute(async (req, res) => {
  accounts.getRow(req.space.ownerId, req.params.id);
  const validation = await accounts.validateAccount(req.params.id);
  res.json({ validation, account: accounts.get(req.space.ownerId, req.params.id) });
}));

router.post('/accounts/:id/sync', requirePermission('providers.manage'), asyncRoute(async (req, res) => {
  accounts.getRow(req.space.ownerId, req.params.id);
  const sync = await catalog.syncAccount(req.params.id);
  res.json({ sync, account: accounts.get(req.space.ownerId, req.params.id) });
}));

router.post('/accounts/:id/quota', requirePermission('providers.view'), asyncRoute(async (req, res) => {
  accounts.getRow(req.space.ownerId, req.params.id);
  res.json({ quota: await accounts.probeQuota(req.params.id) });
}));

module.exports = router;
