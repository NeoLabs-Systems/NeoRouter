'use strict';

// Last names, without a model. First names may stay in the text; only the
// family name is hidden ("Anna [LASTNAME_1]", "Herr [LASTNAME_2]").
//
// A capitalised word counts as a last name when
//   - it follows a known first name (and optional middle names, particles
//     like "von der"):                                      0.4
//       + it is a known last name                            +0.3
//       + it is no common word (an unknown name)             +0.2
//       - it is a common word ("Max Bescheid")               -0.5
//       - the first name is itself a common word ("Will")    -0.3
//       + a greeting, introduction or sign-off is in front   +0.3
//   - it follows a form of address ("Frau", "Mr.", "Dr."):   0.6
//       + it is a known last name                            +0.2
//       - it is a common word and no known name ("Herr Ober") -0.5
//   - it is a known last name in "Schmidt, Anna" form:       0.7
// Names typed in lowercase or capitals ("ich bin anna schmidt", "HERR MÜLLER")
// count too, but only from the name lists, never when the word is a frequent
// or everyday word ("in", "will", "das"), and only after a greeting,
// introduction, form of address or sign-off (-0.3 without the capital):
// many last names are also words ("stark", "waters"), and without the capital
// letter nothing else sets them apart.
// The engine keeps spans scoring at least 0.5.
const { wordList } = require('../registry');

const WORD = /(?<![\p{L}\p{N}_@./\\#-])\p{L}[\p{L}\p{M}]*(?:['’-]\p{L}[\p{L}\p{M}]+)*(?![\p{L}\p{N}_@])/gu;
// "Anna", "McDonald", "Müller-Lüdenscheid"; not "anna" or "ANNA".
const CAPITALISED = /^\p{Lu}[\p{Ll}\p{M}]/u;
const PARTICLES = new Set(['von', 'van', 'de', 'der', 'den', 'del', 'della', 'di', 'da', 'du', 'dos', 'das', 'le', 'la', 'zu', 'vom', 'zum', 'ter', 'ten', 'af', 'bin', 'ibn', 'al', 'el', 'mac']);
const SAME_LINE_GAP = /^[ \t ]{1,3}$/;
const PARTICLE_GROUP = `(?:${[...PARTICLES].join('|')})`;
// A form of address right before a word, optionally with particles: "Frau Dr. ", "Herr von ".
const ADDRESSED = new RegExp(`(?:^|[^\\p{L}])(?:herrn?|frau|hr\\.|fr\\.|dr\\.|prof\\.|mr\\.?|mrs\\.?|ms\\.?|mx\\.?|miss|sir|madame|monsieur|mme\\.?|mlle\\.?|signor|signora|señor|señora|sr\\.|sra\\.)(?:\\s+(?:dr|prof|med|ing|dipl\\.-ing|rer\\.\\s?nat)\\.?)*\\s+((?:${PARTICLE_GROUP}\\s+)*)$`, 'u');
const INTRO = /(?:^|[^\p{L}])(?:hallo|hi|hey|moin|servus|dear|liebe[rs]?|sehr geehrte[rs]?|guten (?:morgen|tag|abend)|name(?:\s+(?:ist|is|lautet))?\s*:?|namens|heiße|heisse|ich bin|i am|i'm|this is|hier ist|ansprechpartner(?:in)?:?|kontakt:?|contact:?|gez\.|signed|cc:?)[\s,:]*$/u;
const SIGN_OFF = /(?:grüße|grüßen|gruß|gruss|grüsse|regards|best|cheers|thanks|thank you|danke|sincerely|mfg|lg|vg|bg)[\s,!.]*\n[ \t]*$/u;

const isWordGap = (text, from, to) => SAME_LINE_GAP.test(text.slice(from, to));

// Every word except lowercase particles, which stay in the gaps ("von der").
function words(text) {
  return [...text.matchAll(WORD)]
    .filter((match) => !PARTICLES.has(match[0]))
    .map((match) => ({ start: match.index, end: match.index + match[0].length, text: match[0], lower: match[0].toLowerCase(), capitalised: CAPITALISED.test(match[0]) }));
}

// Membership in a name list; without a capital letter only for words that
// are neither frequent nor everyday words.
function inList(word, list, lists) {
  if (!list.has(word.lower)) return false;
  return word.capitalised || (!lists.frequent.has(word.lower) && !lists.common.has(word.lower));
}

// Particles between two words on the same line ("von der"), or null.
function particlesBetween(text, from, to) {
  const gap = text.slice(from, to);
  if (SAME_LINE_GAP.test(gap)) return { start: to };
  const parts = gap.trim().split(/[ \t]+/);
  if (!/^[ \t]+/.test(gap) || !/[ \t]+$/.test(gap) || /\n/.test(gap)) return null;
  if (!parts.length || !parts.every((part) => PARTICLES.has(part.toLowerCase()))) return null;
  return { start: from + gap.indexOf(parts[0]) };
}

function lastNameScore(word, lists) {
  if (inList(word, lists.last, lists)) return 0.3;
  if (!word.capitalised) return -1;
  return lists.common.has(word.lower) ? -0.5 : 0.2;
}

function scan(text) {
  const lists = { first: wordList('first_names'), last: wordList('last_names'), common: wordList('common_words'), frequent: wordList('frequent_words') };
  const isFirst = (word) => inList(word, lists.first, lists);
  const isLast = (word) => inList(word, lists.last, lists);
  const list = words(text);
  const spans = [];
  const before = (start) => text.slice(Math.max(0, start - 48), start).toLowerCase();
  for (let index = 0; index < list.length; index += 1) {
    const word = list[index];
    const startsName = isFirst(word) && !(isLast(word) && index > 0 && isFirst(list[index - 1]) && isWordGap(text, list[index - 1].end, word.start));
    if (startsName) {
      // Skip up to two middle names, then expect the last name on the same line.
      let next = index + 1;
      while (next < list.length && next - index <= 2 && isFirst(list[next]) && !isLast(list[next])
        && next + 1 < list.length && isWordGap(text, list[next - 1].end, list[next].start)) next += 1;
      const surname = list[next];
      const join = surname && particlesBetween(text, list[next - 1].end, surname.start);
      if (!surname || !join) continue;
      if (isFirst(surname) && !isLast(surname)) continue;
      let score = 0.4 + lastNameScore(surname, lists);
      if (lists.common.has(word.lower) && !isLast(surname)) score -= 0.3;
      if (!word.capitalised || !surname.capitalised) score -= 0.3;
      const prefix = before(word.start);
      if (ADDRESSED.test(prefix) || INTRO.test(prefix) || SIGN_OFF.test(text.slice(Math.max(0, word.start - 48), word.start))) score += 0.3;
      spans.push({ start: join.start, end: surname.end, score });
      index = next;
      continue;
    }
    const prefix = before(word.start);
    const addressed = prefix.match(ADDRESSED);
    if (addressed) {
      const known = isLast(word);
      if (!known && !word.capitalised) continue;
      const score = 0.6 + (known ? 0.2 : lists.common.has(word.lower) ? -0.5 : 0);
      const particles = addressed[1] || '';
      spans.push({ start: word.start - particles.length, end: word.end, score });
      continue;
    }
    // "Schmidt, Anna"
    const following = list[index + 1];
    if (isLast(word) && !lists.first.has(word.lower) && following && /^,[ \t]?$/.test(text.slice(word.end, following.start)) && isFirst(following)) {
      spans.push({ start: word.start, end: word.end, score: 0.7 });
    }
  }
  return spans;
}

module.exports = {
  id: 'last_names',
  label: { en: 'Last name', de: 'Nachname' },
  category: 'person',
  scan,
  tests: {
    match: [
      { text: 'Hallo, ich bin Anna Schmidt.', value: 'Schmidt' },
      { text: 'Termin mit Frau Schneider morgen', value: 'Schneider' },
      { text: 'Dear Mr. Thompson,', value: 'Thompson' },
      { text: 'Gespräch mit Richard von Weizsäcker', value: 'von Weizsäcker' },
      { text: 'Viele Grüße\nMaria Müller-Lüdenscheid', value: 'Müller-Lüdenscheid' },
      { text: 'Teilnehmer: Schmidt, Anna', value: 'Schmidt' },
      { text: 'Mike Wojciechowski called', value: 'Wojciechowski' },
      { text: 'hallo, ich bin anna schmidt', value: 'schmidt' },
      { text: 'grüße\njonas becker', value: 'becker' },
      { text: 'herr müller hat angerufen', value: 'müller' },
      { text: 'NAME: ANNA SCHMIDT', value: 'SCHMIDT' },
    ],
    ignore: ['Max Bescheid geben', 'Hallo Anna, wie geht es?', 'Grace Period ends', 'Frau Doktor, bitte', 'Anna Maria kommt', 'Anna in Berlin', 'will you do it so', 'ich weiß das', 'paul hat das gemacht', 'talked to joan waters about it', 'frau doktor, bitte', 'ICH BIN DA'],
  },
};
