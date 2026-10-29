'use strict';

// Postal addresses. Each part (street and number, postcode and town, PO box)
// has its own pattern and score; parts that stand next to each other are
// merged into one address and confirm each other. Weak parts (a postcode
// and town alone, "Am Markt 3") only count next to a strong part or a word
// like "Adresse".
const STREET_SUFFIX = 'straße|strasse|str\\.|weg|gasse|allee|platz|ring|damm|ufer|chaussee|steig|pfad|stieg|promenade|graben|zeile';
const STREET_WORD = 'Straße|Strasse|Str\\.|Weg|Allee|Platz|Ring|Damm|Ufer|Chaussee|Gasse|Markt|Steig';
const NUMBER = '\\d{1,4}\\s?[a-zA-Z]?(?:\\s?[-/]\\s?\\d{1,4}[a-zA-Z]?)?';
const END = '(?![\\p{L}\\p{N}])';
const START = '(?<![\\p{L}\\p{N}])';

const PARTS = [
  { score: 0.6, pattern: new RegExp(`${START}\\p{Lu}[\\p{L}.-]*?(?:${STREET_SUFFIX}|-(?:${STREET_WORD}))\\s?${NUMBER}${END}`, 'gu') },
  { score: 0.6, pattern: new RegExp(`${START}(?!(?:Der|Die|Das|Den|Dem|Des|Ein|Eine|Einer|Jeder|Jede|Unser|Unsere|Dieser|Diese|Welche|Welcher)\\s)\\p{Lu}[\\p{L}-]*(?:er|e|es|en)\\s+(?:${STREET_WORD})\\s?${NUMBER}${END}`, 'gu') },
  { score: 0.3, pattern: new RegExp(`${START}(?:Am|An der|An den|Auf der|Auf dem|Im|In der|In den|Zum|Zur|Unter den|Hinter der|Vor dem|Alt)\\s+\\p{Lu}[\\p{L}-]+\\s${NUMBER}${END}`, 'gu') },
  { score: 0.7, pattern: new RegExp(`${START}\\d{1,5}\\s+(?:[NSEW]\\.?\\s+)?(?:\\p{Lu}[\\p{L}'-]*\\s+){1,3}(?:Street|St\\.|Avenue|Ave\\.?|Road|Rd\\.?|Lane|Ln\\.?|Drive|Dr\\.|Boulevard|Blvd\\.?|Court|Ct\\.?|Place|Pl\\.?|Way|Terrace|Parkway|Square|Close|Crescent)(?![\\p{L}])`, 'gu') },
  { score: 0.6, pattern: /(?:Postfach|P\.?\s?O\.?\s?Box)\s+\d{1,6}/giu },
  { score: 0.3, pattern: new RegExp(`${START}(?:D-|A-|CH-)?\\d{4,5}\\s+\\p{Lu}[\\p{Ll}ß]+(?:[- ](?:an der|am|im|bei|ob der|\\p{Lu}[\\p{Ll}ß]+|\\(\\p{L}+\\)))*(?![\\p{L}])`, 'gu') },
  { score: 0.6, pattern: /(?<![\p{L}])\p{Lu}[\p{L}]+(?:\s\p{Lu}[\p{L}]+)*,\s[A-Z]{2}\s\d{5}(?:-\d{4})?(?!\d)/gu },
  { score: 0.3, pattern: /\b(?:[A-Z]{1,2}\d[A-Z\d]?|GIR)\s?\d[A-Z]{2}\b/g },
];
const CONTEXT = /(?<![\p{L}])(?:wohne|wohnhaft|wohnt|adresse|anschrift|address|lives at|live at|lieferadresse|rechnungsadresse|shipping|billing|postanschrift|meldeadresse|zustellung|ship to|send to)(?![\p{L}])/iu;
const JOIN = /^[ \t]*[,\n]?[ \t]*\n?[ \t]*$/;

function scan(text) {
  const parts = [];
  for (const { score, pattern } of PARTS) {
    for (const match of text.matchAll(pattern)) parts.push({ start: match.index, end: match.index + match[0].length, score });
  }
  parts.sort((a, b) => a.start - b.start || b.end - a.end);
  const merged = [];
  for (const part of parts) {
    const last = merged.at(-1);
    if (last && part.start < last.end) {
      last.end = Math.max(last.end, part.end);
      last.score = Math.max(last.score, part.score);
    } else if (last && JOIN.test(text.slice(last.end, part.start))) {
      last.end = part.end;
      last.score = Math.max(last.score, part.score) + 0.3;
    } else {
      merged.push({ ...part });
    }
  }
  for (const span of merged) {
    if (CONTEXT.test(text.slice(Math.max(0, span.start - 40), span.start))) span.score += 0.3;
  }
  return merged;
}

module.exports = {
  id: 'postal_address',
  label: { en: 'Postal address', de: 'Postanschrift' },
  category: 'address',
  scan,
  tests: {
    match: [
      { text: 'Ich wohne in der Hauptstraße 5, 10115 Berlin.', value: 'Hauptstraße 5, 10115 Berlin' },
      { text: 'Lieferung an Berliner Straße 12a', value: 'Berliner Straße 12a' },
      { text: 'Adresse: Am Markt 3', value: 'Am Markt 3' },
      { text: 'Ship to 1600 Pennsylvania Avenue, Washington, DC 20500', value: '1600 Pennsylvania Avenue, Washington, DC 20500' },
      { text: 'Karl-Marx-Allee 34\n10178 Berlin', value: 'Karl-Marx-Allee 34\n10178 Berlin' },
    ],
    ignore: ['Im Jahr 2026', 'Der Platz 3 ging an uns', '10115 Berlin ist schön', 'Kapitel 3'],
  },
};
