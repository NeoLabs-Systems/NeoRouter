'use strict';

const express = require('express');
const { z } = require('zod');
const auth = require('../services/auth/auth_service');
const { requireSession } = require('../middleware/auth');
const { validate } = require('../middleware/validate');
const { slidingWindow } = require('../middleware/rate_limit');
const { asyncRoute, context } = require('./helpers');

const router = express.Router();

router.get('/registration', (_req, res) => res.json(auth.registrationStatus()));

router.post('/register', slidingWindow({ windowMs: 60_000, limit: 10, key: (req) => req.ip }), validate(z.object({
  username: z.string().min(3).max(64),
  email: z.string().email(),
  password: z.string().min(12).max(1024),
  displayName: z.string().max(80).optional(),
})), asyncRoute(async (req, res) => res.status(201).json(await auth.register(req.body, context(req)))));

router.post('/login', slidingWindow({ windowMs: 60_000, limit: 10, key: (req) => req.ip }), validate(z.object({
  account: z.string().min(1).max(320),
  password: z.string().min(1).max(1024),
})), asyncRoute(async (req, res) => res.json(await auth.login(req.body, context(req)))));

router.use(requireSession);
router.get('/me', (req, res) => res.json({ user: req.user }));
router.post('/logout', (req, res) => { auth.logout(req.auth.sessionId); res.status(204).end(); });
router.put('/password', validate(z.object({ currentPassword: z.string(), newPassword: z.string().min(12).max(1024) })),
  asyncRoute(async (req, res) => { await auth.changePassword(req.auth.userId, req.body.currentPassword, req.body.newPassword); res.status(204).end(); }));
router.patch('/profile', validate(z.object({ displayName: z.string().max(80).optional(), email: z.string().email().optional() })),
  (req, res) => res.json({ user: auth.updateProfile(req.auth.userId, req.body) }));

module.exports = router;
