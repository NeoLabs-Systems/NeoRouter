'use strict';

// Parses provider rate-limit headers into observations:
//   { metric: 'rpm'|'rpd'|'tpm'|'tpd', limit, remaining, resetAt }
// Header families seen in the wild:
//   x-ratelimit-{limit,remaining,reset}-{requests,tokens}            (OpenAI, Groq, OpenRouter, …)
//   x-ratelimit-{limit,remaining,reset}-{requests,tokens}-{minute,day} (Cerebras)
//   x-ratelimit-{limit,remaining,reset}                               (plain request window)

const PERIOD_METRIC = { requests: { minute: 'rpm', day: 'rpd', hour: 'rpm' }, tokens: { minute: 'tpm', day: 'tpd', hour: 'tpm' } };

// "1m30.5s", "6ms", "2h", "45" (seconds), epoch seconds or epoch ms.
function parseReset(value, now = Date.now()) {
  if (value === null || value === undefined || value === '') return null;
  const raw = String(value).trim();
  if (/^\d+(\.\d+)?$/.test(raw)) {
    const number = Number(raw);
    if (number > 1e12) return number;
    if (number > 1e9) return number * 1000;
    return now + number * 1000;
  }
  const date = Date.parse(raw);
  if (Number.isFinite(date) && /\d{4}-\d{2}-\d{2}|GMT|UTC/.test(raw)) return date;
  let total = 0;
  let matched = false;
  for (const [, amount, unit] of raw.matchAll(/(\d+(?:\.\d+)?)(ms|h|m|s|d)/g)) {
    matched = true;
    const factor = { ms: 1, s: 1000, m: 60_000, h: 3_600_000, d: 86_400_000 }[unit];
    total += Number(amount) * factor;
  }
  return matched ? now + total : null;
}

function headerEntries(headers) {
  if (!headers) return [];
  if (typeof headers.entries === 'function') return [...headers.entries()];
  return Object.entries(headers);
}

function parseRateLimitHeaders(headers, { headerPeriods = { requests: 'minute', tokens: 'minute' }, plainHeaderMetric = 'rpm' } = {}, now = Date.now()) {
  const observations = new Map();
  const touch = (metric) => {
    if (!observations.has(metric)) observations.set(metric, { metric, limit: null, remaining: null, resetAt: null });
    return observations.get(metric);
  };
  for (const [rawName, value] of headerEntries(headers)) {
    const name = rawName.toLowerCase();
    let match = name.match(/^x-ratelimit-(limit|remaining|reset)-(requests|tokens)(?:-(minute|day|hour))?$/);
    let field;
    let metric;
    if (match) {
      field = match[1];
      const period = match[3] || headerPeriods[match[2]];
      metric = PERIOD_METRIC[match[2]][period];
    } else if ((match = name.match(/^x-ratelimit-(limit|remaining|reset)$/))) {
      field = match[1];
      metric = plainHeaderMetric;
    } else {
      continue;
    }
    const observation = touch(metric);
    if (field === 'reset') observation.resetAt = parseReset(value, now);
    else {
      const number = Number(String(value).split(',')[0]);
      if (Number.isFinite(number)) observation[field] = number;
    }
  }
  return [...observations.values()].filter((item) => item.limit !== null || item.remaining !== null);
}

module.exports = { parseRateLimitHeaders, parseReset };
