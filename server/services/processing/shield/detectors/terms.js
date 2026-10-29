'use strict';

// The user's own terms, and entities found by the optional AI pass. Both are
// matched as exact text; neither runs from the prefilter.
const { wordList, escapeRegExp } = require('../registry');

const compiled = new Map();
function termRegex(term) {
  if (!compiled.has(term)) {
    if (compiled.size > 5000) compiled.clear();
    compiled.set(term, new RegExp(`(?<![\\p{L}\\p{N}])${escapeRegExp(term)}(?![\\p{L}\\p{N}])`, 'giu'));
  }
  return compiled.get(term);
}

function exact(text, value) {
  const spans = [];
  let index = text.indexOf(value);
  while (index >= 0) {
    spans.push({ start: index, end: index + value.length, value });
    index = text.indexOf(value, index + value.length);
  }
  return spans;
}

// First names may stay: a full name from the AI keeps its leading first names.
function withoutFirstNames(name) {
  const firstNames = wordList('first_names');
  const parts = name.split(/\s+/);
  let index = 0;
  while (index < parts.length - 1 && firstNames.has(parts[index].toLowerCase())) index += 1;
  if (parts.length === 1 && firstNames.has(parts[0].toLowerCase())) return '';
  return parts.slice(index).join(' ');
}

module.exports = [
  {
    id: 'custom_terms',
    label: { en: 'Your private terms', de: 'Deine privaten Begriffe' },
    category: 'custom',
    scan(text, { customTerms = [] }) {
      const spans = [];
      for (const term of customTerms) {
        for (const match of text.matchAll(termRegex(term))) spans.push({ start: match.index, end: match.index + match[0].length });
      }
      return spans;
    },
  },
  {
    id: 'ai_entities',
    label: { en: 'AI detection', de: 'KI-Erkennung' },
    // The category comes from each entity; this one is only the default.
    category: 'person',
    scan(text, { entities = [] }) {
      const spans = [];
      for (const entity of entities) {
        const value = entity.category === 'person' ? withoutFirstNames(entity.text.trim()) : entity.text;
        if (value.length < 2) continue;
        for (const span of exact(text, value)) spans.push({ ...span, category: entity.category });
      }
      return spans;
    },
  },
];
