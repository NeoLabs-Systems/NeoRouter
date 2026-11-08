'use strict';

// System One checks (POST /v1/moderations): models that judge whether a text
// is safe instead of answering it. Their ids keep the family name, which is
// how System One knows how to ask them (capabilities/media/system_one.js).
module.exports = [
  {
    id: 'llama-guard-3-1b',
    name: 'Llama Guard 3 1B',
    kind: 'moderation',
    engine: 'llama_cpp',
    summary: {
      en: 'Small safety check: flags harmful requests and answers in a fraction of a second.',
      de: 'Kleine Sicherheitsprüfung: erkennt schädliche Anfragen in Sekundenbruchteilen.',
    },
    source: { repo: 'RichardErkhov/alpindale_-_Llama-Guard-3-1B-gguf', include: ['Llama-Guard-3-1B.Q4_K_M.gguf'] },
    downloadMb: 911,
    languages: 'many',
    license: 'Llama 3.2 Community',
    recommended: true,
  },
  {
    id: 'llama-guard-3-8b',
    name: 'Llama Guard 3 8B',
    kind: 'moderation',
    engine: 'llama_cpp',
    summary: {
      en: 'More careful safety check that catches more subtle cases.',
      de: 'Gründlichere Sicherheitsprüfung, die auch feinere Fälle erkennt.',
    },
    source: { repo: 'mradermacher/Llama-Guard-3-8B-GGUF', include: ['Llama-Guard-3-8B.Q4_K_M.gguf'] },
    downloadMb: 4693,
    languages: 'many',
    license: 'Llama 3.1 Community',
  },
  {
    id: 'gpt-oss-safeguard-20b',
    name: 'gpt-oss-safeguard 20B',
    kind: 'moderation',
    engine: 'llama_cpp',
    summary: {
      en: 'Follows your own written rules, e.g. which topics are allowed.',
      de: 'Folgt deinen eigenen Regeln, z. B. welche Themen erlaubt sind.',
    },
    source: { repo: 'unsloth/gpt-oss-safeguard-20b-GGUF', include: ['gpt-oss-safeguard-20b-Q4_K_M.gguf'] },
    downloadMb: 11086,
    languages: 'many',
    license: 'Apache-2.0',
    features: { reasoning: true },
  },
];
