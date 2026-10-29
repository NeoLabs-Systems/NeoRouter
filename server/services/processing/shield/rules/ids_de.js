'use strict';

// German personal numbers. Those with a check digit are validated; the rest
// only count next to a word that names them.
const ID_CARD_WORDS = ['ausweis', 'personalausweis', 'ausweisnummer', 'ausweis-nr', 'id card', 'id-card', 'identity card', 'perso', 'dokumentennummer'];
const PASSPORT_WORDS = ['reisepass', 'pass-nr', 'passnummer', 'passport', 'passport number', 'passport no', 'reisepassnummer'];

module.exports = [
  {
    id: 'de_tax_id',
    label: { en: 'German tax ID', de: 'Steuer-ID' },
    category: 'id',
    pattern: /(?<![\d])[1-9]\d\s?\d{3}\s?\d{3}\s?\d{3}(?![\d])/g,
    validator: 'steuerId',
    score: 0.3,
    context: { positive: ['steuer-id', 'steuerid', 'steuer-identifikationsnummer', 'identifikationsnummer', 'idnr', 'tin', 'tax id'], window: 40 },
    tests: { match: ['Steuer-ID: 86 095 742 719', '86095742719'], ignore: ['86095742718', '11111111111'] },
  },
  {
    id: 'de_vat_id',
    label: { en: 'German VAT ID', de: 'USt-IdNr.' },
    category: 'id',
    pattern: /\bDE\s?\d{9}\b/g,
    validator: 'ustIdDe',
    score: 0.4,
    tests: { match: ['USt-IdNr. DE136695976'], ignore: ['DE136695975'] },
  },
  {
    id: 'de_tax_number',
    label: { en: 'German tax number', de: 'Steuernummer' },
    category: 'id',
    pattern: /(?<![\d/])\d{2,3}\/\d{3,4}\/\d{4,5}(?![\d/])|(?<!\d)\d{13}(?!\d)/g,
    score: 0.2,
    context: { positive: ['steuernummer', 'st.-nr', 'stnr', 'steuer-nr', 'st-nr', 'finanzamt'], window: 30 },
    tests: { match: ['Steuernummer 21/815/08150'], ignore: ['Seite 21/815/08150'] },
  },
  {
    id: 'de_pension_number',
    label: { en: 'German pension insurance number', de: 'Rentenversicherungsnummer' },
    category: 'id',
    pattern: /\b\d{2}\s?[0-3]\d[01]\d{3}\s?[A-Z]\s?\d{2}\s?\d\b/g,
    validator: 'rvnr',
    score: 0.3,
    tests: { match: ['RV-Nr. 12 150385 M 01 4', '65 170839 J 00 3'], ignore: ['12 150385 M 01 5'] },
  },
  {
    id: 'de_health_insurance_number',
    label: { en: 'German health insurance number', de: 'Krankenversichertennummer' },
    category: 'id',
    pattern: /\b[A-Z]\d{9}\b/g,
    validator: 'kvnr',
    score: 0,
    context: { positive: ['versichertennummer', 'krankenversichertennummer', 'kvnr', 'krankenversicherung', 'krankenkasse', 'gesundheitskarte', 'versicherten-nr', 'versichertennr', 'insurance number'], window: 40 },
    tests: { match: ['Versichertennummer A123456780'], ignore: ['Artikel A123456780', 'Versichertennummer A123456781'] },
  },
  {
    id: 'de_id_card',
    label: { en: 'German ID card number', de: 'Personalausweisnummer' },
    category: 'id',
    pattern: /\b[CFGHJKLMNPRTVWXYZ][CFGHJKLMNPRTVWXYZ0-9]{8}\d\b/g,
    validator: 'idCardDe',
    score: 0,
    context: { positive: ID_CARD_WORDS, window: 40 },
    tests: { match: ['Ausweisnummer T220001293'], ignore: ['Seriennummer T220001293'] },
  },
  {
    id: 'de_document_number',
    label: { en: 'ID card or passport number', de: 'Ausweis- oder Passnummer' },
    category: 'id',
    pattern: /\b[CFGHJKLMNPRTVWXYZ][CFGHJKLMNPRTVWXYZ0-9]{8}\b/g,
    accept: 'mixedAlnum',
    score: 0.2,
    context: { positive: [...ID_CARD_WORDS, ...PASSPORT_WORDS], window: 40 },
    tests: { match: ['Reisepass C01X00T47'], ignore: ['Modell C01X00T47'] },
  },
  {
    id: 'de_driving_licence',
    label: { en: 'Driving licence number', de: 'Führerscheinnummer' },
    category: 'id',
    pattern: /\b[A-Z0-9]{11}\b/g,
    accept: 'mixedAlnum',
    score: 0.2,
    context: { positive: ['führerschein', 'fuehrerschein', 'führerscheinnummer', 'driving licence', 'driving license', 'driver license', "driver's license", 'fahrerlaubnis'], window: 40 },
    tests: { match: ['Führerschein B072RRE2I55'], ignore: ['Produkt B072RRE2I55'] },
  },
];
