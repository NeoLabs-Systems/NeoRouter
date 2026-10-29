'use strict';

const invites = require('./invites');
const members = require('./members');
const permissions = require('./permissions');

// Everything the Team page needs for one account: whose team it is in (if
// any), its own members and invite links, and the permission catalog.
function build(userId) {
  const current = members.membership(userId);
  return {
    memberOf: current ? { owner: members.publicUser(current.ownerUserId), permissions: current.permissions, joinedAt: current.createdAt } : null,
    members: members.list(userId),
    invites: invites.list(userId),
    permissions: permissions.PERMISSIONS,
    presets: permissions.PRESETS,
  };
}

module.exports = { build };
