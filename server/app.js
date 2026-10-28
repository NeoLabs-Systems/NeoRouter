'use strict';

const fs = require('node:fs');
const path = require('node:path');
const express = require('express');
const compression = require('compression');
const { migrate } = require('./db/migrate');
const { getDatabase } = require('./db/database');
const { getConfig } = require('./config/env');
const { requestId } = require('./middleware/request_id');
const { cors } = require('./middleware/cors');
const { notFound, errorHandler, HttpError } = require('./middleware/error_handler');
const capabilities = require('./core/capability_registry');
const { WEB_CLIENT_DIR, LANDING_DIR } = require('../runtime/paths');

function hasBundledWebClient(dir) {
  return fs.existsSync(path.join(dir, 'index.html'));
}

function createApp() {
  migrate();
  const app = express();
  const config = getConfig();
  if (config.trustProxy) app.set('trust proxy', 1);
  app.disable('x-powered-by');
  app.use(requestId, cors);

  app.get('/health', (_req, res) => {
    res.json({ status: 'ok', process: 'http', version: require('../package.json').version });
  });
  app.get('/ready', (_req, res, next) => {
    try {
      getDatabase().prepare('SELECT 1').get();
      res.json({ status: 'ready', database: true });
    } catch (error) {
      next(error);
    }
  });

  // The gateway streams responses, so it is mounted before compression and
  // gets its own, larger body limit for image inputs.
  const gateway = express.Router();
  gateway.use(express.json({ limit: config.maxBodyBytes }));
  // Body-parser failures happen before any route, so answer them here in the
  // OpenAI error shape every gateway SDK understands.
  gateway.use((error, req, res, next) => {
    if (error?.type === 'entity.parse.failed') return res.status(400).json({ error: { message: 'The request body is not valid JSON.', type: 'invalid_request_error', code: 'INVALID_JSON' } });
    if (error?.type === 'entity.too.large') return res.status(413).json({ error: { message: 'The request body is too large.', type: 'invalid_request_error', code: 'BODY_TOO_LARGE' } });
    return next(error);
  });
  capabilities.mount(gateway);
  app.use('/v1', gateway);

  app.use(compression());
  app.use(express.json({ limit: '2mb' }));
  app.use('/api/v1', require('./routes'));

  // Flutter web and landing files keep the same names across releases (the
  // icon font is rebuilt with only the icons in use), so browsers and CDNs
  // must revalidate them; ETags keep that cheap.
  const revalidate = (res) => res.setHeader('Cache-Control', 'private, no-cache');
  const appWebRoot = WEB_CLIENT_DIR;
  app.use('/app', express.static(appWebRoot, { extensions: ['html'], fallthrough: true, setHeaders: revalidate }));
  app.use('/app', (req, res, next) => {
    // During development the Flutter app runs on its own dev server.
    if (!hasBundledWebClient(appWebRoot) && config.webDevUrl) return res.redirect(config.webDevUrl);
    if (!hasBundledWebClient(appWebRoot)) {
      return next(new HttpError(503, 'WEB_UI_MISSING', 'NeoRouter web UI assets are missing. Run `neorouter rebuild-web`.'));
    }
    if (req.method !== 'GET' && req.method !== 'HEAD') return next();
    revalidate(res);
    return res.sendFile(path.join(appWebRoot, 'index.html'), (error) => next(error));
  });

  app.use('/', express.static(LANDING_DIR, { extensions: ['html'], setHeaders: revalidate }));
  app.use(notFound, errorHandler);
  return app;
}

module.exports = { createApp };
