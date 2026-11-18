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

const PARTNER_PATHS = new Set(['/models', '/chat/completions']);

function requireRouterKey(req, _res, next) {
  const key = routerKeys.authenticate(extractToken(req));
  if (!key) return next(new HttpError(401, 'INVALID_API_KEY', 'A valid NeoRouter key (nrt_…) is required.'));
  // A pool partner's key reaches chat and the model list only (for now).
  if (key.peer_link_id && !PARTNER_PATHS.has(req.path)) {
    return next(new HttpError(403, 'POOL_CHAT_ONLY', 'Pool partners can only use chat models.'));
  }
  req.routerKey = key;
  return next();
}

module.exports = { requireRouterKey, extractToken };
