'use strict';

function cors(req, res, next) {
  const origin = req.get('Origin');
  const configured = (process.env.NEOROUTER_ALLOWED_ORIGINS || '').split(',').map((value) => value.trim()).filter(Boolean);
  const allow = !origin
    || configured.includes(origin)
    || configured.includes('*')
    || /^https?:\/\/(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$/.test(origin);
  if (origin && allow) {
    res.set('Access-Control-Allow-Origin', origin);
    res.set('Vary', 'Origin');
    res.set('Access-Control-Expose-Headers', 'X-Request-Id,X-NeoRouter-Model,X-NeoRouter-Trail');
    res.set('Access-Control-Allow-Headers', 'Authorization,Content-Type,X-Api-Key,Anthropic-Version,Anthropic-Beta,OpenAI-Organization,OpenAI-Beta,X-Request-Id,X-NeoRouter-Space');
    res.set('Access-Control-Allow-Methods', 'GET,POST,PUT,PATCH,DELETE,OPTIONS');
    res.set('Access-Control-Allow-Credentials', 'true');
  }
  if (req.method === 'OPTIONS') return res.status(204).end();
  return next();
}

module.exports = { cors };
