'use strict';

// The models an admin can run on this server, one file per kind next to this
// one. Each entry is data only: where the files come from, which engine runs
// them, what they are good for. What a model needs from the machine is worked
// out from its size (fit.js), so adding a model is adding an entry.
const fs = require('node:fs');
const path = require('node:path');
const { z } = require('zod');

const text = z.object({ en: z.string().min(1), de: z.string().min(1) });

const entrySchema = z.object({
  // Stable id; also the model id clients use (local/<id>). Lower case, no slashes.
  id: z.string().regex(/^[a-z0-9][a-z0-9.-]*$/),
  name: z.string().min(1),
  // What the router does with it: the same kinds as provider models.
  kind: z.enum(['chat', 'moderation', 'embedding', 'transcription', 'speech']),
  engine: z.enum(['llama_cpp', 'transformers']),
  summary: text,
  // Hugging Face repo and the files to fetch from it (exact paths or * globs).
  source: z.object({
    repo: z.string().regex(/^[\w.-]+\/[\w.-]+$/),
    include: z.array(z.string().min(1)).min(1),
    // llama.cpp: the vision projector that lets a text model read images.
    projector: z.string().optional(),
  }),
  // Download size in MB (1 MB = 1024 × 1024 bytes, like memory and disk), shown before the exact list is fetched.
  downloadMb: z.number().positive(),
  // Memory it needs when running, in MB, where the size alone says too little.
  memoryMb: z.number().positive().optional(),
  languages: z.union([z.literal('many'), z.array(z.string().length(2)).min(1)]),
  license: z.string().min(1),
  // A good first choice in its kind, offered first.
  recommended: z.boolean().default(false),
  // translate: speech recognition that can also translate into English.
  features: z.object({ tools: z.boolean().default(false), vision: z.boolean().default(false), reasoning: z.boolean().default(false), translate: z.boolean().default(false) }).default({}),
  // transformers.js: the pipeline task and the weight precision to load.
  transformers: z.object({
    task: z.enum(['automatic-speech-recognition', 'text-to-speech']),
    dtype: z.string().optional(),
  }).optional(),
  // Speech: the voices (files in the repo, or one built-in voice).
  voices: z.array(z.object({ id: z.string(), name: z.string(), file: z.string().optional() })).optional(),
}).superRefine((entry, ctx) => {
  if (entry.engine === 'transformers' && !entry.transformers) ctx.addIssue({ code: 'custom', message: 'transformers models need a task' });
  if (entry.kind === 'speech' && !entry.voices?.length) ctx.addIssue({ code: 'custom', message: 'speech models need voices' });
});

let cache = null;

function load() {
  const entries = new Map();
  for (const file of fs.readdirSync(__dirname).filter((name) => name.endsWith('.js') && name !== 'index.js').sort()) {
    for (const raw of require(path.join(__dirname, file))) {
      const parsed = entrySchema.safeParse(raw);
      if (!parsed.success) throw new Error(`Invalid local model in ${file} (${raw?.id || 'unknown'}): ${parsed.error.issues.map((issue) => `${issue.path.join('.')}: ${issue.message}`).join('; ')}`);
      if (entries.has(parsed.data.id)) throw new Error(`Duplicate local model id ${parsed.data.id} in ${file}.`);
      entries.set(parsed.data.id, Object.freeze(parsed.data));
    }
  }
  return entries;
}

function all() {
  if (!cache) cache = load();
  return [...cache.values()];
}

function get(id) {
  if (!cache) cache = load();
  return cache.get(id) || null;
}

module.exports = { all, get, entrySchema };
