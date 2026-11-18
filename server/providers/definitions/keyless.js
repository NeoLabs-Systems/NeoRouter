'use strict';

// Providers that work without (or with an optional) key.
module.exports = [
  {
    id: 'kilo',
    label: 'Kilo Gateway',
    description: 'Gateway with free models, no key required.',
    category: 'free',
    website: 'https://kilo.ai',
    adapter: 'openai_compatible',
    baseUrl: 'https://ai-gateway.kilo.ai/api/v1',
    keyOptional: true,
    // Some paid models (Lyria songs and clips) are listed at token price 0, so the listing is not trusted.
    billing: { defaultMode: 'paid', alwaysFree: [':free$', '^kilo-auto/free$', '^openrouter/free$', '^reflection/beam-free$'], trustListingFree: false, note: 'Only :free models (and kilo-auto/free) are free; everything else, including some models listed at price 0, is paid from credits. Without a key: 200 requests an hour per IP.', source: 'https://kilo.ai/docs/gateway/authentication', checkedAt: '2026-11-07' },
  },
  {
    id: 'llm7',
    label: 'LLM7',
    description: 'Free OpenAI-compatible endpoint; a key raises the limits.',
    category: 'free',
    website: 'https://llm7.io',
    keyUrl: 'https://token.llm7.io',
    adapter: 'openai_compatible',
    baseUrl: 'https://api.llm7.io/v1',
    keyOptional: true,
    // Most models are usage-based ("pro"); only the turbo tier is free.
    billing: { defaultMode: 'free_tier', freeTierModels: ['^codestral-latest$', '^gpt-oss:20b$', '^mistral-nemo-instruct-2407$', '^nemotron-3-nano:30b$'], note: 'Only the turbo-tier models are free; the rest are usage-based.', source: 'https://api.llm7.io/v1/models', checkedAt: '2026-11-07' },
  },
  {
    id: 'ovh',
    label: 'OVHcloud AI Endpoints',
    description: 'European hosting, anonymous access with low limits.',
    category: 'free',
    website: 'https://endpoints.ai.cloud.ovh.net',
    adapter: 'openai_compatible',
    baseUrl: 'https://oai.endpoints.kepler.ai.cloud.ovh.net/v1',
    // Whisper is OpenAI-compatible; the text-to-speech models (nvr-tts-*) speak
    // NVIDIA Riva over HTTP, each on a host of its own.
    media: { transcribe: '/audio/transcriptions', speech: 'https://{model}.endpoints.kepler.ai.cloud.ovh.net/api/v1/tts/text_to_audio' },
    quirks: { speechApi: 'riva' },
    keyOptional: true,
    // Whisper and speech models are listed at 0 but billed per second with a key.
    billing: { defaultMode: 'free_tier', freeTierAll: true, keylessOnly: true, trustListingFree: false, note: 'Free only without a key (anonymous, 2 requests a minute per model). With an API key usage is billed in EUR to your Public Cloud project, including Whisper per second of audio.', source: 'https://docs.ovhcloud.com/en/guides/public-cloud/ai-machine-learning/ai-endpoints-capabilities', checkedAt: '2026-11-07' },
    limits: { pool: 'model', defaults: { rpm: 2 } },
  },
  {
    id: 'pollinations',
    label: 'Pollinations',
    description: 'Community-funded free text, image and embedding models.',
    category: 'free',
    website: 'https://pollinations.ai',
    keyUrl: 'https://enter.pollinations.ai',
    adapter: 'openai_compatible',
    baseUrl: 'https://gen.pollinations.ai/v1',
    keyOptional: true,
    validate: { path: 'https://gen.pollinations.ai/account/key' },
    // Image and embedding models have listings of their own (/v1/models repeats them today).
    models: { extraPaths: ['https://gen.pollinations.ai/image/models', 'https://gen.pollinations.ai/embeddings/models'] },
    media: { embed: '/embeddings', generateImage: '/images/generations' },
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Free models spend free Quest Pollen; models marked paid_only need purchased pollen and are never free. Choose paid if the account holds paid pollen.', source: 'https://github.com/pollinations/pollinations/blob/main/APIDOCS.md', checkedAt: '2026-11-07' },
  },
  {
    id: 'aihorde',
    label: 'AI Horde',
    description: 'Crowdsourced volunteer workers. Slow, queued, free.',
    category: 'free',
    website: 'https://aihorde.net',
    keyUrl: 'https://aihorde.net/register',
    adapter: 'openai_compatible',
    baseUrl: 'https://oai.aihorde.net/v1',
    auth: { type: 'bearer', anonymousKey: '0000000000' },
    keyOptional: true,
    billing: { defaultMode: 'free_tier', freeTierAll: true, note: 'Volunteer capacity; expect queueing. Anonymous use works; a free key gives priority from kudos.', source: 'https://aihorde.net/faq', checkedAt: '2026-11-07' },
    quirks: { noTools: true, minMaxTokens: 16, timeoutMs: 600000, streamUsage: false },
  },
];
