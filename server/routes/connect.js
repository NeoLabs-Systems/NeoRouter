'use strict';

const express = require('express');
const { z } = require('zod');
const apps = require('../services/oauth/app_authorization');
const { getConfig } = require('../config/env');
const { HttpError } = require('../middleware/error_handler');

// The public OAuth endpoints apps talk to ("Connect with NeoRouter"). The
// approval itself happens in the web app (routes/oauth.js, with a session).
const router = express.Router();

const originOf = (req) => (getConfig().publicUrl || `${req.protocol}://${req.get('host')}`).replace(/\/+$/, '');

// Token requests come from apps on any origin and carry no cookies.
function openCors(req, res, next) {
  res.set('Access-Control-Allow-Origin', '*');
  res.set('Access-Control-Allow-Headers', 'Content-Type');
  res.set('Access-Control-Allow-Methods', 'GET,POST,OPTIONS');
  res.set('Cache-Control', 'no-store');
  if (req.method === 'OPTIONS') return res.status(204).end();
  return next();
}

// OAuth errors use the RFC 6749 shape apps' OAuth libraries expect.
function oauthErrors(error, _req, res, next) {
  if (error.oauth) return res.status(error.status || 400).json(error.oauth);
  return next(error);
}

router.get('/.well-known/oauth-authorization-server', openCors, (req, res) => res.json(apps.metadata(originOf(req))));

router.get('/oauth/authorize', (req, res, next) => {
  try {
    const result = apps.startAuthorization(req.query);
    res.redirect(result.redirect || `/app/?connect=${encodeURIComponent(result.requestId)}`);
  } catch (error) {
    // No trustworthy redirect_uri: show the problem in the app instead.
    if (error.oauth) return res.redirect(`/app/?connect_error=${encodeURIComponent(error.oauth.error_description)}`);
    return next(error);
  }
});

const body = [express.urlencoded({ extended: false, limit: '16kb' }), express.json({ limit: '16kb' })];
router.use(['/oauth/token', '/oauth/device/code'], openCors);
router.post('/oauth/device/code', body, (req, res) => res.json(apps.startDevice(req.body || {}, originOf(req))));
router.post('/oauth/token', body, (req, res) => res.json(apps.exchange(req.body || {}, originOf(req))));
router.use(oauthErrors);

// Session routes for the approval screen, mounted under /api/v1/oauth/apps.
const approval = express.Router();
const access = z.object({
  allowFree: z.boolean(),
  allowPlan: z.boolean(),
  allowPaid: z.boolean(),
  budgetUsd: z.number().min(0).nullable().optional(),
  budgetPeriod: z.enum(['total', 'monthly', 'daily']).optional(),
});

approval.get('/requests/:reference', (req, res) => res.json({ request: apps.describe(req.params.reference) }));
approval.post('/requests/:reference/approve', (req, res) => {
  const parsed = access.safeParse(req.body || {});
  if (!parsed.success) throw new HttpError(400, 'VALIDATION_ERROR', 'Choose what the app may use.');
  res.json(apps.approve(req.params.reference, { ownerId: req.space.ownerId, actorId: req.space.actorId, access: parsed.data }));
});
approval.post('/requests/:reference/deny', (req, res) => res.json(apps.deny(req.params.reference)));

module.exports = { router, approval };
