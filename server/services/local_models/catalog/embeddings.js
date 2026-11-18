'use strict';

// Embedding models (GGUF, llama.cpp in embedding mode): turn text into
// vectors for search and similarity. Small, so they run well even on CPUs.
module.exports = [
  {
    id: 'embeddinggemma-300m',
    name: 'EmbeddingGemma 300M',
    kind: 'embedding',
    engine: 'llama_cpp',
    summary: {
      en: 'Tiny and accurate, for search over your own documents in many languages.',
      de: 'Winzig und genau, für die Suche in eigenen Dokumenten in vielen Sprachen.',
    },
    source: { repo: 'ggml-org/embeddinggemma-300M-GGUF', include: ['embeddinggemma-300M-Q8_0.gguf'] },
    downloadMb: 314,
    languages: 'many',
    license: 'Gemma',
    recommended: true,
  },
  {
    id: 'qwen3-embedding-0.6b',
    name: 'Qwen3 Embedding 0.6B',
    kind: 'embedding',
    engine: 'llama_cpp',
    summary: {
      en: 'Strong all-round embeddings that also handle long texts and code.',
      de: 'Starke Embeddings für alles, auch für lange Texte und Code.',
    },
    source: { repo: 'Qwen/Qwen3-Embedding-0.6B-GGUF', include: ['Qwen3-Embedding-0.6B-Q8_0.gguf'] },
    downloadMb: 609,
    languages: 'many',
    license: 'Apache-2.0',
  },
  {
    id: 'qwen3-embedding-4b',
    name: 'Qwen3 Embedding 4B',
    kind: 'embedding',
    engine: 'llama_cpp',
    summary: {
      en: 'Top search quality when the server has memory to spare.',
      de: 'Beste Suchqualität, wenn der Server genug Speicher frei hat.',
    },
    source: { repo: 'Qwen/Qwen3-Embedding-4B-GGUF', include: ['Qwen3-Embedding-4B-Q4_K_M.gguf'] },
    downloadMb: 2381,
    languages: 'many',
    license: 'Apache-2.0',
  },
  {
    id: 'nomic-embed-text-v1.5',
    name: 'Nomic Embed Text v1.5',
    kind: 'embedding',
    engine: 'llama_cpp',
    summary: {
      en: 'Very light English embeddings, fast even on small machines.',
      de: 'Sehr leichte englische Embeddings, schnell auch auf kleinen Rechnern.',
    },
    source: { repo: 'nomic-ai/nomic-embed-text-v1.5-GGUF', include: ['nomic-embed-text-v1.5.Q8_0.gguf'] },
    downloadMb: 139,
    languages: ['en'],
    license: 'Apache-2.0',
  },
];
