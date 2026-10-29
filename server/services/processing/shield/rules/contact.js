'use strict';

// Ways to reach a person or a device: email, phone, network addresses.
module.exports = [
  {
    id: 'email',
    label: { en: 'Email address', de: 'E-Mail-Adresse' },
    category: 'email',
    keywords: ['@'],
    pattern: /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b/g,
    score: 0.9,
    tests: { match: ['Mail anna.schmidt@example.de now'], ignore: ['@channel see the docs'] },
  },
  {
    id: 'phone',
    label: { en: 'Phone number', de: 'Telefonnummer' },
    category: 'phone',
    pattern: /(?<![\w+])(?:\+|00)?\d[\d\s()./-]{6,20}\d(?!\w)/g,
    accept: 'phone',
    score: 0.6,
    context: { negative: ['order', 'bestellung', 'bestellnummer', 'auftrag', 'rechnung', 'rechnungsnummer', 'invoice', 'artikel', 'ticket', 'kundennummer', 'customer number'], window: 25 },
    tests: { match: [{ text: 'call +49 170 1234567 today', value: '+49 170 1234567' }, '(030) 1234 5678'], ignore: ['in 2026 we sold 1500 units', 'released 2026-10-29', 'version 1.20.3', 'Order 4711-0815'] },
  },
  {
    id: 'ipv4',
    label: { en: 'IPv4 address', de: 'IPv4-Adresse' },
    category: 'ip',
    pattern: /\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}\b/g,
    score: 0.6,
    allow: { regexes: ['^127\\.', '^0\\.0\\.0\\.0$'] },
    tests: { match: ['server 10.0.4.17 down'], ignore: ['listen on 127.0.0.1', 'bind 0.0.0.0'] },
  },
  {
    id: 'ipv6',
    label: { en: 'IPv6 address', de: 'IPv6-Adresse' },
    category: 'ip',
    keywords: [':'],
    pattern: /(?<![\w:.])(?:[0-9A-Fa-f]{0,4}:){2,7}[0-9A-Fa-f]{0,4}(?![\w:])/g,
    accept: 'ipv6',
    score: 0.6,
    tests: { match: ['host 2a00:1450:4001:82a::200e answered', 'fe80::1ff:fe23:4567:890a%eth0'], ignore: ['meet at 12:30:45', 'std::vector', 'see 2001:db8::1 in the docs'] },
  },
  {
    id: 'mac_address',
    label: { en: 'MAC address', de: 'MAC-Adresse' },
    category: 'ip',
    pattern: /\b[0-9A-Fa-f]{2}([:-])(?:[0-9A-Fa-f]{2}\1){4}[0-9A-Fa-f]{2}\b|\b[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}\b/g,
    score: 0.7,
    allow: { regexes: ['^(?:00[:-]){5}00$', '^(?:ff[:-]){5}ff$'] },
    tests: { match: ['wifi 3C:22:FB:9A:10:7E', 'port 0023.ac4f.1b2c'], ignore: ['broadcast ff:ff:ff:ff:ff:ff'] },
  },
];
