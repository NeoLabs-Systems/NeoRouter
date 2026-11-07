'use strict';

// What other NeoRouters call to join a pool and keep it in order. Nothing here
// uses a session: an invite token, a ticket signed by the pool's host, or a
// signature by a known member authorises each call (services/pools). Bodies
// are small and calls are rate-limited per address.
const express = require('express');
const pools = require('../services/pools/pool_service');
const { slidingWindow } = require('../middleware/rate_limit');
const { asyncRoute } = require('./helpers');

const router = express.Router();

const limiter = slidingWindow({ windowMs: 60_000, limit: 60, key: (req) => req.ip, code: 'POOL_RATE_LIMITED' });
router.use(limiter);
router.use(express.json({ limit: '64kb' }));

const handlers = {
  preview: pools.handlePreview,
  join: pools.handleJoin,
  introduce: pools.handleIntroduce,
  members: pools.handleMemberList,
  ticket: pools.handleTicket,
  leave: pools.handleLeave,
  remove: pools.handleRemove,
  dissolve: pools.handleDissolve,
  shared_changed: pools.handleSharedChanged,
};

for (const [path, handler] of Object.entries(handlers)) {
  router.post(`/${path}`, asyncRoute(async (req, res) => res.json(await handler(req.body || {}))));
}

module.exports = router;
module.exports.limiter = limiter;
