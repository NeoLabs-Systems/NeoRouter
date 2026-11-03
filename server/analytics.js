'use strict';

const fs = require('node:fs');
const path = require('node:path');

const WEBSITE_ID = 'bc14ddd4-6e6a-4bfc-abb2-235b0eae72b1';

function analyticsEnabled() {
  return !['false', '0', 'off', 'no'].includes(String(process.env.ANALYTICS_ENABLED || '').trim().toLowerCase());
}

function registerAnalytics(app, { appWebRoot, landingRoot }) {
  app.get('/_neolabs/analytics.js', (_req, res, next) => {
    if (!analyticsEnabled()) return next();
    res.set('Cache-Control', 'no-store');
    res.type('application/javascript').send(fs.readFileSync(path.join(__dirname, 'analytics-client.js'), 'utf8').replace('__WEBSITE_ID__', WEBSITE_ID));
  });

  app.use((req, res, next) => {
    if (!analyticsEnabled() || (req.method !== 'GET' && req.method !== 'HEAD')) return next();
    const appPage = /^\/app(?:\/.*)?$/.test(req.path) && !/\.[a-z0-9]+$/i.test(req.path.replace(/\/index\.html$/i, ''));
    const landingPage = req.path === '/' || req.path === '/index.html';
    if (!appPage && !landingPage) return next();
    const entry = path.join(appPage ? appWebRoot : landingRoot, 'index.html');
    let html;
    try { html = fs.readFileSync(entry, 'utf8'); } catch (error) {
      if (error.code === 'ENOENT') return next();
      return next(error);
    }
    if (appPage) html = html.replace('<base href="/">', '<base href="/app/">');
    html = html.replace(/<\/head>/i, '<script defer src="/_neolabs/analytics.js"></script>\n</head>');
    // Like every web file (app.js): always revalidated, and the ETag send() adds
    // keeps that cheap while still catching a new release or a changed setting.
    res.set('Cache-Control', 'private, no-cache');
    return res.type('html').send(html);
  });
}

module.exports = { analyticsEnabled, registerAnalytics, WEBSITE_ID };
