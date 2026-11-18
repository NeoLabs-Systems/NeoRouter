'use strict';

// Chat models (GGUF, run by llama.cpp). Q4_K_M keeps quality close to the
// original at about a third of the size; MoE models (A3B, A4B) answer much
// faster than their size suggests.
const qwen35 = (size, downloadMb, summary, extra = {}) => ({
  id: `qwen3.5-${size.toLowerCase()}`,
  name: `Qwen3.5 ${size}`,
  kind: 'chat',
  engine: 'llama_cpp',
  summary,
  source: { repo: `unsloth/Qwen3.5-${size}-GGUF`, include: [`Qwen3.5-${size}-Q4_K_M.gguf`], projector: 'mmproj-F16.gguf' },
  downloadMb,
  languages: 'many',
  license: 'Apache-2.0',
  features: { tools: true, vision: true, reasoning: true },
  ...extra,
});

const gemma4 = (size, repoSize, file, downloadMb, summary, extra = {}) => ({
  id: `gemma-4-${size.toLowerCase().replace(' ', '-')}`,
  name: `Gemma 4 ${size}`,
  kind: 'chat',
  engine: 'llama_cpp',
  summary,
  source: { repo: `unsloth/gemma-4-${repoSize}-it-GGUF`, include: [file], projector: 'mmproj-F16.gguf' },
  downloadMb,
  languages: 'many',
  license: 'Apache-2.0',
  features: { tools: true, vision: true, reasoning: true },
  ...extra,
});

module.exports = [
  qwen35('0.8B', 704, {
    en: 'Tiny and quick. Fine for short answers, sorting and simple tasks on almost any machine.',
    de: 'Winzig und schnell. Reicht für kurze Antworten, Sortieren und einfache Aufgaben auf fast jedem Rechner.',
  }),
  qwen35('2B', 1859, {
    en: 'Small all-rounder for everyday questions when memory is tight.',
    de: 'Kleiner Allrounder für Alltagsfragen, wenn wenig Speicher frei ist.',
  }),
  qwen35('4B', 3255, {
    en: 'Good everyday assistant: writes, summarises, uses tools and reads images.',
    de: 'Guter Assistent für jeden Tag: schreibt, fasst zusammen, nutzt Werkzeuge und liest Bilder.',
  }, { recommended: true }),
  qwen35('9B', 6293, {
    en: 'Noticeably smarter for longer texts, code and harder questions.',
    de: 'Spürbar klüger bei längeren Texten, Code und schwierigeren Fragen.',
  }),
  gemma4('E2B', 'E2B', 'gemma-4-E2B-it-Q4_K_M.gguf', 3903, {
    en: 'Google’s efficient small model, made for laptops and small servers.',
    de: 'Googles sparsames kleines Modell, gemacht für Laptops und kleine Server.',
  }),
  gemma4('E4B', 'E4B', 'gemma-4-E4B-it-Q4_K_M.gguf', 5691, {
    en: 'Google’s efficient model with more knowledge; strong in many languages.',
    de: 'Googles sparsames Modell mit mehr Wissen; stark in vielen Sprachen.',
  }),
  gemma4('12B', '12b', 'gemma-4-12b-it-Q4_K_M.gguf', 6959, {
    en: 'Balanced and capable; a good main model for a server with a graphics card.',
    de: 'Ausgewogen und fähig; ein gutes Hauptmodell für einen Server mit Grafikkarte.',
  }, { recommended: true }),
  gemma4('26B A4B', '26B-A4B', 'gemma-4-26B-A4B-it-UD-Q4_K_M.gguf', 17301, {
    en: 'Large but fast: only a small part of it works on each word.',
    de: 'Groß, aber schnell: pro Wort arbeitet nur ein kleiner Teil davon.',
  }),
  gemma4('31B', '31B', 'gemma-4-31B-it-Q4_K_M.gguf', 18619, {
    en: 'Google’s largest open model, for demanding work on strong hardware.',
    de: 'Googles größtes offenes Modell, für anspruchsvolle Arbeit auf starker Hardware.',
  }),
  {
    id: 'qwen3.8-27b',
    name: 'Qwen3.8 27B',
    kind: 'chat',
    engine: 'llama_cpp',
    summary: {
      en: 'Very capable for code, analysis and long documents.',
      de: 'Sehr fähig bei Code, Analysen und langen Dokumenten.',
    },
    source: { repo: 'unsloth/Qwen3.8-27B-GGUF', include: ['Qwen3.8-27B-UD-Q4_K_M.gguf'], projector: 'mmproj-F16.gguf' },
    downloadMb: 16586,
    languages: 'many',
    license: 'Apache-2.0',
    features: { tools: true, vision: true, reasoning: true },
  },
  {
    id: 'qwen3.6-35b-a3b',
    name: 'Qwen3.6 35B A3B',
    kind: 'chat',
    engine: 'llama_cpp',
    summary: {
      en: 'Big knowledge, quick replies: only a small part of it works on each word.',
      de: 'Viel Wissen, schnelle Antworten: pro Wort arbeitet nur ein kleiner Teil davon.',
    },
    source: { repo: 'unsloth/Qwen3.6-35B-A3B-GGUF', include: ['Qwen3.6-35B-A3B-UD-Q4_K_M.gguf'], projector: 'mmproj-F16.gguf' },
    downloadMb: 21967,
    languages: 'many',
    license: 'Apache-2.0',
    features: { tools: true, vision: true, reasoning: true },
  },
  {
    id: 'gpt-oss-20b',
    name: 'gpt-oss 20B',
    kind: 'chat',
    engine: 'llama_cpp',
    summary: {
      en: 'OpenAI’s open model: thinks before it answers, good with tools.',
      de: 'OpenAIs offenes Modell: denkt vor der Antwort nach, gut mit Werkzeugen.',
    },
    source: { repo: 'ggml-org/gpt-oss-20b-GGUF', include: ['gpt-oss-20b-MXFP4.gguf'] },
    downloadMb: 11549,
    languages: 'many',
    license: 'Apache-2.0',
    features: { tools: true, reasoning: true },
  },
  {
    id: 'gpt-oss-120b',
    name: 'gpt-oss 120B',
    kind: 'chat',
    engine: 'llama_cpp',
    summary: {
      en: 'OpenAI’s largest open model, for servers with a lot of memory.',
      de: 'OpenAIs größtes offenes Modell, für Server mit sehr viel Speicher.',
    },
    source: { repo: 'ggml-org/gpt-oss-120b-GGUF', include: ['gpt-oss-120b-MXFP4.gguf'] },
    downloadMb: 60451,
    languages: 'many',
    license: 'Apache-2.0',
    features: { tools: true, reasoning: true },
  },
];
