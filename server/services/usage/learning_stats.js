'use strict';

const { getDatabase } = require('../../db/database');
const stats = require('../routing/stats_store');
const experience = require('../routing/task/experience');
const scoring = require('../routing/scoring');
const { PRESETS } = require('../routing/profiles');
const { needsCredits } = require('../routing/router');
const catalog = require('../catalog/catalog_service');
const { KINDS } = require('../routing/task/kinds');

const DAY = 86_400_000;
const CURVE_DAYS = 30;
const FACTS = 12;
const SKILLS_OF_TASK = new Map(KINDS.map((kind) => [kind.id, kind.skills]));
// Measured models listed even when they have had no traffic yet.
const UNTRIED_MODELS = 60;
const BENCHMARK_SOURCES = /^(artificial_analysis|openrouter_benchmarks)/;
// Cooldowns that outlast the moment: refused for this account, not merely busy.
const LASTING_COOLDOWNS = new Set(['not_available', 'payment_required', 'auth_failed', 'incompatible', 'no_free_quota']);
const NO_EVIDENCE_RELIABILITY = 0.85;
const NO_ANSWERS = Object.freeze({ successes: 0, failures: 0, liked: 0, disliked: 0 });

// One model, whichever provider or account served it.
const nameOf = (row) => row.display_name || row.model_id;

// How often the model the router chose first answered, per day, among requests
// where a model could be tried at all. Requests that found every model busy say
// nothing about the choice: they are counted apart, as no capacity.
function firstTryCurve(userId, now) {
  const since = Math.floor((now - CURVE_DAYS * DAY) / DAY) * DAY + DAY;
  const days = new Map();
  let rescued = 0;
  let noCapacity = 0;
  const rows = getDatabase().prepare('SELECT created_at, status, error_code, trail_json FROM requests WHERE user_id = ? AND created_at >= ? ORDER BY created_at');
  for (const row of rows.iterate(userId, since)) {
    const bucket = Math.floor(row.created_at / DAY) * DAY;
    if (!days.has(bucket)) days.set(bucket, { bucket, requests: 0, tried: 0, firstTry: 0 });
    const day = days.get(bucket);
    day.requests += 1;
    const trail = JSON.parse(row.trail_json || '[]');
    if (!trail.length) {
      if (row.status === 'error' && /no_model_available/i.test(row.error_code || '')) noCapacity += 1;
      continue;
    }
    day.tried += 1;
    // A hedge that lost a race was not a wrong choice, only a slower one.
    const worked = (step) => step.outcome === 'success' || step.outcome === 'hedge_lost';
    if (worked(trail[0])) day.firstTry += 1;
    if (row.status === 'success' && !trail.every(worked)) rescued += 1;
  }
  const curve = [...days.values()].filter((day) => day.tried > 0).map((day) => ({ ...day, rate: day.firstTry / day.tried }));
  const share = (items) => {
    const tried = items.reduce((sum, day) => sum + day.tried, 0);
    return tried ? items.reduce((sum, day) => sum + day.firstTry, 0) / tried : null;
  };
  // Thirds of the range, so one odd day does not decide the headline.
  const third = Math.max(1, Math.floor(curve.length / 3));
  return {
    curve,
    rescued,
    noCapacity,
    firstTryThen: curve.length >= 3 ? share(curve.slice(0, third)) : null,
    firstTryNow: curve.length ? share(curve.slice(-third)) : null,
  };
}

// Whether a copy can take a request at all, for a key that allows it. Short
// cooldowns pass: a model that is merely busy can still be the best one.
function usable(entry, now) {
  if (!entry.enabled || !entry.available || !entry.accountEnabled || entry.accountStatus === 'disabled' || entry.accountStatus === 'invalid') return false;
  if (needsCredits(entry)) return false;
  const { reason } = stats.availability(entry.ref, now);
  return !LASTING_COOLDOWNS.has(reason);
}

// How reliably a model delivered this kind of work: its usable answers, not the
// rate limits it hit, since being popular says nothing about what it is good at.
// Without much to go on it starts from an ordinary value.
function workedRate(standing) {
  return standing.share * standing.rate + (1 - standing.share) * NO_EVIDENCE_RELIABILITY;
}

// What it knows per kind of task about every model this person can use, not
// only the ones that happened to get traffic: a model nobody tried can still be
// the best one. `score` is the one number for how good it is at that work, on the
// balanced style's weights; the parts go along, so the app can rank for any mix.
// A model counts as its best copy.
function taskKnowledge(userId, now) {
  const rows = getDatabase().prepare(`SELECT s.* FROM model_task_stats s JOIN models m ON m.id = s.model_ref
    JOIN provider_accounts a ON a.id = m.account_id WHERE a.user_id = ?`).all(userId);
  const entries = catalog.listForUser(userId, { includeUnavailable: true }).filter((entry) => usable(entry, now));
  const byRef = new Map(entries.map((entry) => [entry.ref, entry]));
  const counts = new Map();
  const tasks = new Set();
  for (const row of rows) {
    if (!byRef.has(row.model_ref)) continue;
    tasks.add(row.task);
    counts.set(`${row.model_ref}|${row.task}`, experience.faded(row, now));
  }
  const measuredByBenchmarks = (entry) => entry.intelligenceSource === 'manual' || BENCHMARK_SOURCES.test(entry.intelligenceSource || '');
  // A model only OpenRouter knows has a score for reasoning and little else:
  // measured for the work that calls on it, an estimate for the rest.
  const measuredFor = (entry, task) => {
    if (!measuredByBenchmarks(entry)) return false;
    if (entry.intelligenceSource !== 'openrouter_benchmarks') return true;
    const needed = Object.keys(SKILLS_OF_TASK.get(task) || {});
    return !needed.length || needed.some((skill) => Number.isFinite(entry.skills?.[skill]));
  };
  // Ids can differ ("beam", "beam-1.0-501b"); one benchmark record is one model.
  const modelOf = (entry) => (entry.skills ? `${entry.intelligence}|${JSON.stringify(entry.skills)}` : entry.canonicalId);
  const tried = new Set(rows.filter((row) => byRef.has(row.model_ref)).map((row) => modelOf(byRef.get(row.model_ref))));
  // The strongest measured models too, whatever their traffic. Others stay out,
  // or the list would hold every model of every connection.
  const strongest = new Set([...new Set(entries.filter(measuredByBenchmarks).sort((a, b) => (b.intelligence ?? 0) - (a.intelligence ?? 0)).map(modelOf))].slice(0, UNTRIED_MODELS));
  const { weights } = PRESETS.balanced;
  const cells = new Map();
  for (const entry of entries) {
    const model = modelOf(entry);
    if (!tried.has(model) && !strongest.has(model)) continue;
    for (const task of tasks) {
      const key = `${task}|${model}`;
      if (!cells.has(key)) {
        cells.set(key, { task, model, name: entry.displayName || entry.modelId, successes: 0, failures: 0, liked: 0, disliked: 0, score: -1, free: null, freeScore: -1, benchmarked: measuredFor(entry, task) });
      }
      const cell = cells.get(key);
      const faded = counts.get(`${entry.ref}|${task}`);
      if (faded) for (const field of ['successes', 'failures', 'liked', 'disliked']) cell[field] += faded[field];
      const features = scoring.worthParts(entry, { task: { primary: task, skills: SKILLS_OF_TASK.get(task) }, now });
      const parts = { ...features, reliability: workedRate(experience.standing(faded || NO_ANSWERS)) };
      const value = scoring.combineWorth(parts, weights);
      if (value > cell.score) Object.assign(cell, { score: value, quality: parts.intelligence, speed: parts.speed, reliability: parts.reliability });
      if (entry.isFree && value > cell.freeScore) Object.assign(cell, { freeScore: value, free: { quality: parts.intelligence, speed: parts.speed, reliability: parts.reliability } });
    }
  }
  return [...cells.values()].map((cell) => {
    const answers = Math.round(cell.successes + cell.failures);
    return {
      task: cell.task,
      model: cell.model,
      name: cell.name,
      answers,
      score: cell.score,
      quality: cell.quality,
      speed: cell.speed,
      reliability: cell.reliability,
      free: cell.free,
      benchmarked: cell.benchmarked,
      rate: answers ? experience.standing(cell).rate : null,
      liked: Math.round(cell.liked),
      disliked: Math.round(cell.disliked),
    };
  });
}

// Speed and reliability it measured itself, across every copy of a model.
function measured(userId, now) {
  const rows = getDatabase().prepare(`SELECT s.model_ref, m.canonical_id, m.model_id, m.display_name FROM model_stats s
    JOIN models m ON m.id = s.model_ref JOIN provider_accounts a ON a.id = m.account_id WHERE a.user_id = ? AND s.samples > 0`).all(userId);
  const models = new Map();
  for (const row of rows) {
    const seen = stats.snapshot(row.model_ref, now);
    if (!models.has(row.canonical_id)) models.set(row.canonical_id, { model: row.canonical_id, name: nameOf(row), samples: 0, latencyMs: null, tps: null, successes: 0, failures: 0 });
    const model = models.get(row.canonical_id);
    model.samples += seen.samples;
    model.successes += seen.successes;
    model.failures += seen.failures;
    // The best copy: that is the one routing ends up preferring.
    if (seen.latency_ms !== null && (model.latencyMs === null || seen.latency_ms < model.latencyMs)) model.latencyMs = Math.round(seen.latency_ms);
    if (seen.tps !== null && (model.tps === null || seen.tps > model.tps)) model.tps = Math.round(seen.tps);
  }
  return [...models.values()].map(({ successes, failures, ...model }) => ({
    ...model,
    reliability: successes + failures > 0 ? successes / (successes + failures) : null,
  }));
}

// Things nobody told it: limits read out of error messages, room a model turned
// out to need, and models it is giving a rest right now.
function discoveries(userId, now) {
  const db = getDatabase();
  const facts = [];
  for (const row of db.prepare(`SELECT l.metric, l.value, l.updated_at, m.model_id, m.display_name FROM quota_limits l
    JOIN models m ON l.scope_key = 'm:' || m.id JOIN provider_accounts a ON a.id = m.account_id
    WHERE a.user_id = ? AND l.source = 'learned'`).all(userId)) {
    facts.push({ kind: 'limit', name: nameOf(row), metric: row.metric, value: row.value, at: row.updated_at });
  }
  for (const row of db.prepare(`SELECT l.metric, l.value, l.updated_at, a.label FROM quota_limits l
    JOIN provider_accounts a ON l.scope_key = 'p:' || a.id WHERE a.user_id = ? AND l.source = 'learned'`).all(userId)) {
    facts.push({ kind: 'limit', name: row.label, metric: row.metric, value: row.value, at: row.updated_at });
  }
  const seen = db.prepare(`SELECT s.min_output_tokens, s.last_failure_at, s.cooldown_until, s.cooldown_reason, s.breaker_open_until, m.model_id, m.display_name
    FROM model_stats s JOIN models m ON m.id = s.model_ref JOIN provider_accounts a ON a.id = m.account_id
    WHERE a.user_id = ? AND (s.min_output_tokens > 0 OR s.cooldown_until > ? OR s.breaker_open_until > ?)`).all(userId, now, now);
  for (const row of seen) {
    if (row.min_output_tokens > 0) facts.push({ kind: 'room', name: nameOf(row), value: row.min_output_tokens, at: row.last_failure_at });
    const until = Math.max(row.cooldown_until, row.breaker_open_until);
    if (until > now) facts.push({ kind: 'resting', name: nameOf(row), reason: row.cooldown_until > now ? row.cooldown_reason : 'circuit_open', until, at: row.last_failure_at });
  }
  // What is happening now first, then the newest finds.
  return facts.sort((a, b) => (b.kind === 'resting') - (a.kind === 'resting') || (b.at || 0) - (a.at || 0)).slice(0, FACTS);
}

function learning(userId, now = Date.now()) {
  const cells = taskKnowledge(userId, now);
  const curve = firstTryCurve(userId, now);
  const models = measured(userId, now);
  const facts = discoveries(userId, now);

  const taskWeight = new Map();
  const modelWeight = new Map();
  for (const cell of cells) {
    if (!cell.answers) continue;
    taskWeight.set(cell.task, (taskWeight.get(cell.task) || 0) + cell.answers);
    modelWeight.set(cell.model, (modelWeight.get(cell.model) || 0) + cell.answers);
  }
  // Busiest first; the app ranks them for whatever the person is after.
  const busiest = (weights) => [...weights.entries()].sort((a, b) => b[1] - a[1]).map(([key]) => key);

  return {
    answers: [...taskWeight.values()].reduce((sum, value) => sum + value, 0),
    tasksKnown: taskWeight.size,
    modelsKnown: modelWeight.size,
    measurements: models.reduce((sum, model) => sum + model.samples, 0),
    rescued: curve.rescued,
    noCapacity: curve.noCapacity,
    firstTryThen: curve.firstTryThen,
    firstTryNow: curve.firstTryNow,
    curve: curve.curve,
    tasks: busiest(taskWeight),
    models: busiest(modelWeight),
    cells,
    fastest: models.filter((model) => model.latencyMs !== null).sort((a, b) => a.latencyMs - b.latencyMs).slice(0, 5),
    discoveries: facts,
  };
}

module.exports = { learning };
