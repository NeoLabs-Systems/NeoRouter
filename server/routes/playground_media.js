'use strict';

const express = require('express');
const { z } = require('zod');
const routerKeys = require('../services/router_keys/router_key_service');
const handlers = require('../capabilities/media/handlers');
const videoJobs = require('../capabilities/media/video_jobs');
const { HttpError } = require('../middleware/error_handler');
const { requirePermission } = require('../middleware/space');
const { asyncRoute } = require('./helpers');
const catalog = require('../services/catalog/catalog_service');
const accounts = require('../services/providers/account_service');
const { OPENAI_VOICES, voiceList } = require('../providers/adapters/openai_media');

const router = express.Router();
router.use(requirePermission('playground.use'));

// The playground's non-chat modes, through the same handlers as /v1 with one
// of the user's keys. Audio comes and goes as base64 JSON so the dashboard
// needs no multipart or binary handling.
const envelope = z.object({ keyId: z.string().min(1), freeOnly: z.boolean().optional() }).passthrough();

function keyFor(req) {
  const parsed = envelope.safeParse(req.body || {});
  if (!parsed.success) throw new HttpError(400, 'INVALID_REQUEST', 'keyId is required.');
  const key = routerKeys.getRow(req.space.ownerId, parsed.data.keyId);
  if (key.revoked_at) throw new HttpError(400, 'KEY_REVOKED', 'This router key is revoked.');
  const { keyId, freeOnly, ...body } = req.body;
  return { key, body, freeOnly: freeOnly === true };
}

const KINDS = {
  embedding: (ctx) => handlers.embeddings(ctx),
  image: (ctx) => handlers.images({ ...ctx, body: { response_format: 'b64_json', ...ctx.body } }),
  speech: (ctx) => handlers.speech({ ...ctx, asJson: true }),
  moderation: (ctx) => handlers.moderations(ctx),
  decision: (ctx) => handlers.decide(ctx),
  search: (ctx) => handlers.search(ctx),
  weather: (ctx) => handlers.weather(ctx),
  video: (ctx) => handlers.createVideo(ctx),
  transcription: (ctx) => {
    const { audio, filename, mime, translate, ...fields } = ctx.body;
    if (typeof audio !== 'string' || !audio) throw new HttpError(400, 'INVALID_REQUEST', 'audio: base64 audio is required.');
    return handlers.transcription({ ...ctx, fields, file: { data: Buffer.from(audio, 'base64'), filename: filename || 'audio.wav', mime: mime || null }, translate: translate === true, includeModel: true });
  },
};

router.post('/media/:kind', asyncRoute(async (req, res) => {
  const run = KINDS[req.params.kind];
  if (!run) throw new HttpError(404, 'UNKNOWN_KIND', `Unknown kind "${req.params.kind}".`);
  const { key, body, freeOnly } = keyFor(req);
  await run({ req, res, key, body, freeOnly });
}));

// The voices a speech model offers. Auto may land on any provider, so it
// offers OpenAI's names, which every speech adapter maps to a voice of its own.
router.get('/voices', asyncRoute(async (req, res) => {
  const model = typeof req.query.model === 'string' ? req.query.model : 'auto';
  const entry = model.startsWith('auto') ? null : catalog.listForUser(req.space.ownerId, { capability: 'speech' }).find((item) => item.id === model);
  const runtime = entry && accounts.getRuntime(entry.accountId);
  const adapter = runtime && accounts.adapterFor(runtime);
  let voices = voiceList(OPENAI_VOICES);
  if (adapter?.voices) {
    try {
      voices = await adapter.voices({ modelId: entry.modelId });
    } catch {
      // The provider's list is optional: OpenAI's names still work.
    }
  }
  res.json({ voices });
}));

// Video jobs as the dashboard sees them; the finished file comes back as
// base64 so the playground can offer it for download.
router.get('/videos/:id', asyncRoute(async (req, res) => {
  res.json(videoJobs.toObject(await videoJobs.refresh(videoJobs.find(req.space.ownerId, req.params.id))));
}));

router.get('/videos/:id/content', asyncRoute(async (req, res) => {
  const job = await videoJobs.refresh(videoJobs.find(req.space.ownerId, req.params.id));
  const { response } = await videoJobs.content(job, AbortSignal.timeout(300_000));
  res.json({ video: Buffer.from(await response.arrayBuffer()).toString('base64'), mime: response.headers.get('content-type') || 'video/mp4' });
}));

module.exports = router;
