'use strict';

// Bank accounts and payment cards.
const CARD_WORDS = ['cvv', 'cvc', 'cvn', 'cvv2', 'cvc2', 'sicherheitscode', 'prüfziffer', 'prüfnummer', 'kartenprüfnummer', 'security code', 'card code'];
const EXPIRY_WORDS = ['gültig bis', 'valid thru', 'valid through', 'exp', 'expiry', 'expires', 'expiration', 'ablaufdatum', 'läuft ab', 'ablauf'];
const AMOUNT_WORDS = ['betrag', 'eur', 'euro', 'usd', 'dollar', 'saldo', 'kontostand', 'balance', 'summe', 'total'];

module.exports = [
  {
    id: 'iban',
    label: 'IBAN',
    category: 'iban',
    pattern: /\b[A-Z]{2}\d{2}(?:\s?[A-Z0-9]{4}){2,7}(?:\s?[A-Z0-9]{1,4})?\b/g,
    validator: 'iban',
    score: 0.4,
    tests: { match: [{ text: 'IBAN DE89 3704 0044 0532 0130 00 ok', value: 'DE89 3704 0044 0532 0130 00' }, 'GB82WEST12345698765432'], ignore: ['IBAN DE89 3704 0044 0532 0130 01'] },
  },
  {
    id: 'bic',
    label: { en: 'BIC / SWIFT code', de: 'BIC / SWIFT-Code' },
    category: 'iban',
    pattern: /\b[A-Z]{4}(?:AD|AE|AT|AU|BE|BG|BR|CA|CH|CN|CY|CZ|DE|DK|EE|ES|FI|FR|GB|GR|HK|HR|HU|IE|IN|IS|IT|JP|LI|LT|LU|LV|MC|MT|MX|NL|NO|NZ|PL|PT|RO|SE|SG|SI|SK|SM|TR|UA|US|ZA)[A-Z0-9]{2}(?:[A-Z0-9]{3})?\b/g,
    score: 0.3,
    context: { positive: ['bic', 'swift', 'swift-code', 'bank', 'bankverbindung', 'iban'], window: 40 },
    tests: { match: ['BIC: COBADEFFXXX'], ignore: ['DEUTSCHE Bahn', 'COBADEFFXXX alone'] },
  },
  {
    id: 'bank_account_de',
    label: { en: 'Account number', de: 'Kontonummer' },
    category: 'iban',
    pattern: /(?<!\d)\d{5,10}(?!\d)/g,
    score: 0.2,
    context: { positive: ['kontonummer', 'konto-nr', 'kontonr', 'kto', 'kto-nr', 'account number', 'account no', 'acct', 'account #'], negative: AMOUNT_WORDS, window: 25 },
    tests: { match: ['Kontonummer 1234567890'], ignore: ['Kontostand 12345 EUR', 'order 1234567890'] },
  },
  {
    id: 'blz',
    label: { en: 'Bank code', de: 'Bankleitzahl' },
    category: 'iban',
    pattern: /(?<!\d)\d{3}\s?\d{3}\s?\d{2}(?!\d)/g,
    score: 0.2,
    context: { positive: ['blz', 'bankleitzahl', 'sort code'], window: 25 },
    tests: { match: ['BLZ 370 400 44'], ignore: ['370 400 44 Stück'] },
  },
  {
    id: 'us_routing',
    label: { en: 'Routing number', de: 'US-Bankleitzahl (ABA)' },
    category: 'iban',
    pattern: /(?<!\d)\d{9}(?!\d)/g,
    validator: 'abaRouting',
    score: 0,
    context: { positive: ['routing', 'aba', 'rtn', 'routing number', 'routing no'], window: 30 },
    tests: { match: ['routing number 021000021'], ignore: ['ticket 021000021', 'routing number 021000022'] },
  },
  {
    id: 'card',
    label: { en: 'Card number', de: 'Kartennummer' },
    category: 'card',
    pattern: /\b\d(?:[ -]?\d){12,18}\b/g,
    validator: 'card',
    score: 0.4,
    tests: { match: ['card 4111 1111 1111 1111', '5500-0000-0000-0004'], ignore: ['order 4111 1111 1111 1112'] },
  },
  {
    id: 'card_cvv',
    label: { en: 'Card security code', de: 'Kartenprüfnummer' },
    category: 'card',
    pattern: /(?<![\d])\d{3,4}(?![\d])/g,
    score: 0.2,
    context: { positive: CARD_WORDS, window: 20 },
    tests: { match: ['CVV: 123', 'Prüfziffer 4321'], ignore: ['room 123'] },
  },
  {
    id: 'card_expiry',
    label: { en: 'Card expiry date', de: 'Ablaufdatum der Karte' },
    category: 'card',
    pattern: /\b(?:0[1-9]|1[0-2])\s?\/\s?(?:20\d{2}|\d{2})\b/g,
    score: 0.2,
    context: { positive: EXPIRY_WORDS, window: 25 },
    tests: { match: ['valid thru 09/28', 'gültig bis 11/2027'], ignore: ['ratio 09/28'] },
  },
];
