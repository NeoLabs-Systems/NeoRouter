'use strict';

const crypto = require('node:crypto');
const { z } = require('zod');
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const teamMembers = require('../teams/members');

// Weights are relative; scoring normalizes them. Reliability counts in every
// preset, and among models of similar quality the router takes the cheaper
// one anyway (router.js), so three presets cover it.
const PRESETS = Object.freeze({
  balanced: { label: 'Balanced', weights: { intelligence: 0.35, speed: 0.2, reliability: 0.3, cost: 0.15 } },
  fast: { label: 'Fastest', weights: { intelligence: 0.15, speed: 0.5, reliability: 0.3, cost: 0.05 } },
  smart: { label: 'Smartest', weights: { intelligence: 0.6, speed: 0.05, reliability: 0.3, cost: 0.05 } },
});
// Earlier presets, still accepted in keys, model names (auto:best) and apps.
const ALIASES = Object.freeze({ reliable: 'balanced', cheap: 'balanced', best: 'smart' });

const DEFAULT_CONSTRAINTS = Object.freeze({
  minIntelligence: null,
  maxPriceOut: null,
  maxTtftMs: null,
  includeProviders: [],
  excludeProviders: [],
  excludeModels: [],
  adaptive: true,
  exploration: 0.08,
  // Custom styles: how much each kind of model counts (0 = never used).
  // Null means every kind the key allows, equally.
  kinds: null,
});

const weightsSchema = z.object({
  intelligence: z.number().min(0).max(1),
  speed: z.number().min(0).max(1),
  reliability: z.number().min(0).max(1),
  cost: z.number().min(0).max(1),
}).refine((value) => value.intelligence + value.speed + value.reliability + value.cost > 0, 'At least one weight must be above zero.');

const constraintsSchema = z.object({
  minIntelligence: z.number().min(0).max(1).nullable().optional(),
  maxPriceOut: z.number().min(0).nullable().optional(),
  maxTtftMs: z.number().int().positive().nullable().optional(),
  includeProviders: z.array(z.string()).optional(),
  excludeProviders: z.array(z.string()).optional(),
  excludeModels: z.array(z.string()).optional(),
  adaptive: z.boolean().optional(),
  exploration: z.number().min(0).max(1).optional(),
  kinds: z.object({ free: z.number().min(0).max(1), plan: z.number().min(0).max(1), paid: z.number().min(0).max(1) })
    .refine((value) => value.free + value.plan + value.paid > 0, 'At least one kind of model must count.')
    .nullable().optional(),
});

function rowToProfile(row) {
  return {
    id: row.id,
    name: row.name,
    preset: false,
    weights: JSON.parse(row.weights_json),
    constraints: { ...DEFAULT_CONSTRAINTS, ...JSON.parse(row.constraints_json || '{}') },
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    createdBy: teamMembers.publicUser(row.created_by_user_id),
  };
}

// Names reserved for presets, aliases and plain auto.
const RESERVED_SLUGS = new Set([...Object.keys(PRESETS), ...Object.keys(ALIASES), 'default', 'custom']);

function slugify(name) {
  return String(name || '').toLowerCase().normalize('NFKD').replace(/[\u0300-\u036f]/g, '')
    .replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40) || 'style';
}

// A user's own styles, each with the readable name used in model ids
// ("auto:paid-first"); clashes with presets or each other get a number.
function customProfiles(userId) {
  const taken = new Set(RESERVED_SLUGS);
  return getDatabase().prepare('SELECT * FROM routing_profiles WHERE user_id = ? ORDER BY created_at').all(userId).map((row) => {
    const base = slugify(row.name);
    let slug = taken.has(base) ? `${base}-2` : base;
    for (let index = 3; taken.has(slug); index += 1) slug = `${base}-${index}`;
    taken.add(slug);
    return { ...rowToProfile(row), slug };
  });
}

function list(userId) {
  const presets = Object.entries(PRESETS).map(([id, preset]) => ({ id, slug: id, name: preset.label, preset: true, weights: preset.weights, constraints: { ...DEFAULT_CONSTRAINTS } }));
  return [...presets, ...customProfiles(userId)];
}

// Resolves "balanced", "fast", … or one of the user's own styles by its
// readable name ("paid-first") or its id.
function resolve(userId, reference) {
  const key = ALIASES[reference] || String(reference || 'balanced');
  if (PRESETS[key]) return { id: key, slug: key, name: PRESETS[key].label, preset: true, weights: PRESETS[key].weights, constraints: { ...DEFAULT_CONSTRAINTS } };
  return customProfiles(userId).find((profile) => profile.id === key || profile.slug === key) || null;
}

function create(userId, { name, weights, constraints = {} }, createdBy = userId) {
  const parsedWeights = weightsSchema.parse(weights);
  const parsedConstraints = constraintsSchema.parse(constraints);
  const id = `custom_${crypto.randomBytes(6).toString('hex')}`;
  getDatabase().prepare('INSERT INTO routing_profiles (id, user_id, name, weights_json, constraints_json, created_by_user_id) VALUES (?, ?, ?, ?, ?, ?)')
    .run(id, userId, name, JSON.stringify(parsedWeights), JSON.stringify(parsedConstraints), createdBy);
  return resolve(userId, id);
}

function update(userId, id, { name, weights, constraints }) {
  const existing = resolve(userId, id);
  if (!existing || existing.preset) throw new HttpError(404, 'PROFILE_NOT_FOUND', 'This routing profile does not exist or is a built-in preset.');
  getDatabase().prepare(`UPDATE routing_profiles SET name = ?, weights_json = ?, constraints_json = ?,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ? AND user_id = ?`)
    .run(name ?? existing.name, JSON.stringify(weights ? weightsSchema.parse(weights) : existing.weights),
      JSON.stringify(constraints ? constraintsSchema.parse(constraints) : existing.constraints), id, userId);
  return resolve(userId, id);
}

function remove(userId, id) {
  const result = getDatabase().prepare('DELETE FROM routing_profiles WHERE id = ? AND user_id = ?').run(id, userId);
  if (!result.changes) throw new HttpError(404, 'PROFILE_NOT_FOUND', 'This routing profile does not exist.');
  getDatabase().prepare("UPDATE router_keys SET profile = 'balanced' WHERE user_id = ? AND profile = ?").run(userId, id);
}

module.exports = { PRESETS, ALIASES, DEFAULT_CONSTRAINTS, list, resolve, create, update, remove, weightsSchema, constraintsSchema };
