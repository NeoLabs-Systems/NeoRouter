'use strict';

const express = require('express');
const { requireSession } = require('../middleware/auth');
const { resolveSpace, requireOwnSpace } = require('../middleware/space');
const { getConfig } = require('../config/env');
const capabilities = require('../core/capability_registry');

const router = express.Router();

router.use('/auth', require('./auth'));
router.use('/oauth', require('./oauth'));
router.use(requireSession);
router.use(resolveSpace);
router.get('/info', (req, res) => {
  const config = getConfig();
  const origin = config.publicUrl || `${req.protocol}://${req.get('host')}`;
  res.json({
    version: require('../../package.json').version,
    endpoint: `${origin.replace(/\/+$/, '')}/v1`,
    capabilities: capabilities.list(),
  });
});
router.use('/admin', requireOwnSpace, require('./admin'));
router.use('/teams', requireOwnSpace, require('./teams'));
router.use('/providers', require('./providers'));
router.use('/models', require('./models'));
router.use('/keys', require('./keys'));
router.use('/routing', require('./routing'));
router.use('/usage', require('./usage'));
router.use('/playground', require('./playground'));
router.use('/playground', require('./playground_media'));
router.use('/processing', require('./processing'));

module.exports = router;
