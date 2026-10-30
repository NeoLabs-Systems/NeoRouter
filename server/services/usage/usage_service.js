'use strict';

const { getDatabase } = require('../../db/database');

const RANGES = { '24h': 86_400_000, '7d': 7 * 86_400_000, '30d': 30 * 86_400_000 };

// What a free answer would have cost at the model's market price: the median
// paid price of the same model across providers (model_metadata).
function savedUsd(entry) {
  if (!entry.is_free || entry.status !== 'success' || !entry.model_ref) return 0;
  const price = getDatabase().prepare(`SELECT md.price_in, md.price_out FROM models m
    JOIN model_metadata md ON md.canonical_id = m.canonical_id WHERE m.id = ?`).get(entry.model_ref);
  if (!price) return 0;
  return ((entry.input_tokens || 0) * (price.price_in || 0) + (entry.output_tokens || 0) * (price.price_out || 0)) / 1e6;
}

function record(entry) {
  getDatabase().prepare(`INSERT INTO requests (id, user_id, router_key_id, created_at, api_format, requested_model, model_ref, model_label,
      account_id, status, http_status, error_code, error_message, input_tokens, output_tokens, cost_usd, is_free, latency_ms, ttft_ms,
      attempts, stream, trail_json, saved_usd, redacted_count, compression_saved_tokens, capability, units, task)
    VALUES (@id, @user_id, @router_key_id, @created_at, @api_format, @requested_model, @model_ref, @model_label, @account_id, @status,
      @http_status, @error_code, @error_message, @input_tokens, @output_tokens, @cost_usd, @is_free, @latency_ms, @ttft_ms, @attempts,
      @stream, @trail_json, @saved_usd, @redacted_count, @compression_saved_tokens, @capability, @units, @task)`).run({
    model_ref: null, model_label: null, account_id: null, error_code: null, error_message: null, ttft_ms: null,
    redacted_count: 0, compression_saved_tokens: 0, capability: 'chat', units: 0, task: null, ...entry,
    saved_usd: savedUsd(entry),
  });
}

function rangeStart(range, now = Date.now()) {
  return now - (RANGES[range] || RANGES['24h']);
}

function summary(userId, range = '24h', now = Date.now()) {
  const db = getDatabase();
  const since = rangeStart(range, now);
  const totals = db.prepare(`SELECT COUNT(*) requests, SUM(status = 'success') successes, SUM(cost_usd) cost, SUM(saved_usd) saved,
      SUM(input_tokens) input_tokens, SUM(output_tokens) output_tokens, SUM(is_free AND status = 'success') free_requests,
      AVG(CASE WHEN status = 'success' THEN latency_ms END) avg_latency, AVG(CASE WHEN status = 'success' THEN ttft_ms END) avg_ttft,
      SUM(attempts > 1) fallbacks
    FROM requests WHERE user_id = ? AND created_at >= ?`).get(userId, since);
  const bucketMs = range === '24h' ? 3_600_000 : 86_400_000;
  const rows = db.prepare(`SELECT CAST(created_at / ? AS INTEGER) * ? bucket, COUNT(*) requests, SUM(status = 'success') successes,
      SUM(cost_usd) cost, SUM(input_tokens + output_tokens) tokens
    FROM requests WHERE user_id = ? AND created_at >= ? GROUP BY bucket ORDER BY bucket`).all(bucketMs, bucketMs, userId, since);
  // Continuous series with empty buckets so charts have a stable time axis.
  const byBucket = new Map(rows.map((row) => [row.bucket, row]));
  const series = [];
  for (let bucket = Math.floor(since / bucketMs) * bucketMs + bucketMs; bucket <= now; bucket += bucketMs) {
    series.push(byBucket.get(bucket) || { bucket, requests: 0, successes: 0, cost: 0, tokens: 0 });
  }
  const topModels = db.prepare(`SELECT model_label, COUNT(*) requests, SUM(status = 'success') successes, SUM(cost_usd) cost,
      SUM(input_tokens + output_tokens) tokens, AVG(latency_ms) avg_latency
    FROM requests WHERE user_id = ? AND created_at >= ? AND model_label IS NOT NULL GROUP BY model_label ORDER BY requests DESC LIMIT 10`)
    .all(userId, since);
  const byKey = db.prepare(`SELECT r.router_key_id id, k.name, COUNT(*) requests, SUM(r.cost_usd) cost
    FROM requests r LEFT JOIN router_keys k ON k.id = r.router_key_id
    WHERE r.user_id = ? AND r.created_at >= ? GROUP BY r.router_key_id ORDER BY requests DESC`).all(userId, since);
  const errors = db.prepare(`SELECT error_code, COUNT(*) count FROM requests WHERE user_id = ? AND created_at >= ? AND status = 'error'
    GROUP BY error_code ORDER BY count DESC LIMIT 10`).all(userId, since);
  return {
    range,
    bucketMs,
    totals: {
      requests: totals.requests || 0,
      successes: totals.successes || 0,
      successRate: totals.requests ? (totals.successes || 0) / totals.requests : null,
      costUsd: totals.cost || 0,
      savedUsd: totals.saved || 0,
      inputTokens: totals.input_tokens || 0,
      outputTokens: totals.output_tokens || 0,
      freeRequests: totals.free_requests || 0,
      avgLatencyMs: totals.avg_latency,
      avgTtftMs: totals.avg_ttft,
      fallbacks: totals.fallbacks || 0,
    },
    series,
    topModels,
    byKey,
    errors,
  };
}

function percentile(sorted, p) {
  if (!sorted.length) return null;
  return sorted[Math.min(sorted.length - 1, Math.floor(p * sorted.length))];
}

function newGroup() {
  return { requests: 0, successes: 0, errors: 0, inputTokens: 0, outputTokens: 0, costUsd: 0, savedUsd: 0, freeRequests: 0, fallbacks: 0, latencies: [], ttfts: [] };
}

function addRow(group, row) {
  group.requests += 1;
  if (row.status === 'success') group.successes += 1;
  else group.errors += 1;
  group.inputTokens += row.input_tokens;
  group.outputTokens += row.output_tokens;
  group.costUsd += row.cost_usd;
  group.savedUsd += row.saved_usd || 0;
  if (row.is_free && row.status === 'success') group.freeRequests += 1;
  if (row.attempts > 1) group.fallbacks += 1;
  if (row.status === 'success' && row.latency_ms !== null) group.latencies.push(row.latency_ms);
  if (row.ttft_ms !== null) group.ttfts.push(row.ttft_ms);
}

function finishGroup(group) {
  const latencies = group.latencies.sort((a, b) => a - b);
  const ttfts = group.ttfts.sort((a, b) => a - b);
  const { latencies: _l, ttfts: _t, ...rest } = group;
  return {
    ...rest,
    successRate: group.requests ? group.successes / group.requests : null,
    latencyP50: percentile(latencies, 0.5),
    latencyP95: percentile(latencies, 0.95),
    ttftP50: percentile(ttfts, 0.5),
  };
}

// Rows counted under a key each, e.g. per provider; the largest kept.
function grouper(keyOf) {
  const groups = new Map();
  return {
    add(row) {
      const key = keyOf(row);
      if (key === null || key === undefined) return;
      if (!groups.has(key)) groups.set(key, newGroup());
      addRow(groups.get(key), row);
    },
    top(limit) {
      return [...groups.entries()].map(([key, group]) => ({ key, ...finishGroup(group) }))
        .sort((a, b) => b.requests - a.requests).slice(0, limit);
    },
  };
}

// Everything the analytics page charts, computed from request metadata.
function analytics(userId, range = '24h', now = Date.now()) {
  const db = getDatabase();
  const since = rangeStart(range, now);
  const bucketMs = range === '24h' ? 3_600_000 : range === '7d' ? 6 * 3_600_000 : 86_400_000;
  const rows = db.prepare(`SELECT r.created_at, r.status, r.error_code, r.input_tokens, r.output_tokens, r.cost_usd, r.saved_usd, r.is_free,
      r.latency_ms, r.ttft_ms, r.attempts, r.stream, r.api_format, r.model_label, r.router_key_id, r.trail_json, r.capability, k.name key_name
    FROM requests r LEFT JOIN router_keys k ON k.id = r.router_key_id
    WHERE r.user_id = ? AND r.created_at >= ? ORDER BY r.created_at`);

  const buckets = new Map();
  for (let bucket = Math.floor(since / bucketMs) * bucketMs + bucketMs; bucket <= now; bucket += bucketMs) buckets.set(bucket, newGroup());
  const total = newGroup();
  const outcomes = {};
  const histogramEdges = [250, 500, 1000, 2000, 5000, 10000, 30000, Infinity];
  const histogram = histogramEdges.map((edge) => ({ upToMs: Number.isFinite(edge) ? edge : null, count: 0 }));
  const providerOf = (row) => (row.model_label ? row.model_label.split('/')[0] : null);
  const groups = {
    byProvider: grouper(providerOf),
    byModel: grouper((row) => row.model_label),
    byKey: grouper((row) => row.key_name || row.router_key_id),
    byFormat: grouper((row) => `${row.api_format}${row.stream ? ' · stream' : ''}`),
    byCapability: grouper((row) => row.capability || 'chat'),
    errors: grouper((row) => (row.status === 'error' ? row.error_code || 'unknown' : null)),
  };
  // Streamed row by row, so every request in the range counts.
  for (const row of rows.iterate(userId, since)) {
    addRow(total, row);
    for (const group of Object.values(groups)) group.add(row);
    const bucket = Math.floor(row.created_at / bucketMs) * bucketMs;
    if (!buckets.has(bucket)) buckets.set(bucket, newGroup());
    addRow(buckets.get(bucket), row);
    for (const step of JSON.parse(row.trail_json || '[]')) outcomes[step.outcome] = (outcomes[step.outcome] || 0) + 1;
    if (row.status === 'success' && row.latency_ms !== null) histogram[histogramEdges.findIndex((edge) => row.latency_ms <= edge)].count += 1;
  }
  return {
    range,
    bucketMs,
    totals: finishGroup(total),
    series: [...buckets.entries()].sort((a, b) => a[0] - b[0]).map(([bucket, group]) => ({ bucket, ...finishGroup(group) })),
    byProvider: groups.byProvider.top(20),
    byModel: groups.byModel.top(15),
    byKey: groups.byKey.top(20),
    byFormat: groups.byFormat.top(10),
    byCapability: groups.byCapability.top(10),
    errors: groups.errors.top(10).map(({ key, requests }) => ({ key, count: requests })),
    attemptOutcomes: Object.entries(outcomes).map(([key, count]) => ({ key, count })).sort((a, b) => b.count - a.count),
    latencyHistogram: histogram,
  };
}

function logs(userId, { limit = 100, before = null, keyId = null, status = null } = {}) {
  return getDatabase().prepare(`SELECT r.*, k.name key_name FROM requests r LEFT JOIN router_keys k ON k.id = r.router_key_id
    WHERE r.user_id = ? AND (? IS NULL OR r.created_at < ?) AND (? IS NULL OR r.router_key_id = ?) AND (? IS NULL OR r.status = ?)
    ORDER BY r.created_at DESC LIMIT ?`)
    .all(userId, before, before, keyId, keyId, status, status, Math.min(500, limit))
    .map((row) => ({
      id: row.id,
      createdAt: row.created_at,
      keyId: row.router_key_id,
      keyName: row.key_name,
      apiFormat: row.api_format,
      requestedModel: row.requested_model,
      model: row.model_label,
      status: row.status,
      httpStatus: row.http_status,
      errorCode: row.error_code,
      errorMessage: row.error_message,
      inputTokens: row.input_tokens,
      outputTokens: row.output_tokens,
      costUsd: row.cost_usd,
      isFree: Boolean(row.is_free),
      latencyMs: row.latency_ms,
      ttftMs: row.ttft_ms,
      attempts: row.attempts,
      stream: Boolean(row.stream),
      trail: JSON.parse(row.trail_json || '[]'),
      capability: row.capability || 'chat',
      task: row.task || null,
      units: row.units || 0,
    }));
}

function prune(retentionDays = 90, now = Date.now()) {
  getDatabase().prepare('DELETE FROM requests WHERE created_at < ?').run(now - retentionDays * 86_400_000);
}

module.exports = { record, summary, analytics, logs, prune };
