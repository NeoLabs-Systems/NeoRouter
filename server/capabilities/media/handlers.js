'use strict';

// Request handlers for the non-chat kinds. Each validates an OpenAI-style
// body, runs it through runCapability (routing, fallback, learning, quotas,
// budget, usage log) and writes the OpenAI response shape. The /v1 gateway
// and the dashboard playground share these, so both behave the same.
const crypto = require('node:crypto');
const { z } = require('zod');
const { HttpError } = require('../../middleware/error_handler');
const { runCapability } = require('../shared/run_capability');
const { tokenCost, unitCost, estimateAudioMinutes } = require('./pricing');
const systemOne = require('./system_one');
const mediaStore = require('./media_store');
const pins = require('./embedding_pins');
const transcode = require('./transcode');
const videoJobs = require('./video_jobs');
const catalog = require('../../services/catalog/catalog_service');
const accounts = require('../../services/providers/account_service');

const MAX_EMBEDDING_INPUTS = 2048;
const MAX_AUDIO_BYTES = 25 * 1024 * 1024;

function parseBody(schema, body) {
  const parsed = schema.safeParse(body || {});
  if (!parsed.success) {
    const issue = parsed.error.issues[0];
    throw new HttpError(400, 'INVALID_REQUEST', `${issue.path.join('.') || 'body'}: ${issue.message}`);
  }
  return parsed.data;
}

const estimateTokens = (texts) => texts.reduce((sum, text) => sum + Math.ceil(String(text).length / 4), 0);
// Unknown unit price: fall back to token price (Gemini prices speech per token).
const priced = (unitValue, tokenValue) => (unitValue !== null ? unitValue : tokenValue);

// ---------------------------------------------------------------- embeddings

const embeddingSchema = z.object({
  model: z.string().max(300).default('auto'),
  input: z.union([z.string().min(1), z.array(z.string().min(1)).min(1).max(MAX_EMBEDDING_INPUTS)]),
  dimensions: z.number().int().positive().max(8192).optional(),
  encoding_format: z.enum(['float', 'base64']).default('float'),
  // NeoRouter extension: retrieval models embed queries and documents differently.
  input_type: z.enum(['query', 'passage', 'document', 'search_query', 'search_document']).optional(),
  user: z.string().optional(),
});

function float32Base64(vector) {
  return Buffer.from(new Float32Array(vector).buffer).toString('base64');
}

async function embeddings({ req, res, key, body, freeOnly = false }) {
  const input = parseBody(embeddingSchema, body);
  const texts = Array.isArray(input.input) ? input.input : [input.input];
  const inputType = /passage|document/.test(input.input_type || '') ? 'passage' : 'query';
  const tokens = estimateTokens(texts);
  const auto = input.model === 'auto' || input.model.startsWith('auto:');
  const pin = auto ? pins.get(key.id) : null;
  if (pin?.dimensions && input.dimensions && input.dimensions !== pin.dimensions) {
    throw new HttpError(400, 'EMBEDDING_PIN_MISMATCH', `This key is pinned to ${pin.canonicalId} at ${pin.dimensions} dimensions; vectors of another size would not be comparable. Ask for ${pin.dimensions} dimensions or reset the pin.`);
  }
  const dimensions = input.dimensions || pin?.dimensions || undefined;
  await runCapability({
    req, res, key, capability: 'embedding', method: 'embed', requestedModel: input.model, freeOnly, inputTokens: tokens, units: texts.length,
    filter: pin ? (entry) => entry.canonicalId === pin.canonicalId : null,
    onNoCandidate: () => (pin ? new HttpError(503, 'EMBEDDING_PIN_UNAVAILABLE', `This key is pinned to the embedding model ${pin.canonicalId}, and no provider can serve it right now. Mixing in another model would make your vectors incomparable; reset the pin to switch models (and re-embed your data).`) : null),
    estimateCost: (entry) => tokenCost(entry, tokens),
    invoke: ({ adapter, entry, signal, timeoutMs }) => adapter.embed({ input: texts, dimensions, inputType }, { modelId: entry.modelId, signal, timeoutMs }),
    costOf: (entry, result) => tokenCost(entry, result.inputTokens ?? tokens) || 0,
    respond: (result, entry) => {
      const size = result.vectors[0]?.length || null;
      if (auto && !pin) pins.set(key.id, entry.canonicalId, input.dimensions || size);
      if (auto) res.set('X-NeoRouter-Embedding-Pin', entry.canonicalId);
      const used = result.inputTokens ?? tokens;
      res.json({
        object: 'list',
        data: result.vectors.map((vector, index) => ({ object: 'embedding', index, embedding: input.encoding_format === 'base64' ? float32Base64(vector) : vector })),
        model: entry.id,
        usage: { prompt_tokens: used, total_tokens: used },
      });
    },
  });
}

// -------------------------------------------------------------------- images

const imageSchema = z.object({
  model: z.string().max(300).default('auto'),
  prompt: z.string().min(1).max(32000),
  n: z.number().int().min(1).max(4).default(1),
  size: z.string().regex(/^(\d{2,4}x\d{2,4}|auto)$/).optional(),
  quality: z.string().max(20).optional(),
  response_format: z.enum(['url', 'b64_json']).default('b64_json'),
  user: z.string().optional(),
});

async function fetchImage(url, signal) {
  const response = await fetch(url, { signal: AbortSignal.any([signal, AbortSignal.timeout(30_000)]) });
  if (!response.ok) throw new HttpError(502, 'IMAGE_FETCH_FAILED', 'The generated image could not be downloaded from the provider.');
  return { b64: Buffer.from(await response.arrayBuffer()).toString('base64'), mime: response.headers.get('content-type') || 'image/png' };
}

async function images({ req, res, key, body, freeOnly = false }) {
  const input = parseBody(imageSchema, body);
  const size = input.size === 'auto' ? undefined : input.size;
  await runCapability({
    req, res, key, capability: 'image', method: 'generateImage', requestedModel: input.model, freeOnly, inputTokens: estimateTokens([input.prompt]), units: input.n,
    estimateCost: (entry) => unitCost(entry, 'image', input.n),
    invoke: ({ adapter, entry, signal, timeoutMs }) => adapter.generateImage({ prompt: input.prompt, n: input.n, size, quality: input.quality }, { modelId: entry.modelId, signal, timeoutMs }),
    costOf: (entry, result) => unitCost(entry, 'image', result.images.length) || 0,
    respond: async (result, entry) => {
      const data = [];
      for (const image of result.images) {
        let { b64, mime } = image;
        if (!b64 && input.response_format === 'b64_json') ({ b64, mime } = await fetchImage(image.url, AbortSignal.timeout(30_000)));
        const item = input.response_format === 'b64_json'
          ? { b64_json: b64 }
          : { url: b64 ? mediaStore.publicUrl(req, mediaStore.put(Buffer.from(b64, 'base64'), mime || 'image/png')) : image.url };
        if (image.revisedPrompt) item.revised_prompt = image.revisedPrompt;
        data.push(item);
      }
      res.set('X-NeoRouter-Model', entry.id);
      res.json({ created: Math.floor(Date.now() / 1000), model: entry.id, data });
    },
  });
}

// -------------------------------------------------------------------- speech

const speechSchema = z.object({
  model: z.string().max(300).default('auto'),
  input: z.string().min(1).max(4096),
  voice: z.string().max(60).optional(),
  response_format: z.enum(['mp3', 'opus', 'aac', 'flac', 'wav', 'pcm']).default('mp3'),
  speed: z.number().min(0.25).max(4).optional(),
  instructions: z.string().max(4000).optional(),
});

async function speech({ req, res, key, body, freeOnly = false, asJson = false }) {
  const input = parseBody(speechSchema, body);
  const chars = input.input.length;
  await runCapability({
    req, res, key, capability: 'speech', method: 'speech', requestedModel: input.model, freeOnly, inputTokens: estimateTokens([input.input]), units: chars,
    estimateCost: (entry) => priced(unitCost(entry, 'mchar', chars), tokenCost(entry, estimateTokens([input.input]))),
    invoke: ({ adapter, entry, signal, timeoutMs }) => adapter.speech({ input: input.input, voice: input.voice, format: input.response_format, speed: input.speed }, { modelId: entry.modelId, signal, timeoutMs }),
    costOf: (entry) => priced(unitCost(entry, 'mchar', chars), tokenCost(entry, estimateTokens([input.input]))) || 0,
    respond: async (result, entry) => {
      const converted = await transcode.toFormat(result.audio, result.mime, input.response_format);
      res.set('X-NeoRouter-Audio-Format', converted.format);
      if (asJson) return res.json({ audio: converted.audio.toString('base64'), mime: converted.mime, format: converted.format, model: entry.id });
      res.set('Content-Type', converted.mime);
      return res.send(converted.audio);
    },
  });
}

// ------------------------------------------------------------- transcription

const transcriptionSchema = z.object({
  model: z.string().max(300).default('auto'),
  language: z.string().max(20).optional(),
  prompt: z.string().max(4000).optional(),
  response_format: z.enum(['json', 'text', 'srt', 'verbose_json', 'vtt']).default('json'),
  temperature: z.coerce.number().min(0).max(1).optional(),
});

function timestamp(seconds, separator) {
  // Whole milliseconds, so 1.2 s is 00:00:01,200 and not 00:00:01,199.
  const ms = Math.round(Math.max(0, Number(seconds) || 0) * 1000);
  const pad = (value, size = 2) => String(Math.floor(value)).padStart(size, '0');
  return `${pad(ms / 3600000)}:${pad((ms % 3600000) / 60000)}:${pad((ms % 60000) / 1000)}${separator}${pad(ms % 1000, 3)}`;
}

// Subtitles need timestamps; without segments the whole text is one cue.
function subtitles(result, format) {
  const segments = result.segments?.length ? result.segments : [{ start: 0, end: result.duration || 0, text: result.text }];
  const cues = segments.map((segment, index) => {
    const range = `${timestamp(segment.start, format === 'srt' ? ',' : '.')} --> ${timestamp(segment.end, format === 'srt' ? ',' : '.')}`;
    return format === 'srt' ? `${index + 1}\n${range}\n${String(segment.text).trim()}\n` : `${range}\n${String(segment.text).trim()}\n`;
  });
  return format === 'vtt' ? `WEBVTT\n\n${cues.join('\n')}` : cues.join('\n');
}

/** fields: multipart text fields; file: { data Buffer, filename, mime }. */
async function transcription({ req, res, key, fields, file, translate = false, freeOnly = false, includeModel = false }) {
  const input = parseBody(transcriptionSchema, fields);
  if (!file?.data?.length) throw new HttpError(400, 'INVALID_REQUEST', 'file: an audio file is required.');
  if (file.data.length > MAX_AUDIO_BYTES) throw new HttpError(413, 'FILE_TOO_LARGE', 'Audio files can be at most 25 MB.');
  const minutes = estimateAudioMinutes(file.data.length);
  // Subtitles need segment timestamps: prefer models that return them, as
  // long as one is in the catalog (otherwise the whole text is one cue).
  const needsTimestamps = ['srt', 'vtt', 'verbose_json'].includes(input.response_format) && (input.model === 'auto' || input.model.startsWith('auto:'));
  const hasTimestamps = (adapter) => adapter.transcribeTimestamps !== false;
  const timestampFilter = needsTimestamps && catalog.listForUser(key.user_id, { capability: 'transcription' }).some((entry) => {
    const runtime = entry.enabled && accounts.getRuntime(entry.accountId);
    return runtime && hasTimestamps(accounts.adapterFor(runtime));
  }) ? (_entry, adapter) => hasTimestamps(adapter) : null;
  const tokensFor = (mins) => Math.ceil(mins * 60 * 32); // Gemini counts about 32 tokens per second of audio
  await runCapability({
    req, res, key, capability: 'transcription', method: 'transcribe', requestedModel: input.model, freeOnly, inputTokens: tokensFor(minutes), units: minutes,
    filter: timestampFilter,
    // Audio is not text in a context window (Whisper lists 448 tokens).
    contextTokens: input.prompt ? estimateTokens([input.prompt]) : 0,
    estimateCost: (entry) => priced(unitCost(entry, 'minute', minutes), tokenCost(entry, tokensFor(minutes))),
    invoke: async ({ adapter, entry, signal, timeoutMs }) => {
      const result = await adapter.transcribe({ file: file.data, filename: file.filename, mime: file.mime, language: input.language, prompt: input.prompt, translate }, { modelId: entry.modelId, signal, timeoutMs });
      if (result.duration) result.units = result.duration / 60;
      return result;
    },
    costOf: (entry, result) => {
      const used = result.duration ? result.duration / 60 : minutes;
      return priced(unitCost(entry, 'minute', used), tokenCost(entry, result.inputTokens ?? tokensFor(used))) || 0;
    },
    respond: (result, entry) => {
      if (includeModel) return res.json({ text: result.text, language: result.language || null, duration: result.duration ?? null, model: entry.id });
      if (input.response_format === 'text') return res.type('text/plain').send(result.text);
      if (input.response_format === 'srt' || input.response_format === 'vtt') {
        return res.type(input.response_format === 'srt' ? 'application/x-subrip' : 'text/vtt').send(subtitles(result, input.response_format));
      }
      if (input.response_format === 'verbose_json') {
        return res.json({
          task: translate ? 'translate' : 'transcribe',
          language: result.language || null,
          duration: result.duration ?? null,
          text: result.text,
          segments: (result.segments || []).map((segment, id) => ({ id, start: segment.start, end: segment.end, text: segment.text })),
        });
      }
      return res.json({ text: result.text });
    },
  });
}

// -------------------------------------------------------- System One / moderation

const moderationSchema = z.object({
  model: z.string().max(300).default('auto'),
  input: z.union([
    z.string().min(1),
    z.array(z.union([z.string().min(1), z.object({ type: z.string(), text: z.string().optional() }).passthrough()])).min(1).max(32),
  ]),
  // NeoRouter extensions: what to check, and the policy for policy-following
  // classifiers (gpt-oss-safeguard, topic control).
  check: z.enum(['safety', 'prompt_injection', 'topic']).default('safety'),
  policy: z.string().max(20000).optional(),
});

// Which System One families can answer which question. Prompt Guard only
// spots prompt attacks and topic control needs a policy, so neither may
// stand in for a general safety check.
const CHECK_FAMILIES = {
  safety: new Set(['llama_guard', 'nemoguard', 'safeguard', 'shieldgemma', 'native', 'generic']),
  prompt_injection: new Set(['prompt_guard']),
  topic: new Set(['topic_control', 'safeguard']),
};

async function moderations({ req, res, key, body, freeOnly = false }) {
  const input = parseBody(moderationSchema, body);
  if (input.check === 'topic' && !input.policy) throw new HttpError(400, 'INVALID_REQUEST', 'policy: a topic check needs a policy that says which topics are allowed.');
  const inputs = (Array.isArray(input.input) ? input.input : [input.input])
    .map((item) => (typeof item === 'string' ? item : item.text || ''))
    .filter(Boolean);
  if (!inputs.length) throw new HttpError(400, 'INVALID_REQUEST', 'input: only text inputs are supported.');
  const tokens = estimateTokens(inputs) + (input.policy ? estimateTokens([input.policy]) : 0);
  await runCapability({
    req, res, key, capability: 'moderation', method: 'chat', requestedModel: input.model, freeOnly, inputTokens: tokens, units: inputs.length,
    // A model named explicitly is taken at its word; auto picks a family that fits the check.
    filter: input.model === 'auto' || input.model.startsWith('auto:') ? (entry) => CHECK_FAMILIES[input.check].has(systemOne.family(entry.modelId)) : null,
    estimateCost: (entry) => tokenCost(entry, tokens + 400 * inputs.length),
    invoke: ({ adapter, entry, signal, timeoutMs }) => systemOne.classify(adapter, { inputs, policy: input.policy }, { modelId: entry.modelId, signal, timeoutMs }),
    costOf: (entry, result) => tokenCost(entry, result.inputTokens || tokens) || 0,
    respond: (result, entry) => res.json({ id: `modr-${crypto.randomUUID()}`, model: entry.id, results: result.results }),
  });
}

// --------------------------------------------------------------------- video

const videoSchema = z.object({
  model: z.string().max(300).default('auto'),
  prompt: z.string().min(1).max(32000),
  seconds: z.coerce.number().int().min(1).max(60).default(4),
  size: z.string().max(20).optional(),
});

async function createVideo({ req, res, key, body, freeOnly = false }) {
  const input = parseBody(videoSchema, body);
  await runCapability({
    req, res, key, capability: 'video', method: 'createVideo', requestedModel: input.model, freeOnly, inputTokens: estimateTokens([input.prompt]), units: input.seconds,
    estimateCost: (entry) => unitCost(entry, 'second', input.seconds),
    invoke: ({ adapter, entry, signal, timeoutMs }) => adapter.createVideo({ prompt: input.prompt, seconds: input.seconds, size: input.size }, { modelId: entry.modelId, signal, timeoutMs }),
    costOf: (entry) => unitCost(entry, 'second', input.seconds) || 0,
    respond: (result, entry) => {
      const job = videoJobs.create({ key, requestId: req.id, entry, upstreamId: result.upstreamId, status: result.status, progress: result.progress, request: input });
      res.status(200).json(videoJobs.toObject(job));
    },
  });
}

module.exports = { embeddings, images, speech, transcription, moderations, createVideo, subtitles, float32Base64 };
