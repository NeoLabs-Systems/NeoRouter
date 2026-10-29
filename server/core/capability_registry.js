'use strict';

// Each routable kind of model and its gateway routes under /v1. Routing,
// quota, budget and catalog services are capability-agnostic and keyed by
// the model's `capability` column; the non-chat kinds share one router.
const chatRoutes = () => require('../capabilities/chat/gateway_routes');
const mediaRoutes = () => require('../capabilities/media/gateway_routes');

const CAPABILITIES = [
  { id: 'chat', label: 'Chat', routes: chatRoutes },
  { id: 'embedding', label: 'Embeddings', routes: mediaRoutes },
  { id: 'image', label: 'Images', routes: mediaRoutes },
  { id: 'speech', label: 'Speech', routes: mediaRoutes },
  { id: 'transcription', label: 'Transcription', routes: mediaRoutes },
  { id: 'moderation', label: 'System One', routes: mediaRoutes },
  { id: 'video', label: 'Video', routes: mediaRoutes },
  { id: 'search', label: 'Web search', routes: mediaRoutes },
];

function mount(router) {
  for (const routes of new Set(CAPABILITIES.map((capability) => capability.routes))) router.use(routes());
}

function list() {
  return CAPABILITIES.map(({ id, label }) => ({ id, label }));
}

module.exports = { mount, list };
