'use strict';

// The limits of one provider account in a form a person can read at a glance:
// per metric, what the whole account shares and what each model gets (a range
// when models differ), plus where the numbers come from.
const METRICS = ['rpm', 'rpd', 'tpm', 'tpd'];
const SOURCE_ORDER = ['manual', 'header', 'learned', 'curated'];

// A window without a positive limit is only an "out until later" marker.
const isLimit = (window) => Number.isFinite(window?.limit) && window.limit > 0 && window.source !== 'error';

function sourcesOf(windows) {
  const seen = new Set(windows.map((window) => window.source));
  return SOURCE_ORDER.filter((source) => seen.has(source));
}

function summarizeLimits({ pool = [], models = [] }) {
  const rows = [];
  for (const metric of METRICS) {
    const shared = pool.filter((window) => window.metric === metric && isLimit(window))[0] || null;
    const own = models.flatMap((model) => model.windows.filter((window) => window.metric === metric && isLimit(window)));
    if (!shared && !own.length) continue;
    const values = own.map((window) => window.limit);
    rows.push({
      metric,
      shared: shared ? { limit: shared.limit, source: shared.source } : null,
      perModel: own.length ? { min: Math.min(...values), max: Math.max(...values), models: own.length, sources: sourcesOf(own) } : null,
    });
  }
  return rows;
}

module.exports = { summarizeLimits };
