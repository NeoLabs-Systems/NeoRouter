'use strict';

// The pool page: create or join a pool, invite, choose what to share, pause,
// leave. Pools belong to a person's own space; team members of that space may
// look (to know whether pool keys are possible) but not change anything.
const express = require('express');
const { z } = require('zod');
const pools = require('../services/pools/pool_service');
const peerPrivacy = require('../services/pools/peer_privacy');
const { SHARE_BOUNDS } = require('../services/pools/share_policy');
const { validate } = require('../middleware/validate');
const { requireOwnSpace } = require('../middleware/space');
const { getConfig } = require('../config/env');
const { asyncRoute } = require('./helpers');

const router = express.Router();

// This NeoRouter's public address, as partners must reach it.
const selfUrl = (req) => getConfig().publicUrl || `${req.protocol}://${req.get('host')}`;
const owner = (req) => req.space.ownerId;

router.get('/', (req, res) => res.json(pools.summary(owner(req), { canManage: req.space.isOwner })));

router.use(requireOwnSpace);

const name = z.string().trim().min(1).max(60);

router.post('/', validate(z.object({ name, selfName: name, sharedAccountIds: z.array(z.string().max(64)).max(200).default([]) })), (req, res) => {
  res.status(201).json(pools.create(owner(req), { ...req.body, selfUrl: selfUrl(req) }));
});

router.post('/preview', validate(z.object({ link: z.string().min(1).max(2048) })), asyncRoute(async (req, res) => {
  res.json(await pools.preview(owner(req), req.body.link));
}));

router.post('/join', validate(z.object({
  link: z.string().min(1).max(2048),
  selfName: name,
  sharedAccountIds: z.array(z.string().max(64)).max(200).default([]),
})), asyncRoute(async (req, res) => {
  res.json(await pools.join(owner(req), { ...req.body, selfUrl: selfUrl(req) }));
}));

router.post('/invites', (req, res) => res.status(201).json(pools.createInvite(owner(req))));
router.delete('/invites/:id', (req, res) => {
  pools.revokeInvite(owner(req), req.params.id);
  res.status(204).end();
});

const bounded = (field, integer = true) => {
  const [min, max] = SHARE_BOUNDS[field];
  return (integer ? z.number().int() : z.number()).min(min).max(max).optional();
};

router.put('/share', validate(z.object({
  requestsPerDay: bounded('requestsPerDay'),
  tokensPerDay: bounded('tokensPerDay'),
  concurrent: bounded('concurrent'),
  reserve: bounded('reserve', false),
  sharedAccountIds: z.array(z.string().max(64)).max(200).optional(),
}).strict()), asyncRoute(async (req, res) => res.json(await pools.updateShare(owner(req), req.body))));

// "Share in pool" in a connection's menu.
router.put('/accounts/:id/share', validate(z.object({ shared: z.boolean() })), asyncRoute(async (req, res) => {
  res.json(await pools.setAccountShared(owner(req), req.params.id, req.body.shared));
}));

// The quick switch: the whole pool on or off for now.
router.put('/paused', validate(z.object({ paused: z.boolean() })), asyncRoute(async (req, res) => res.json(await pools.setPoolPaused(owner(req), req.body.paused))));

router.put('/partners/:linkId/paused', validate(z.object({ paused: z.boolean() })), asyncRoute(async (req, res) => {
  res.json(await pools.setPartnerPaused(owner(req), req.params.linkId, req.body.paused));
}));

router.delete('/members/:memberId', asyncRoute(async (req, res) => {
  await pools.removeMember(owner(req), req.params.memberId);
  res.json(pools.summary(owner(req)));
}));

router.post('/leave', asyncRoute(async (req, res) => {
  await pools.leave(owner(req));
  res.json(pools.summary(owner(req)));
}));

router.post('/sync', asyncRoute(async (req, res) => {
  await pools.sync(owner(req));
  res.json(pools.summary(owner(req)));
}));

// "Keep very sensitive requests off the pool" (account settings).
router.put('/privacy', validate(z.object({ keepSensitivePrivate: z.boolean() }).strict()), (req, res) => {
  res.json({ privacy: peerPrivacy.saveSettings(owner(req), req.body) });
});

module.exports = router;
