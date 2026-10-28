'use strict';

const routerKeys = require('../services/router_keys/router_key_service');
const { HttpError } = require('./error_handler');

// Gateway clients send the router key the way their SDK does: OpenAI uses
// Authorization: Bearer, Anthropic uses x-api-key, Gemini tooling x-goog-api-key.
function extractToken(req) {
  const authorization = req.get('Authorization') || '';
  if (authorization.startsWith('Bearer ')) return authorization.slice(7).trim();
  return (req.get('x-api-key') || req.get('x-goog-api-key') || '').trim() || null;
}

function requireRouterKey(req, _res, next) {
  const key = routerKeys.authenticate(extractToken(req));
  if (!key) return next(new HttpError(401, 'INVALID_API_KEY', 'A valid NeoRouter key (nrt_…) is required.'));
  req.routerKey = key;
  return next();
}

module.exports = { requireRouterKey, extractToken };
