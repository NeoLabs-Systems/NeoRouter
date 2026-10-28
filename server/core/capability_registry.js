'use strict';

// Each capability (chat today; search, image, audio, video later) mounts its
// own gateway routes under /v1. Routing, quota, budget and catalog services
// are capability-agnostic and keyed by the model's `capability` column.
const CAPABILITIES = [
  { id: 'chat', label: 'Chat', routes: () => require('../capabilities/chat/gateway_routes') },
];

function mount(router) {
  for (const capability of CAPABILITIES) router.use(capability.routes());
}

function list() {
  return CAPABILITIES.map(({ id, label }) => ({ id, label }));
}

module.exports = { mount, list };
