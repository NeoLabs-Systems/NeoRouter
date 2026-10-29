'use strict';

// What an owner can let a team member do in the owner's space. Every
// create/edit/delete right brings the matching view right with it, so a
// member is never allowed to change what they cannot see.
const PERMISSIONS = Object.freeze([
  'activity.view',
  'playground.use',
  'keys.view',
  'keys.create',
  'keys.edit',
  'keys.delete',
  'providers.view',
  'providers.manage',
  'providers.delete',
  'models.manage',
  'routing.view',
  'routing.edit',
  'processing.view',
  'processing.edit',
]);
const PERMISSION_SET = new Set(PERMISSIONS);

const PRESETS = Object.freeze({
  view: ['activity.view', 'keys.view', 'providers.view', 'routing.view', 'processing.view'],
  keys: ['activity.view', 'playground.use', 'keys.view', 'keys.create', 'keys.edit', 'keys.delete', 'providers.view', 'routing.view'],
  full: [...PERMISSIONS],
});

function viewFor(permission) {
  const [area, action] = permission.split('.');
  if (action === 'view' || action === 'use') return null;
  if (area === 'models') return 'providers.view';
  return `${area}.view`;
}

function normalize(permissions) {
  const result = new Set();
  for (const permission of permissions || []) {
    if (!PERMISSION_SET.has(permission)) continue;
    result.add(permission);
    const view = viewFor(permission);
    if (view) result.add(view);
  }
  return PERMISSIONS.filter((permission) => result.has(permission));
}

function parse(json) {
  try {
    const parsed = JSON.parse(json || '[]');
    return Array.isArray(parsed) ? normalize(parsed) : [];
  } catch {
    return [];
  }
}

module.exports = { PERMISSIONS, PERMISSION_SET, PRESETS, normalize, parse };
