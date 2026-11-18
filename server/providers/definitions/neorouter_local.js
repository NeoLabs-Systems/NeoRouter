'use strict';

// The models an admin runs on this server (Admin → Local models). Nothing to
// configure: the connection lists whatever is running right now. Admins get
// it when they install a model; other people only when an admin allows it.
// Named like a product, so the name needs no translation where it shows up.
module.exports = {
  id: 'neorouter_local',
  label: 'NeoRouter Local',
  description: 'Models running on this server, managed by your admin.',
  category: 'local',
  adapter: 'local_runtime',
  keyOptional: true,
  media: { embed: true, speech: true, transcribe: true },
  billing: {
    defaultMode: 'free_tier',
    freeTierAll: true,
    note: 'Runs on this server: requests cost nothing.',
  },
  quirks: { timeoutMs: 600000 },
};
