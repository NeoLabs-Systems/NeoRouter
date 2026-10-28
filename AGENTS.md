# Agent instructions

- NeoRouter is a standalone NeoLabs product. Reuse NeoLabs design tokens,
  auth UX, landing structure, and backend conventions. Do not invent a
  second visual language.
- Always expect the application to run on another server. Logs supplied by
  a user may not match files on this computer.
- Do not hardcode models or scenario-specific behaviour. Routing must stay
  data-driven: metadata sources, measured stats and provider definitions.
  Centralize thresholds (see `server/services/routing/*`, `server/config/env.js`).
- "Free" means 100 % free. Never classify an unknown price as free.
- Do not add fake implementations or unfinished placeholder behaviour.
- Never log or store prompts, completions, provider keys or router key secrets.
- New provider = new entry in `server/providers/definitions/`; new capability
  (search, image, audio, video) = new folder in `server/capabilities/` mounted
  through `server/core/capability_registry.js`.
