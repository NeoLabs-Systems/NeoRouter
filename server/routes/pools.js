'use strict';

// The pool page: start or join pools, invite, choose what each pool shares,
// pause, leave. Pools belong to a person's own space; team members of that
// space may look (to know whether pool keys are possible) but not change
// anything.
const express = require('express');
const { z } = require('zod');
const pools = require('../services/pools/pool_service');
const peerPrivacy = require('../services/pools/peer_privacy');
const { SHARE_BOUNDS, SHARE_KINDS } = require('../services/pools/share_policy');
const poolActivity = require('../services/pools/pool_activity');
const { validate } = require('../middleware/validate');
const { requireOwnSpace } = require('../middleware/space');
const { asyncRoute } = require('./helpers');
const { publicOrigin } = require('../utils/public_origin');

const router = express.Router();

// This NeoRouter's public address, as partners must reach it.
const owner = (req) => req.space.ownerId;

router.get('/', (req, res) => res.json(pools.summary(owner(req), { canManage: req.space.isOwner })));

router.use(requireOwnSpace);

const name = z.string().trim().min(1).max(60);
const accountIds = z.array(z.string().max(64)).max(200);

router.post('/', validate(z.object({ name, selfName: name, sharedAccountIds: accountIds.default([]) })), (req, res) => {
  res.status(201).json(pools.create(owner(req), { ...req.body, selfUrl: publicOrigin(req) }));
});

router.post('/preview', validate(z.object({ link: z.string().min(1).max(2048) })), asyncRoute(async (req, res) => {
  res.json(await pools.preview(owner(req), req.body.link));
}));

router.post('/join', validate(z.object({ link: z.string().min(1).max(2048), selfName: name, sharedAccountIds: accountIds.default([]) })), asyncRoute(async (req, res) => {
  res.json(await pools.join(owner(req), { ...req.body, selfUrl: publicOrigin(req) }));
}));

// "Keep very sensitive requests off pools" (account settings), for all pools.
router.put('/privacy', validate(z.object({ keepSensitivePrivate: z.boolean() }).strict()), (req, res) => {
  res.json({ privacy: peerPrivacy.saveSettings(owner(req), req.body) });
});

router.put('/partners/:linkId/paused', validate(z.object({ paused: z.boolean() })), asyncRoute(async (req, res) => {
  res.json(await pools.setPartnerPaused(owner(req), req.params.linkId, req.body.paused));
}));

// Everything below is about one pool.
const pool = express.Router({ mergeParams: true });
router.use('/:poolId', pool);
const poolId = (req) => req.params.poolId;

pool.post('/invites', (req, res) => res.status(201).json(pools.createInvite(owner(req), poolId(req))));
pool.delete('/invites/:id', (req, res) => {
  pools.revokeInvite(owner(req), poolId(req), req.params.id);
  res.status(204).end();
});

const bounded = (field, integer = true) => {
  const [min, max] = SHARE_BOUNDS[field];
  // null: back to automatic.
  return (integer ? z.number().int() : z.number()).min(min).max(max).nullable().optional();
};

pool.put('/share', validate(z.object({
  requestsPerDay: bounded('requestsPerDay'),
  tokensPerDay: bounded('tokensPerDay'),
  concurrent: bounded('concurrent'),
  reserve: bounded('reserve', false),
  sharedAccountIds: accountIds.optional(),
  shareKinds: z.record(z.array(z.enum(SHARE_KINDS)).max(3)).optional(),
}).strict()), asyncRoute(async (req, res) => res.json(await pools.updateShare(owner(req), poolId(req), req.body))));

// shared: on or off; kinds: which of free, plan (subscription) and paid it shares. Either or both.
pool.put('/accounts/:id/share', validate(z.object({ shared: z.boolean().optional(), kinds: z.array(z.enum(SHARE_KINDS)).max(3).optional() }).refine((body) => body.shared !== undefined || body.kinds !== undefined)), asyncRoute(async (req, res) => {
  res.json(await pools.setAccountShared(owner(req), poolId(req), req.params.id, req.body));
}));

// What partners used of ours and we of theirs, as counts by connection, model and partner.
pool.get('/activity', (req, res) => {
  pools.requirePool(owner(req), poolId(req));
  res.json(poolActivity.activity(owner(req), poolId(req), { days: req.query.days }));
});

// The quick switch: this pool on or off for now.
pool.put('/paused', validate(z.object({ paused: z.boolean() })), asyncRoute(async (req, res) => res.json(await pools.setPoolPaused(owner(req), poolId(req), req.body.paused))));

// The host's switch: pass traffic for members without a public address.
pool.put('/relay', validate(z.object({ enabled: z.boolean() })), (req, res) => res.json(pools.setRelayEnabled(owner(req), poolId(req), req.body.enabled)));

pool.delete('/members/:memberId', asyncRoute(async (req, res) => {
  await pools.removeMember(owner(req), poolId(req), req.params.memberId);
  res.json(pools.summary(owner(req)));
}));

pool.post('/leave', asyncRoute(async (req, res) => {
  await pools.leave(owner(req), poolId(req));
  res.json(pools.summary(owner(req)));
}));

pool.post('/sync', asyncRoute(async (req, res) => {
  await pools.sync(owner(req), poolId(req));
  res.json(pools.summary(owner(req)));
}));

module.exports = router;
