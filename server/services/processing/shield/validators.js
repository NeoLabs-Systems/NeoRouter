'use strict';

// Checks that turn "looks like" into "is": checksums, check digits and
// structure tests. Rules name them by key (`validator: 'iban'`). A failed
// check drops the match, a passed one raises its score. All are pure and fast.
const crypto = require('node:crypto');
const net = require('node:net');

const digitsOf = (value) => String(value).replace(/\D/g, '');
const compactOf = (value) => String(value).replace(/[\s.\-/]/g, '').toUpperCase();
const crossSum = (number) => String(number).split('').reduce((sum, digit) => sum + Number(digit), 0);
const letterNumber = (char) => char.charCodeAt(0) - 64; // A=1 … Z=26

function luhn(digits) {
  let sum = 0;
  for (let index = 0; index < digits.length; index += 1) {
    let digit = Number(digits[digits.length - 1 - index]);
    if (index % 2 === 1) {
      digit *= 2;
      if (digit > 9) digit -= 9;
    }
    sum += digit;
  }
  return sum % 10 === 0;
}

// Luhn plus a known issuer prefix with that issuer's length, so random long
// numbers (order ids, account parts) are not mistaken for cards.
function cardValid(value) {
  const digits = digitsOf(value);
  const brand = [
    [/^4/, [13, 16, 19]],
    [/^(5[1-5]|222[1-9]|22[3-9]\d|2[3-6]\d{2}|27[01]\d|2720)/, [16]],
    [/^3[47]/, [15]],
    [/^(6011|65|64[4-9]|622)/, [16, 17, 18, 19]],
    [/^3(0[0-5]|[689])/, [14, 15, 16, 17, 18, 19]],
    [/^35(2[89]|[3-8])/, [16, 17, 18, 19]],
    [/^(50|5[6-9]|6\d)/, [12, 13, 14, 15, 16, 17, 18, 19]],
  ].find(([prefix]) => prefix.test(digits));
  return Boolean(brand && brand[1].includes(digits.length) && luhn(digits));
}

function mod97(numeric) {
  let remainder = 0;
  for (const digit of numeric) remainder = (remainder * 10 + Number(digit)) % 97;
  return remainder;
}

function ibanValid(value) {
  const compact = String(value).replace(/\s/g, '');
  if (compact.length < 15 || compact.length > 34) return false;
  const rearranged = compact.slice(4) + compact.slice(0, 4);
  return mod97(rearranged.replace(/[A-Z]/g, (char) => String(char.charCodeAt(0) - 55))) === 1;
}

function entropy(value) {
  const counts = new Map();
  for (const char of value) counts.set(char, (counts.get(char) || 0) + 1);
  let bits = 0;
  for (const count of counts.values()) {
    const p = count / value.length;
    bits -= p * Math.log2(p);
  }
  return bits;
}

function looksLikePhone(value) {
  const digits = digitsOf(value);
  if (digits.length < 8 || digits.length > 15) return false;
  // Dates, versions and plain amounts are not phone numbers.
  if (/^\d{4}[-./]\d{1,2}[-./]\d{1,2}$/.test(value.trim())) return false;
  return /^(\+|00|0|\()/.test(value.trim()) || /[\s/-]/.test(value);
}

// ISO 7064 MOD 11,10: German tax ID and VAT ID check digits.
function iso7064Mod11_10(digits) {
  let product = 10;
  for (const digit of digits.slice(0, -1)) {
    let sum = (Number(digit) + product) % 10;
    if (sum === 0) sum = 10;
    product = (sum * 2) % 11;
  }
  const check = (11 - product) % 10;
  return check === Number(digits.at(-1));
}

// Steuer-ID: 11 digits, no leading 0, in the first ten one digit appears two
// or three times (three never all in a row) and every other digit at most once.
function steuerId(value) {
  const digits = digitsOf(value);
  if (!/^[1-9]\d{10}$/.test(digits)) return false;
  const counts = new Map();
  for (const digit of digits.slice(0, 10)) counts.set(digit, (counts.get(digit) || 0) + 1);
  const repeated = [...counts.entries()].filter(([, count]) => count > 1);
  if (repeated.length !== 1 || repeated[0][1] > 3) return false;
  if (repeated[0][1] === 3 && digits.slice(0, 10).includes(repeated[0][0].repeat(3))) return false;
  return iso7064Mod11_10(digits);
}

const ustIdDe = (value) => {
  const digits = digitsOf(value);
  return digits.length === 9 && iso7064Mod11_10(digits);
};

// Rentenversicherungsnummer: area, birth date, initial letter, serial, check.
function rvnr(value) {
  const compact = compactOf(value);
  const match = compact.match(/^(\d{8})([A-Z])(\d{2})(\d)$/);
  if (!match) return false;
  const digits = `${match[1]}${String(letterNumber(match[2])).padStart(2, '0')}${match[3]}`;
  const weights = [2, 1, 2, 5, 7, 1, 2, 1, 2, 1, 2, 1];
  const sum = weights.reduce((total, weight, index) => total + crossSum(weight * Number(digits[index])), 0);
  return sum % 10 === Number(match[4]);
}

// Krankenversichertennummer: a letter and nine digits, the last is the check.
function kvnr(value) {
  const compact = compactOf(value);
  const match = compact.match(/^([A-Z])(\d{8})(\d)$/);
  if (!match) return false;
  const digits = `${String(letterNumber(match[1])).padStart(2, '0')}${match[2]}`;
  const sum = [...digits].reduce((total, digit, index) => total + crossSum(Number(digit) * (index % 2 ? 2 : 1)), 0);
  return sum % 10 === Number(match[3]);
}

// ICAO 9303 7-3-1 check digit (passports, ID cards, machine-readable zones).
function icaoCheck(chars) {
  const weights = [7, 3, 1];
  let sum = 0;
  for (let index = 0; index < chars.length; index += 1) {
    const char = chars[index];
    const number = char === '<' ? 0 : /\d/.test(char) ? Number(char) : char.charCodeAt(0) - 55;
    sum += number * weights[index % 3];
  }
  return sum % 10;
}

// German ID card number: nine characters plus an ICAO check digit.
function idCardDe(value) {
  const compact = compactOf(value);
  return /^[CFGHJKLMNPRTVWXYZ0-9]{9}\d$/.test(compact) && icaoCheck(compact.slice(0, 9)) === Number(compact[9]);
}

// Second line of a passport MRZ: document number, birth date and expiry each
// carry their own check digit, and one more checks them all together.
function mrzLine(value) {
  const line = String(value).trim();
  if (line.length !== 44) return false;
  const field = (start, length) => icaoCheck(line.slice(start, start + length)) === Number(line[start + length]);
  const composite = line.slice(0, 10) + line.slice(13, 20) + line.slice(21, 43);
  return field(0, 9) && field(13, 6) && field(21, 6) && icaoCheck(composite) === Number(line[43]);
}

// Swiss AHV number: EAN-13 starting with 756.
function ahv(value) {
  const digits = digitsOf(value);
  if (!/^756\d{10}$/.test(digits)) return false;
  const sum = [...digits.slice(0, 12)].reduce((total, digit, index) => total + Number(digit) * (index % 2 ? 3 : 1), 0);
  return (10 - (sum % 10)) % 10 === Number(digits[12]);
}

// Austrian social security number: the fourth digit checks the other nine.
function svnrAt(value) {
  const digits = digitsOf(value);
  if (!/^\d{10}$/.test(digits) || digits[0] === '0') return false;
  const weights = [3, 7, 9, 0, 5, 8, 4, 2, 1, 6];
  const sum = weights.reduce((total, weight, index) => total + weight * Number(digits[index]), 0);
  return sum % 11 === Number(digits[3]);
}

// French social security number (NIR): 13 digits and a mod-97 key; Corsica
// (2A/2B) counts as 19/18.
function nirFr(value) {
  const compact = compactOf(value).replace(/^(\d{5})2A/, '$119').replace(/^(\d{5})2B/, '$118');
  if (!/^[12]\d{14}$/.test(compact)) return false;
  return 97 - Number(BigInt(compact.slice(0, 13)) % 97n) === Number(compact.slice(13));
}

// Spanish DNI and NIE: the letter is the number modulo 23.
function dniEs(value) {
  const compact = compactOf(value).replace(/^X/, '0').replace(/^Y/, '1').replace(/^Z/, '2');
  const match = compact.match(/^(\d{8})([A-Z])$/);
  return Boolean(match) && 'TRWAGMYFPDXBNJZSQVHLCKE'[Number(match[1]) % 23] === match[2];
}

// Italian Codice Fiscale: the last letter checks the first fifteen characters.
const CF_ODD = [1, 0, 5, 7, 9, 13, 15, 17, 19, 21, 2, 4, 18, 20, 11, 3, 6, 8, 12, 14, 16, 10, 22, 25, 24, 23];
function codiceFiscale(value) {
  const compact = compactOf(value);
  if (!/^[A-Z]{6}[0-9LMNPQRSTUV]{2}[A-EHLMPRST][0-9LMNPQRSTUV]{2}[A-Z][0-9LMNPQRSTUV]{3}[A-Z]$/.test(compact)) return false;
  let sum = 0;
  for (let index = 0; index < 15; index += 1) {
    const char = compact[index];
    const position = /\d/.test(char) ? Number(char) : char.charCodeAt(0) - 65;
    sum += index % 2 === 0 ? CF_ODD[position] : position;
  }
  return String.fromCharCode(65 + (sum % 26)) === compact[15];
}

// Dutch BSN: the "11-proef".
function bsnNl(value) {
  const digits = digitsOf(value);
  if (!/^\d{9}$/.test(digits)) return false;
  const sum = [...digits.slice(0, 8)].reduce((total, digit, index) => total + Number(digit) * (9 - index), 0) - Number(digits[8]);
  return sum % 11 === 0 && sum > 0;
}

function peselPl(value) {
  const digits = digitsOf(value);
  if (!/^\d{11}$/.test(digits)) return false;
  const weights = [1, 3, 7, 9, 1, 3, 7, 9, 1, 3];
  const sum = weights.reduce((total, weight, index) => total + weight * Number(digits[index]), 0);
  return (10 - (sum % 10)) % 10 === Number(digits[10]);
}

// Belgian national number: mod-97 over the first nine digits (with a leading
// 2 for people born from 2000 on).
function nationalNumberBe(value) {
  const digits = digitsOf(value);
  if (!/^\d{11}$/.test(digits)) return false;
  const check = Number(digits.slice(9));
  const base = BigInt(digits.slice(0, 9));
  return 97 - Number(base % 97n) === check || 97 - Number((2_000_000_000n + base) % 97n) === check;
}

function nhsUk(value) {
  const digits = digitsOf(value);
  if (!/^\d{10}$/.test(digits)) return false;
  const sum = [...digits.slice(0, 9)].reduce((total, digit, index) => total + Number(digit) * (10 - index), 0);
  const check = 11 - (sum % 11);
  return check !== 10 && (check === 11 ? 0 : check) === Number(digits[9]);
}

// US bank routing number (ABA): weights 3-7-1.
function abaRouting(value) {
  const digits = digitsOf(value);
  if (!/^\d{9}$/.test(digits)) return false;
  const weights = [3, 7, 1];
  return [...digits].reduce((total, digit, index) => total + Number(digit) * weights[index % 3], 0) % 10 === 0;
}

// Vehicle identification number: the North American check digit (position 9).
const VIN_VALUES = { A: 1, B: 2, C: 3, D: 4, E: 5, F: 6, G: 7, H: 8, J: 1, K: 2, L: 3, M: 4, N: 5, P: 7, R: 9, S: 2, T: 3, U: 4, V: 5, W: 6, X: 7, Y: 8, Z: 9 };
const VIN_WEIGHTS = [8, 7, 6, 5, 4, 3, 2, 10, 0, 9, 8, 7, 6, 5, 4, 3, 2];
function vinCheck(value) {
  const vin = String(value).toUpperCase();
  if (!/^[A-HJ-NPR-Z0-9]{17}$/.test(vin)) return false;
  const sum = [...vin].reduce((total, char, index) => total + (/\d/.test(char) ? Number(char) : VIN_VALUES[char]) * VIN_WEIGHTS[index], 0);
  const check = sum % 11;
  return (check === 10 ? 'X' : String(check)) === vin[8];
}

const BASE58 = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';
function base58Decode(value) {
  let number = 0n;
  for (const char of value) {
    const digit = BASE58.indexOf(char);
    if (digit < 0) return null;
    number = number * 58n + BigInt(digit);
  }
  const bytes = [];
  while (number > 0n) {
    bytes.unshift(Number(number % 256n));
    number /= 256n;
  }
  for (const char of value) {
    if (char !== '1') break;
    bytes.unshift(0);
  }
  return Buffer.from(bytes);
}

// Bitcoin, Litecoin, Tron and other base58check addresses.
function base58Check(value) {
  const bytes = base58Decode(value);
  if (!bytes || bytes.length < 21) return false;
  const payload = bytes.subarray(0, -4);
  const hash = crypto.createHash('sha256').update(crypto.createHash('sha256').update(payload).digest()).digest();
  return hash.subarray(0, 4).equals(bytes.subarray(-4));
}

// Bech32 and Bech32m (bc1…, ltc1…, tb1…).
const BECH32 = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l';
function bech32(value) {
  const lower = String(value).toLowerCase();
  const separator = lower.lastIndexOf('1');
  if (separator < 1 || lower.length - separator < 7) return false;
  const hrp = lower.slice(0, separator);
  const data = [...lower.slice(separator + 1)].map((char) => BECH32.indexOf(char));
  if (data.includes(-1)) return false;
  const generator = [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3];
  let check = 1;
  for (const number of [...[...hrp].map((char) => char.charCodeAt(0) >> 5), 0, ...[...hrp].map((char) => char.charCodeAt(0) & 31), ...data]) {
    const top = check >> 25;
    check = ((check & 0x1ffffff) << 5) ^ number;
    for (let bit = 0; bit < 5; bit += 1) if ((top >> bit) & 1) check ^= generator[bit];
  }
  return check === 1 || check === 0x2bc830a3;
}

function jwt(value) {
  try {
    const header = JSON.parse(Buffer.from(String(value).split('.')[0], 'base64url').toString('utf8'));
    return Boolean(header && typeof header === 'object' && (header.alg || header.typ));
  } catch {
    return false;
  }
}

const basicAuth = (value) => {
  try {
    return /^[^\s:]{1,}:\S+$/.test(Buffer.from(value, 'base64').toString('utf8'));
  } catch {
    return false;
  }
};

// At least three groups, so C++ scopes ("a::b") and times do not count.
const ipv6 = (value) => net.isIPv6(value) && value.split(':').filter(Boolean).length >= 3 && !/^2001:db8:/i.test(value);

// Range check for "lat, lon" pairs.
function coordinates(value) {
  const numbers = String(value).match(/-?\d+(?:\.\d+)?/g)?.map(Number) || [];
  return numbers.length >= 2 && Math.abs(numbers[0]) <= 90 && Math.abs(numbers[1]) <= 180 && !(numbers[0] === 0 && numbers[1] === 0);
}

// Values that only stand in for a secret ("${API_KEY}", "<your-key>", "xxxx").
const PLACEHOLDER_VALUE = /^(?:\$\{?[\w.:-]+\}?|%[\w]+%|<[^>]*>|\{\{.*\}\}|\[[A-Z]+_\d+\]|x{3,}|\*{3,}|\.{3,}|-+|_+|0+|changeme|change_me|your[-_ ]?\w*|my[-_ ]?\w*|example\w*|placeholder|dummy|sample|test|todo|tbd|null|none|nil|true|false|undefined|redacted|secret|password|passwort|token)$/i;
const notPlaceholder = (value) => !PLACEHOLDER_VALUE.test(String(value).trim());

// Machine-readable zone name lines: letters with "<<" between surname and given names.
const mrzNames = (value) => /[A-Z]{2}<<[A-Z]/.test(value);

// An EIP-55 checksummed Ethereum address has upper- and lowercase hex letters.
const mixedCaseHex = (value) => /[a-f]/.test(value.slice(2)) && /[A-F]/.test(value.slice(2));

// "Mixed" means letters and digits together: real IDs, not words or amounts.
const mixedAlnum = (value) => /\d/.test(value) && /[A-Za-z]/.test(value);

// No hex-only values (git hashes, digests) and no plain words.
const tokenLike = (value) => !/^[0-9a-f]+$/i.test(value) && /\d/.test(value) && /[a-z]/.test(value) && /[A-Z]/.test(value);

const VALIDATORS = Object.freeze({
  luhn: (value) => luhn(digitsOf(value)),
  card: cardValid,
  iban: ibanValid,
  phone: looksLikePhone,
  steuerId,
  ustIdDe,
  rvnr,
  kvnr,
  idCardDe,
  mrzLine,
  ahv,
  svnrAt,
  nirFr,
  dniEs,
  codiceFiscale,
  bsnNl,
  peselPl,
  nationalNumberBe,
  nhsUk,
  abaRouting,
  vinCheck,
  base58Check,
  bech32,
  jwt,
  basicAuth,
  ipv6,
  coordinates,
  notPlaceholder,
  mixedAlnum,
  mrzNames,
  mixedCaseHex,
  tokenLike,
});

module.exports = { VALIDATORS, luhn, cardValid, ibanValid, entropy, looksLikePhone, iso7064Mod11_10, icaoCheck, base58Check, bech32 };
