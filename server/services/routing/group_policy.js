'use strict';

const groups = require('./groups');

// Only the client knows whether this is a child request. A handoff happens
// once, before ranking, and never changes the key's permissions.
function resolve(userId, name, workload, checkAccess) {
  if (!name) return { group: null, alias: null };
  const alias = groups.requireGroup(userId, name);
  checkAccess(alias);
  if (workload !== 'subagent' || !alias.subagentGroup) return { group: alias, alias };
  const group = groups.requireGroup(userId, alias.subagentGroup);
  checkAccess(group);
  return { group, alias };
}

function allowsOutside(...groups) {
  return groups.every((group) => group?.allowOutside !== false);
}

function allowsMetered(...groups) {
  return groups.every((group) => group?.allowMetered !== false);
}

// An active conversation gets first use of its existing endpoint. New
// conversations start on the preferred account if it can serve.
function preference(group, entry, affinityRef) {
  if (!group?.preferredAccount) return 0;
  if (entry.ref === affinityRef) return 0;
  return entry.accountId === group.preferredAccount ? 1 : 2;
}

function reason(group, choice, ranked, affinityRef) {
  if (!group?.preferredAccount) return null;
  if (choice.entry.ref === affinityRef) return 'same_connection';
  if (choice.entry.accountId === group.preferredAccount) return 'preferred_connection';
  const skipped = ranked.filter((item) => item.entry.accountId === group.preferredAccount);
  if (!skipped.length) return null;
  if (skipped.some((item) => ['account_busy', 'slot_overflow'].includes(item.rejected))) return 'preferred_busy';
  if (skipped.some((item) => item.rejected === 'plan_used_up' || item.rejected?.startsWith('quota_'))) return 'preferred_allowance';
  return 'preferred_unavailable';
}

module.exports = { resolve, allowsOutside, allowsMetered, preference, reason };
