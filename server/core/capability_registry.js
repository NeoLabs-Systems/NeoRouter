'use strict';

// Each routable kind of model and its gateway routes under /v1. Routing,
// quota, budget and catalog services are capability-agnostic and keyed by
// the model's `capability` column; the non-chat kinds share one router.
const chatRoutes = () => require('../capabilities/chat/gateway_routes');
const mediaRoutes = () => require('../capabilities/media/gateway_routes');

// method: the adapter method that serves the kind, so a model is only listed
// and routed when its provider's adapter can actually do it.
const CAPABILITIES = [
  { id: 'chat', label: 'Chat', routes: chatRoutes },
  { id: 'embedding', label: 'Embeddings', routes: mediaRoutes, method: 'embed' },
  { id: 'image', label: 'Images', routes: mediaRoutes, method: 'generateImage' },
  { id: 'speech', label: 'Speech', routes: mediaRoutes, method: 'speech' },
  { id: 'transcription', label: 'Transcription', routes: mediaRoutes, method: 'transcribe' },
  { id: 'moderation', label: 'System One', routes: mediaRoutes },
  { id: 'video', label: 'Video', routes: mediaRoutes, method: 'createVideo' },
  { id: 'search', label: 'Web search', routes: mediaRoutes, method: 'search' },
  { id: 'weather', label: 'Weather', routes: mediaRoutes, method: 'weather' },
  // Live voice sessions are WebSockets, attached to the HTTP server (capabilities/live).
  { id: 'live', label: 'Live voice', routes: null },
];

function mount(router) {
  for (const routes of new Set(CAPABILITIES.map((capability) => capability.routes).filter(Boolean))) router.use(routes());
}

function list() {
  return CAPABILITIES.map(({ id, label }) => ({ id, label }));
}

/** The adapter method serving a kind, or null when any model of it can be called. */
function methodFor(id) {
  return CAPABILITIES.find((capability) => capability.id === id)?.method || null;
}

module.exports = { mount, list, methodFor };
