'use strict';

// What one pool did for and with a person over the last days, read from the
// request metadata that is stored anyway (never content):
//   given  what partners used of ours: which connections and models, which
//          partner, and what it cost us (paid models shared on purpose);
//   taken  what we used of partners: from whom and which models.
const { getDatabase } = require('../../db/database');

const DAY_MS = 86_400_000;
const TOP = 8;
const RANGES = Object.freeze([7, 30]);

// Successful requests in the range, as "r", joined to the partner link "l".
const GIVEN = `FROM requests r JOIN router_keys k ON k.id = r.router_key_id JOIN peer_links l ON l.id = k.peer_link_id
  WHERE r.user_id = ? AND l.pool_id = ? AND r.created_at >= ? AND r.status = 'success'`;
const TAKEN = `FROM requests r JOIN peer_links l ON l.account_id = r.account_id AND l.user_id = r.user_id
  WHERE r.user_id = ? AND l.pool_id = ? AND r.created_at >= ? AND r.status = 'success'`;
const SUMS = 'COUNT(*) requests, COALESCE(SUM(r.input_tokens + r.output_tokens), 0) tokens, COALESCE(SUM(r.cost_usd), 0) costUsd';

function grouped(from, select, groupBy, params) {
  return getDatabase().prepare(`SELECT ${select}, ${SUMS} ${from} GROUP BY ${groupBy} ORDER BY requests DESC LIMIT ${TOP}`).all(...params);
}

function totals(from, params) {
  const row = getDatabase().prepare(`SELECT ${SUMS} ${from}`).get(...params);
  return { requests: row.requests, tokens: row.tokens, costUsd: Number(row.costUsd.toFixed(6)) };
}

/** days: 7 or 30. Rows are { key, name, requests, tokens, costUsd }, most used first. */
function activity(userId, poolId, { days = 7, now = Date.now() } = {}) {
  const range = RANGES.includes(Number(days)) ? Number(days) : RANGES[0];
  const params = [userId, poolId, now - range * DAY_MS];
  return {
    days: range,
    given: {
      ...totals(GIVEN, params),
      byConnection: grouped(`${GIVEN.replace('WHERE', 'LEFT JOIN provider_accounts a ON a.id = r.account_id WHERE')}`, 'r.account_id key, a.label name', 'r.account_id', params),
      byModel: grouped(GIVEN, 'r.model_label key, r.model_label name', 'r.model_label', params),
      byPartner: grouped(GIVEN, 'l.member_id key, l.name name', 'l.member_id', params),
    },
    taken: {
      ...totals(TAKEN, params),
      byPartner: grouped(TAKEN, 'l.member_id key, l.name name', 'l.member_id', params),
      byModel: grouped(TAKEN, 'r.model_label key, r.model_label name', 'r.model_label', params),
    },
  };
}

module.exports = { activity, RANGES };
