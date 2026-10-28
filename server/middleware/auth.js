'use strict';

const authService = require('../services/auth/auth_service');
const { HttpError } = require('./error_handler');

function bearer(req) {
  const value = req.get('Authorization') || '';
  return value.startsWith('Bearer ') ? value.slice(7).trim() : null;
}

function requireSession(req, _res, next) {
  const auth = authService.authenticateToken(bearer(req));
  if (!auth) return next(new HttpError(401, 'AUTHENTICATION_REQUIRED', 'A valid session is required.'));
  req.auth = auth;
  req.user = auth.user;
  return next();
}

function requireAdmin(req, _res, next) {
  if (req.user?.role === 'admin') return next();
  return next(new HttpError(403, 'ADMIN_REQUIRED', 'Administrator privileges are required.'));
}

module.exports = { bearer, requireSession, requireAdmin };
