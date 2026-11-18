'use strict';

const crypto = require('node:crypto');
const { z } = require('zod');
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const profiles = require('./profiles');

const schema = z.object({
  name: z.string().regex(/^[a-z0-9][a-z0-9_-]{0,79}$/),
  accounts: z.array(z.string().min(1)).max(200).default([]),
  models: z.array(z.string().min(1).max(300)).max(200).default([]),
  profile: z.string().max(80).nullable().default(null),
  minIntelligence: z.number().min(0).max(1).nullable().default(null),
  fallback: z.string().max(80).nullable().default(null),
  subagentGroup: z.string().max(80).nullable().optional(),
  preferredAccount: z.string().min(1).nullable().optional(),
  allowOutside: z.boolean().optional(),
  allowMetered: z.boolean().optional(),
}).refine((value) => value.accounts.length + value.models.length > 0, 'Choose at least one account or model.');

function list(userId) {
  return getDatabase().prepare('SELECT * FROM routing_groups WHERE user_id = ? ORDER BY name').all(userId)
    .map((row) => ({ id: row.id, ...JSON.parse(row.config_json) }));
}
function resolve(userId, name) {
  return list(userId).find((group) => group.name === name || group.id === name) || null;
}
function requireGroup(userId, name) {
  const group = resolve(userId, name);
  if (!group) throw new HttpError(400, 'UNKNOWN_ROUTING_GROUP', `Unknown routing group "${name}".`);
  return group;
}
function save(userId, input, id = null) {
  const config = schema.parse(input);
  const all = list(userId);
  if (id && !all.some((group) => group.id === id)) throw new HttpError(404, 'GROUP_NOT_FOUND', 'Routing group does not exist.');
  if (all.some((group) => group.name === config.name && group.id !== id)) throw new HttpError(409, 'GROUP_NAME_TAKEN', 'Routing group name is already used.');
  if (config.profile) {
    const profile = profiles.resolve(userId, config.profile);
    if (!profile) throw new HttpError(400, 'UNKNOWN_PROFILE', 'Routing style does not exist.');
    config.profile = profile.id;
  }
  const previous = all.find((group) => group.id === id);
  if (previous && previous.name !== config.name) throw new HttpError(400, 'GROUP_NAME_IMMUTABLE', 'Create a new group to change its routing name.');
  config.allowOutside ??= previous ? previous.allowOutside !== false : false;
  config.allowMetered ??= previous ? previous.allowMetered !== false : false;
  if (config.preferredAccount === undefined) config.preferredAccount = previous?.preferredAccount ?? null;
  if (config.subagentGroup === undefined) config.subagentGroup = previous?.subagentGroup ?? null;
  if (config.preferredAccount && !config.accounts.includes(config.preferredAccount)) {
    throw new HttpError(400, 'UNKNOWN_PREFERRED_ACCOUNT', 'Choose a preferred connection from the selected connections.');
  }
  if (config.fallback) config.fallback = requireGroup(userId, config.fallback).name;
  if (config.subagentGroup) config.subagentGroup = requireGroup(userId, config.subagentGroup).name;
  for (const account of config.accounts) {
    if (!getDatabase().prepare('SELECT 1 FROM provider_accounts WHERE id = ? AND user_id = ?').get(account, userId)) {
      throw new HttpError(400, 'UNKNOWN_ACCOUNT', 'Routing group account does not belong to this space.');
    }
  }
  const seen = new Set();
  let next = config;
  while (next) {
    if (seen.has(next.name)) throw new HttpError(400, 'GROUP_CYCLE', 'Fallback groups must not form a cycle.');
    seen.add(next.name);
    next = next.fallback ? requireGroup(userId, next.fallback) : null;
    if (next?.name === config.name) next = config;
  }
  if (config.subagentGroup === config.name) {
    throw new HttpError(400, 'GROUP_CYCLE', 'Choose a different group for subagents.');
  }
  id ||= crypto.randomUUID();
  getDatabase().prepare('INSERT INTO routing_groups (id, user_id, name, config_json) VALUES (?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET config_json = excluded.config_json')
    .run(id, userId, config.name, JSON.stringify(config));
  return resolve(userId, id);
}
function remove(userId, id) {
  const group = requireGroup(userId, id);
  if (list(userId).some((item) => item.fallback === group.name || item.fallback === group.id)) throw new HttpError(409, 'GROUP_IN_USE', 'Another group uses this fallback.');
  if (list(userId).some((item) => item.subagentGroup === group.name)) throw new HttpError(409, 'GROUP_IN_USE', 'Another group uses this group for subagents.');
  getDatabase().transaction(() => {
    getDatabase().prepare('UPDATE router_keys SET routing_group = NULL WHERE user_id = ? AND routing_group IN (?, ?)').run(userId, group.id, group.name);
    getDatabase().prepare('DELETE FROM routing_groups WHERE id = ? AND user_id = ?').run(group.id, userId);
  })();
}
function status(userId) {
  const entries = require('../catalog/catalog_service').listForUser(userId, { includeUnavailable: true });
  const plans = require('./plans');
  const labels = new Map(getDatabase().prepare('SELECT id, label FROM provider_accounts WHERE user_id = ?').all(userId).map((row) => [row.id, row.label]));
  return list(userId).map((group) => {
    const accounts = new Map();
    for (const entry of entries) if (entry.isPlan && contains(group, entry) && !accounts.has(entry.accountId)) {
      const room = plans.room(entry);
      accounts.set(entry.accountId, { accountId: entry.accountId, name: labels.get(entry.accountId) || entry.providerName || entry.providerSlug, usedShare: room ? 1 - room.share : null, resetAt: room?.resetAt || null });
    }
    return { ...group, plans: [...accounts.values()] };
  });
}

// Membership is a union: an account includes all its models; model patterns
// can also include models across accounts. Key policy still applies separately.
function contains(group, entry) {
  const { globToRegExp } = require('./router');
  return group.accounts.includes(entry.accountId) || group.models.some((pattern) => globToRegExp(pattern).test(entry.id));
}
module.exports = { schema, list, status, resolve, requireGroup, save, remove, contains };
