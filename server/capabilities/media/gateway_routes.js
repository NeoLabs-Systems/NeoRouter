'use strict';

// OpenAI-compatible endpoints for embeddings, images, speech, transcription,
// System One (moderations) and video jobs. See handlers.js.
const express = require('express');
const { requireRouterKey } = require('../../middleware/router_key_auth');
const { HttpError } = require('../../middleware/error_handler');
const { gatewayErrorHandler } = require('../chat/gateway_routes');
const handlers = require('./handlers');
const mediaStore = require('./media_store');
const pins = require('./embedding_pins');
const videoJobs = require('./video_jobs');

const router = express.Router();
const asyncRoute = (handler) => (req, res, next) => Promise.resolve(handler(req, res)).catch(next);
const multipart = express.raw({ type: 'multipart/form-data', limit: '26mb' });

// Multipart uploads (audio files) parsed with the built-in Fetch API parser.
async function readForm(req) {
  if (!Buffer.isBuffer(req.body)) throw new HttpError(400, 'INVALID_REQUEST', 'Send the audio as multipart/form-data with a "file" field.');
  let form;
  try {
    form = await new Request('http://local/', { method: 'POST', headers: { 'content-type': req.get('content-type') }, body: req.body }).formData();
  } catch {
    throw new HttpError(400, 'INVALID_REQUEST', 'The multipart body could not be parsed.');
  }
  const fields = {};
  let file = null;
  for (const [name, value] of form.entries()) {
    if (typeof value === 'string') fields[name] = value;
    else if (name === 'file') file = { data: Buffer.from(await value.arrayBuffer()), filename: value.name || 'audio', mime: value.type || null };
  }
  return { fields, file };
}

const keyed = (handler) => [requireRouterKey, asyncRoute((req, res) => handler({ req, res, key: req.routerKey, body: req.body }))];

router.post('/embeddings', ...keyed(handlers.embeddings));
router.delete('/embeddings/pin', requireRouterKey, (req, res) => res.json({ reset: pins.clear(req.routerKey.id) }));
router.get('/embeddings/pin', requireRouterKey, (req, res) => res.json({ pin: pins.get(req.routerKey.id) }));
router.post('/images/generations', ...keyed(handlers.images));
router.post('/audio/speech', ...keyed(handlers.speech));
router.post('/moderations', ...keyed(handlers.moderations));

for (const [path, translate] of [['/audio/transcriptions', false], ['/audio/translations', true]]) {
  router.post(path, requireRouterKey, multipart, asyncRoute(async (req, res) => {
    const { fields, file } = await readForm(req);
    await handlers.transcription({ req, res, key: req.routerKey, fields, file, translate });
  }));
}

router.post('/videos', ...keyed(handlers.createVideo));
router.get('/videos', requireRouterKey, (req, res) => {
  const data = videoJobs.list(req.routerKey.user_id, { limit: Number(req.query.limit) || 20 }).map(videoJobs.toObject);
  res.json({ object: 'list', data, has_more: false });
});
router.get('/videos/:id', requireRouterKey, asyncRoute(async (req, res) => {
  res.json(videoJobs.toObject(await videoJobs.refresh(videoJobs.find(req.routerKey.user_id, req.params.id))));
}));
router.get('/videos/:id/content', requireRouterKey, asyncRoute(async (req, res) => {
  const job = await videoJobs.refresh(videoJobs.find(req.routerKey.user_id, req.params.id));
  const { response } = await videoJobs.content(job, AbortSignal.timeout(300_000));
  res.set('Content-Type', response.headers.get('content-type') || 'video/mp4');
  const length = response.headers.get('content-length');
  if (length) res.set('Content-Length', length);
  for await (const chunk of response.body) res.write(chunk);
  res.end();
}));

// Generated media handed out as URLs. The unguessable id is the capability.
router.get('/media/:id', (req, res) => {
  const item = mediaStore.get(req.params.id);
  if (!item) return res.status(404).json({ error: { message: 'This media link expired or does not exist.', type: 'invalid_request_error', code: 'MEDIA_NOT_FOUND' } });
  res.set('Content-Type', item.mime);
  res.set('Cache-Control', 'private, max-age=3600');
  return res.send(item.data);
});

router.use(gatewayErrorHandler('openai_chat'));

module.exports = router;
