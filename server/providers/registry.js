'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { z } = require('zod');
const { OpenAICompatibleAdapter } = require('./adapters/openai_compatible');
const { AnthropicAdapter } = require('./adapters/anthropic');
const { GoogleGeminiAdapter } = require('./adapters/google_gemini');
const { CloudflareAdapter } = require('./adapters/cloudflare');
const { SearchAdapter } = require('./adapters/search_engines');
const { DeepgramAdapter } = require('./adapters/deepgram');
const { ElevenLabsAdapter } = require('./adapters/elevenlabs');
const { CodexAdapter } = require('./adapters/codex');

const ADAPTERS = {
  openai_compatible: OpenAICompatibleAdapter,
  anthropic: AnthropicAdapter,
  google_gemini: GoogleGeminiAdapter,
  cloudflare: CloudflareAdapter,
  search: SearchAdapter,
  deepgram: DeepgramAdapter,
  elevenlabs: ElevenLabsAdapter,
  codex: CodexAdapter,
};

const limitSchema = z.object({
  // Requests that may run at once (account level only).
  concurrent: z.number().int().positive().optional(),
  rpm: z.number().int().positive().optional(),
  rpd: z.number().int().positive().optional(),
  tpm: z.number().int().positive().optional(),
  tpd: z.number().int().positive().optional(),
});

const definitionSchema = z.object({
  id: z.string().regex(/^[a-z0-9][a-z0-9_-]*$/),
  label: z.string().min(1),
  description: z.string().default(''),
  category: z.enum(['free', 'paid', 'aggregator', 'plan', 'local', 'custom', 'community', 'search']),
  // Web search engines: which dialect the search adapter speaks.
  // Connected by signing in instead of pasting a key.
  connect: z.enum(['codex_device']).optional(),
  search: z.object({ engine: z.enum(['tavily', 'exa', 'linkup', 'parallel', 'firecrawl', 'serper', 'brave', 'jina', 'searxng']) }).optional(),
  website: z.string().url().optional(),
  keyUrl: z.string().url().optional(),
  adapter: z.enum(Object.keys(ADAPTERS)),
  capabilities: z.array(z.enum(['chat'])).default(['chat']),
  // Non-chat endpoints this provider offers, as adapter method → path
  // (OpenAI-compatible adapters) or `true` (adapters with their own dialect).
  media: z.object({
    embed: z.union([z.string(), z.literal(true)]).optional(),
    generateImage: z.union([z.string(), z.literal(true)]).optional(),
    speech: z.union([z.string(), z.literal(true)]).optional(),
    transcribe: z.union([z.string(), z.literal(true)]).optional(),
    moderate: z.union([z.string(), z.literal(true)]).optional(),
    classify: z.union([z.string(), z.literal(true)]).optional(),
    createVideo: z.union([z.string(), z.literal(true)]).optional(),
    decide: z.union([z.string(), z.literal(true)]).optional(),
    search: z.literal(true).optional(),
  }).default({}),
  // Live voice WebSocket protocols the provider speaks (capabilities/live/dialects.js).
  live: z.array(z.enum(['openai_live', 'openai_realtime', 'gemini_live'])).default([]),
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
  // extraPaths: further listings, e.g. OpenRouter's /embeddings/models.
  models: z.object({ path: z.string().optional(), static: z.array(z.any()).optional(), extraPaths: z.array(z.string()).default([]) }).default({}),
  // Models a provider serves but does not list (NVIDIA's FLUX endpoints).
  extraModels: z.array(z.object({ id: z.string(), name: z.string().optional(), type: z.string().optional() })).default([]),
  validate: z.union([z.literal(false), z.object({ path: z.string().optional(), chatProbe: z.boolean().optional() })]).optional(),
  billing: z.object({
    defaultMode: z.enum(['free_tier', 'paid', 'plan', 'plan_paid']),
    // A subscription with an included allowance this provider sells for API use.
    plan: z.object({ note: z.string() }).optional(),
    // With billing_mode=free_tier the account has no payment method, so
    // nothing it can call is ever charged. freeTierAll covers every model;
    // freeTierModels (patterns) only the models the free tier includes.
    freeTierAll: z.boolean().default(false),
    freeTierModels: z.array(z.string()).default([]),
    // Automatic proof that the account is not billed (services/providers/
    // billing_check.js). Without it the user's billing mode is trusted.
    verify: z.discriminatedUnion('type', [
      z.object({ type: z.literal('ratelimit_headers'), maxFreeRequestsPerDay: z.number().positive(), probeModels: z.array(z.string()).default([]) }),
      z.object({ type: z.literal('paid_model_probe'), paidOnlyModels: z.array(z.string()).min(1) }),
    ]).optional(),
    // The free tier applies only to a loopback/private base URL, or only without a key.
    localOnly: z.boolean().default(false),
    keylessOnly: z.boolean().default(false),
    // Where the free-tier facts were checked, and when (YYYY-MM-DD).
    source: z.string().optional(),
    checkedAt: z.string().optional(),
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
    // Media dialect details (see adapters/openai_media.js).
    embeddingInputType: z.boolean().optional(),
    imageApi: z.enum(['openai', 'nvidia_genai']).optional(),
    noImageResponseFormat: z.boolean().optional(),
    defaultVoice: z.string().optional(),
    voices: z.array(z.string()).optional(),
    speechFormats: z.array(z.string()).optional(),
    transcriptionJson: z.boolean().optional(),
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
    connect: definition.connect || null,
    billing: {
      defaultMode: definition.billing.defaultMode,
      // "Free" (no credits, free models only) is offered whenever the provider
      // is known to have free models; accounts may also find some in their listing.
      freeTierAvailable: definition.billing.freeTierAll || definition.billing.freeTierModels.length > 0 || definition.billing.alwaysFree.length > 0,
      freeTierPartial: !definition.billing.freeTierAll && (definition.billing.freeTierModels.length > 0 || definition.billing.alwaysFree.length > 0),
      verified: Boolean(definition.billing.verify),
      hasAlwaysFreeModels: definition.billing.alwaysFree.length > 0,
      note: definition.billing.note,
      // Prepaid plans: offered by the provider, or possible on any custom endpoint.
      planAvailable: Boolean(definition.billing.plan) || definition.editableBaseUrl,
      planNote: definition.billing.plan?.note || null,
    },
  };
}

module.exports = { all, get, list, createAdapter, publicDefinition, definitionSchema };
