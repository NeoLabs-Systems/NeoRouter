'use strict';

const router = require('express').Router();
const { z } = require('zod');
const pool = require('../services/proxies/pool');
const settings = require('../services/settings/settings_service');
const { validate } = require('../middleware/validate');
const { HttpError } = require('../middleware/error_handler');
const { isPrivateDestination } = require('../providers/net_guard');
const { asyncRoute } = require('./helpers');

// Mounted behind the admin router's authentication and role checks.
router.get('/', (_req, res) => res.json({ proxies: pool.list(), policy: pool.config() }));

router.put('/policy', validate(z.object({ enabled: z.boolean(), fallbackDirect: z.boolean() })), (req, res) => {
  settings.setGlobal('outbound_proxy_policy', req.body);
  res.json({ policy: pool.config() });
});

router.post('/import', validate(z.object({
  text: z.string().max(1000000),
  protocol: z.enum(['socks5', 'http', 'https']).default('socks5'),
})), (req, res) => res.json(pool.importLines(req.body.text, req.body.protocol)));

// Several at once: ids, or every proxy with all: true.
router.post('/bulk', validate(z.object({
  action: z.enum(['enable', 'disable', 'delete']),
  ids: z.array(z.string().max(200)).max(100000).default([]),
  all: z.boolean().default(false),
})), (req, res) => {
  const ids = req.body.all ? pool.list().map((row) => row.id) : req.body.ids;
  for (const id of ids) {
    if (req.body.action === 'delete') pool.remove(id);
    else pool.change(id, req.body.action === 'enable');
  }
  res.json({ changed: ids.length });
});

router.post('/:id/test', validate(z.object({ url: z.string().url().max(2048) })), asyncRoute(async (req, res) => {
  const url = new URL(req.body.url);
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password || await isPrivateDestination(url.toString())) {
    throw new HttpError(400, 'INVALID_TEST_URL', 'Choose a public HTTP or HTTPS URL without credentials.');
  }
  res.json(await pool.probe(req.params.id, url.toString()));
}));

router.patch('/:id', validate(z.object({ enabled: z.boolean() })), (req, res) => {
  pool.change(req.params.id, req.body.enabled);
  res.status(204).end();
});

router.delete('/:id', (req, res) => {
  pool.remove(req.params.id);
  res.status(204).end();
});

module.exports = router;
