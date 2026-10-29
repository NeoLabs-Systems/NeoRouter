'use strict';

// Wallet recovery phrases: 12 to 24 consecutive words from the BIP-39
// wordlist, optionally numbered ("1. abandon 2. ability …"). A phrase whose
// built-in checksum is right is certain; one with the right length but a
// wrong checksum is still hidden, because a typo does not make it harmless.
const crypto = require('node:crypto');
const { wordList } = require('../registry');

const LENGTHS = new Set([12, 15, 18, 21, 24]);
const WORD = /[a-z]+/g;
const SEPARATOR = /^[\s,;]*(?:\d{1,2}[.):]?\s*)?$/;

let indexOf = null;
function wordIndex() {
  if (!indexOf) indexOf = new Map([...wordList('bip39_english')].map((word, index) => [word, index]));
  return indexOf;
}

function checksumValid(words) {
  const index = wordIndex();
  const bits = words.map((word) => index.get(word).toString(2).padStart(11, '0')).join('');
  const checksumBits = words.length / 3;
  const entropyBits = bits.slice(0, bits.length - checksumBits);
  const entropy = Buffer.from(entropyBits.match(/.{8}/g).map((byte) => parseInt(byte, 2)));
  const hash = crypto.createHash('sha256').update(entropy).digest();
  return hash[0].toString(2).padStart(8, '0').slice(0, checksumBits) === bits.slice(-checksumBits);
}

function scan(text) {
  const index = wordIndex();
  const lower = text.toLowerCase();
  const spans = [];
  let run = [];
  const close = () => {
    if (run.length >= 12) {
      const words = run.map((match) => match[0]);
      const score = LENGTHS.has(words.length) ? (checksumValid(words) ? 1 : 0.7) : 0.6;
      spans.push({ start: run[0].index, end: run.at(-1).index + run.at(-1)[0].length, score });
    }
    run = [];
  };
  for (const match of lower.matchAll(WORD)) {
    const previous = run.at(-1);
    const isWord = index.has(match[0]) && (match.index === 0 || !/[a-z]/i.test(text[match.index - 1]));
    if (isWord && (!previous || SEPARATOR.test(lower.slice(previous.index + previous[0].length, match.index)))) {
      run.push(match);
    } else {
      close();
      if (isWord) run.push(match);
    }
  }
  close();
  return spans;
}

module.exports = {
  id: 'seed_phrase',
  label: { en: 'Wallet recovery phrase', de: 'Wiederherstellungsphrase einer Wallet' },
  category: 'secret',
  scan,
  tests: {
    match: ['my seed: abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about', '1. legal 2. winner 3. thank 4. year 5. wave 6. sausage 7. worth 8. useful 9. legal 10. winner 11. thank 12. yellow'],
    ignore: ['Please review the budget and approve the final plan for the next quarter today.'],
  },
};
