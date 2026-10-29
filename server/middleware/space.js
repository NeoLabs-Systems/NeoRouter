'use strict';

const members = require('../services/teams/members');
const { PERMISSIONS } = require('../services/teams/permissions');
const { HttpError } = require('./error_handler');

const HEADER = 'X-NeoRouter-Space';

// Decides whose data a request works on. Without the header, or with the
// caller's own id, it is their own space with every right. Otherwise the
// caller must be a member of that owner's team and gets only the rights the
// owner granted. A space the caller does not belong to looks like it does not
// exist.
function resolveSpace(req, _res, next) {
  const actorId = req.auth.userId;
  const requested = req.get(HEADER);
  if (!requested || requested === actorId) {
    req.space = { ownerId: actorId, actorId, isOwner: true, permissions: new Set(PERMISSIONS) };
    return next();
  }
  const current = members.membership(actorId);
  if (!current || current.ownerUserId !== requested) return next(new HttpError(404, 'SPACE_NOT_FOUND', 'This space does not exist.'));
  req.space = { ownerId: requested, actorId, isOwner: false, permissions: new Set(current.permissions) };
  return next();
}

function requirePermission(permission) {
  return (req, _res, next) => {
    if (req.space?.permissions.has(permission)) return next();
    return next(new HttpError(403, 'PERMISSION_DENIED', 'The team owner has not allowed this for you.', { permission }));
  };
}

// Team management, admin and account pages only ever act on the caller's own
// space.
function requireOwnSpace(req, _res, next) {
  if (req.space?.isOwner) return next();
  return next(new HttpError(403, 'OWN_SPACE_ONLY', 'Switch to your own space to do this.'));
}

module.exports = { HEADER, resolveSpace, requirePermission, requireOwnSpace };
