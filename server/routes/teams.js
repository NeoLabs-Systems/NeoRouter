'use strict';

const express = require('express');
const { z } = require('zod');
const invites = require('../services/teams/invites');
const members = require('../services/teams/members');
const summary = require('../services/teams/summary');
const { PERMISSIONS } = require('../services/teams/permissions');
const { getConfig } = require('../config/env');
const { validate } = require('../middleware/validate');
const { slidingWindow } = require('../middleware/rate_limit');

const router = express.Router();

const inviteLimit = slidingWindow({ windowMs: 15 * 60_000, limit: 20 });
const changeLimit = slidingWindow({ windowMs: 15 * 60_000, limit: 120 });
const permissionList = z.array(z.enum(PERMISSIONS)).max(PERMISSIONS.length);
const linkInput = z.object({ link: z.string().min(1).max(2048) });

router.get('/', (req, res) => res.json(summary.build(req.auth.userId)));

router.post('/invites/preview', inviteLimit, validate(linkInput), (req, res) => res.json(invites.preview(req.auth.userId, req.body.link)));

router.post('/invites/redeem', inviteLimit, validate(linkInput), (req, res) => {
  invites.redeem(req.auth.userId, req.body.link);
  res.json(summary.build(req.auth.userId));
});

router.post('/invites', changeLimit, validate(z.object({
  label: z.string().max(80).optional(),
  permissions: permissionList,
  expiresInHours: z.number().int().min(1).max(24 * 90).nullable().optional(),
  singleUse: z.boolean().optional(),
})), (req, res) => {
  const { invite, token } = invites.create(req.auth.userId, req.body);
  const origin = getConfig().publicUrl || `${req.protocol}://${req.get('host')}`;
  res.status(201).json({ invite, link: invites.buildLink(origin, token) });
});

router.delete('/invites/:id', changeLimit, (req, res) => {
  invites.revoke(req.auth.userId, req.params.id);
  res.json(summary.build(req.auth.userId));
});

router.put('/members/:userId/permissions', changeLimit, validate(z.object({ permissions: permissionList })), (req, res) => {
  members.setPermissions(req.auth.userId, req.params.userId, req.body.permissions);
  res.json(summary.build(req.auth.userId));
});

router.delete('/members/:userId', changeLimit, (req, res) => {
  members.release(req.auth.userId, req.params.userId);
  res.json(summary.build(req.auth.userId));
});

router.post('/leave', changeLimit, (req, res) => {
  members.leave(req.auth.userId);
  res.json(summary.build(req.auth.userId));
});

module.exports = router;
