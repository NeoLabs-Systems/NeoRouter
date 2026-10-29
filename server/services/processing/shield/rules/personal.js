'use strict';

// Dates of birth, places and vehicles: harmless alone, identifying together.
const MONTHS_DE_EN = 'Jan(?:uar|uary)?|Feb(?:ruar|ruary)?|Mär(?:z)?|Maerz|Mar(?:ch)?|Apr(?:il)?|Mai|May|June?|Juni|July?|Juli|Aug(?:ust)?|Sep(?:t(?:ember)?)?|Okt(?:ober)?|Oct(?:ober)?|Nov(?:ember)?|Dez(?:ember)?|Dec(?:ember)?';
const DAY = '(?:0?[1-9]|[12]\\d|3[01])';
const BIRTH_WORDS = ['geb.', 'geb', 'geboren', 'geburtsdatum', 'geburtstag', 'dob', 'd.o.b.', 'born', 'date of birth', 'birth date', 'birthdate', 'birthday', 'né le', 'née le', 'fecha de nacimiento', 'data di nascita'];
const GEO_WORDS = ['lat', 'lon', 'lng', 'latitude', 'longitude', 'koordinaten', 'coordinates', 'gps', 'standort', 'location', 'position', 'geo', 'maps', 'wohnort', 'ort'];
const PLATE_WORDS = ['kennzeichen', 'nummernschild', 'kfz', 'kfz-kennzeichen', 'auto', 'fahrzeug', 'pkw', 'lkw', 'wagen', 'license plate', 'licence plate', 'number plate', 'plate', 'amtliches kennzeichen'];
const VIN_WORDS = ['vin', 'fin', 'fahrgestellnummer', 'fahrzeug-identifizierungsnummer', 'fahrzeugidentifizierungsnummer', 'chassis', 'chassis number', 'vehicle identification'];

module.exports = [
  {
    id: 'birthdate',
    label: { en: 'Date of birth', de: 'Geburtsdatum' },
    category: 'birthdate',
    pattern: new RegExp([
      `${DAY}[./-](?:0?[1-9]|1[0-2])[./-](?:19|20)\\d{2}`,
      '(?:19|20)\\d{2}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\\d|3[01])',
      `${DAY}\\.?\\s+(?:${MONTHS_DE_EN})\\.?\\s+(?:19|20)\\d{2}`,
      `(?:${MONTHS_DE_EN})\\.?\\s+${DAY}(?:st|nd|rd|th)?,?\\s+(?:19|20)\\d{2}`,
    ].map((part) => `(?<![\\p{L}\\p{N}])${part}(?![\\p{L}\\p{N}])`).join('|'), 'giu'),
    score: 0.2,
    context: { positive: BIRTH_WORDS, window: 30 },
    tests: { match: ['geb. 12.03.1985', 'Date of birth: March 12, 1985', 'geboren am 3. Mai 1990'], ignore: ['Termin am 12.03.2026', 'released 2026-10-29'] },
  },
  {
    id: 'gps_decimal',
    label: { en: 'GPS coordinates', de: 'GPS-Koordinaten' },
    category: 'location',
    pattern: /(?<![\d.])-?\d{1,2}\.\d{4,}\s*,\s*-?\d{1,3}\.\d{4,}(?![\d.])/g,
    accept: 'coordinates',
    score: 0.3,
    context: { positive: GEO_WORDS, window: 40 },
    tests: { match: ['Standort: 52.520008, 13.404954', 'https://maps.example/@48.13743,11.57549,15z'], ignore: ['weights 0.1234, 0.5678', 'GPS 95.12345, 13.40495'] },
  },
  {
    id: 'gps_hemisphere',
    label: { en: 'GPS coordinates', de: 'GPS-Koordinaten' },
    category: 'location',
    pattern: /(?<![\d.])\d{1,2}(?:\.\d{3,}°?|°\s?\d{1,2}['′](?:\s?\d{1,2}(?:[.,]\d+)?["″])?)\s?[NS],?\s*\d{1,3}(?:\.\d{3,}°?|°\s?\d{1,2}['′](?:\s?\d{1,2}(?:[.,]\d+)?["″])?)\s?[EWO](?![\p{L}])/gu,
    score: 0.8,
    tests: { match: ['52.5200° N, 13.4050° E', '52°31\'12" N 13°24\'18" E'], ignore: ['5 N, 10 E'] },
  },
  {
    id: 'plus_code',
    label: { en: 'Plus Code location', de: 'Plus-Code-Standort' },
    category: 'location',
    keywords: ['+'],
    pattern: /\b[23456789CFGHJMPQRVWX]{8}\+[23456789CFGHJMPQRVWX]{2,3}\b/g,
    score: 0.6,
    tests: { match: ['meet at 9F4MGC22+22'], ignore: ['C++ code'] },
  },
  {
    id: 'de_number_plate',
    label: { en: 'Number plate', de: 'Kfz-Kennzeichen' },
    category: 'vehicle',
    pattern: /(?<![\p{L}\p{N}])[A-ZÄÖÜ]{1,3}[- ][A-Z]{1,2}[- ]?[1-9]\d{0,3}[EH]?(?![\p{L}\p{N}])/gu,
    score: 0.2,
    context: { positive: PLATE_WORDS, window: 30 },
    tests: { match: ['Kennzeichen: M-AB 1234', 'Auto B XY 99E'], ignore: ['Typ M-AB 1234'] },
  },
  {
    id: 'vin_checked',
    label: { en: 'Vehicle identification number', de: 'Fahrgestellnummer' },
    category: 'vehicle',
    pattern: /\b[A-HJ-NPR-Z0-9]{17}\b/g,
    accept: 'mixedAlnum',
    validator: 'vinCheck',
    score: 0.3,
    tests: { match: ['1HGCM82633A004352'], ignore: ['1HGCM82633A004353'] },
  },
  {
    id: 'vin',
    label: { en: 'Vehicle identification number', de: 'Fahrgestellnummer' },
    category: 'vehicle',
    pattern: /\b[A-HJ-NPR-Z0-9]{17}\b/g,
    accept: 'mixedAlnum',
    score: 0.2,
    context: { positive: VIN_WORDS, window: 30 },
    tests: { match: ['FIN: WVWZZZ1JZXW000001'], ignore: ['Artikel WVWZZZ1JZXW000001'] },
  },
];
