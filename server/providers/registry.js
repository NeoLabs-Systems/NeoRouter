'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { z } = require('zod');
const { OpenAICompatibleAdapter } = require('./adapters/openai_compatible');
const { AnthropicAdapter } = require('./adapters/anthropic');
const { GoogleGeminiAdapter } = require('./adapters/google_gemini');
const { CloudflareAdapter } = require('./adapters/cloudflare');

const ADAPTERS = {
  openai_compatible: OpenAICompatibleAdapter,
  anthropic: AnthropicAdapter,
  google_gemini: GoogleGeminiAdapter,
  cloudflare: CloudflareAdapter,
};

const limitSchema = z.object({
  rpm: z.number().int().positive().optional(),
  rpd: z.number().int().positive().optional(),
  tpm: z.number().int().positive().optional(),
  tpd: z.number().int().positive().optional(),
});

const definitionSchema = z.object({
  id: z.string().regex(/^[a-z0-9][a-z0-9_-]*$/),
  label: z.string().min(1),
  description: z.string().default(''),
  category: z.enum(['free', 'paid', 'aggregator', 'local', 'custom', 'community']),
  website: z.string().url().optional(),
  keyUrl: z.string().url().optional(),
  adapter: z.enum(Object.keys(ADAPTERS)),
  capabilities: z.array(z.enum(['chat'])).default(['chat']),
  baseUrl: z.string().default(''),
  editableBaseUrl: z.boolean().default(false),
  auth: z.object({
    type: z.enum(['bearer', 'header', 'none']).default('bearer'),
    header: z.string().optional(),
    anonymousKey: z.string().optional(),
  }).default({ type: 'bearer' }),
  keyOptional: z.boolean().default(false),
  fields: z.array(z.object({ key: z.string(), label: z.string(), required: z.boolean().default(true), placeholder: z.string().optional() })).default([]),
  headers: z.record(z.string()).default({}),
  chatPath: z.string().optional(),
  models: z.object({ path: z.string().optional(), static: z.array(z.any()).optional() }).default({}),
  validate: z.union([z.literal(false), z.object({ path: z.string().optional(), chatProbe: z.boolean().optional() })]).optional(),
  billing: z.object({
    defaultMode: z.enum(['free_tier', 'paid']),
    // With billing_mode=free_tier the account has no payment method, so
    // nothing it can call is ever charged.
    freeTierAll: z.boolean().default(false),
    // Models that are free on this provider regardless of billing mode.
    alwaysFree: z.array(z.string()).default([]),
    // Ignore "price 0" in the listing (some providers report 0 for unpriced models).
    trustListingFree: z.boolean().default(true),
    note: z.string().default(''),
  }),
  limits: z.object({
    pool: z.enum(['account', 'model']).default('model'),
    account: limitSchema.default({}),
    defaults: limitSchema.default({}),
    models: z.array(limitSchema.extend({ match: z.string() })).default([]),
    poolMatch: z.string().optional(),
  }).default({}),
  quota: z.object({
    headerPeriods: z.object({ requests: z.enum(['minute', 'day']), tokens: z.enum(['minute', 'day']) }).default({ requests: 'minute', tokens: 'minute' }),
    plainHeaderMetric: z.enum(['rpm', 'rpd']).default('rpm'),
    probe: z.object({ path: z.string(), parse: z.function() }).optional(),
  }).default({}),
  quirks: z.object({
    timeoutMs: z.number().int().positive().optional(),
    userAgent: z.string().optional(),
    singleToolCall: z.boolean().optional(),
    noTools: z.boolean().optional(),
    maxTokensField: z.string().optional(),
    minMaxTokens: z.number().int().optional(),
    dropFields: z.array(z.string()).optional(),
    stripSchemaKeys: z.array(z.string()).optional(),
    streamUsage: z.boolean().optional(),
  }).default({}),
});

let cache = null;

function load() {
  const directory = path.join(__dirname, 'definitions');
  const definitions = new Map();
  for (const file of fs.readdirSync(directory).filter((name) => name.endsWith('.js')).sort()) {
    const exported = require(path.join(directory, file));
    for (const raw of Array.isArray(exported) ? exported : [exported]) {
      const parsed = definitionSchema.safeParse(raw);
      if (!parsed.success) {
        throw new Error(`Invalid provider definition in ${file} (${raw?.id || 'unknown'}): ${parsed.error.issues.map((issue) => `${issue.path.join('.')}: ${issue.message}`).join('; ')}`);
      }
      if (definitions.has(parsed.data.id)) throw new Error(`Duplicate provider definition id ${parsed.data.id} in ${file}.`);
      definitions.set(parsed.data.id, Object.freeze(parsed.data));
    }
  }
  return definitions;
}

function all() {
  if (!cache) cache = load();
  return cache;
}

function get(id) {
  return all().get(id) || null;
}

function list() {
  return [...all().values()];
}

function createAdapter(definition, account) {
  const Adapter = ADAPTERS[definition.adapter];
  return new Adapter(definition, account);
}

function publicDefinition(definition) {
  return {
    id: definition.id,
    label: definition.label,
    description: definition.description,
    category: definition.category,
    website: definition.website || null,
    keyUrl: definition.keyUrl || null,
    capabilities: definition.capabilities,
    baseUrl: definition.baseUrl,
    editableBaseUrl: definition.editableBaseUrl,
    keyOptional: definition.keyOptional,
    fields: definition.fields,
    billing: {
      defaultMode: definition.billing.defaultMode,
      freeTierAvailable: definition.billing.freeTierAll,
      hasAlwaysFreeModels: definition.billing.alwaysFree.length > 0,
      note: definition.billing.note,
    },
  };
}

module.exports = { all, get, list, createAdapter, publicDefinition, definitionSchema };
