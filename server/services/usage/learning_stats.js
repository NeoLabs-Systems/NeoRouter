'use strict';

const { getDatabase } = require('../../db/database');
const stats = require('../routing/stats_store');
const experience = require('../routing/task/experience');

const DAY = 86_400_000;
const CURVE_DAYS = 30;
const MAP_TASKS = 8;
const MAP_MODELS = 8;
const FACTS = 12;

// One model, whichever provider or account served it.
const nameOf = (row) => row.display_name || row.model_id;

// How often the model the router chose first was the one that answered, per
// day. Rising means it has stopped trying models that would fail.
function firstTryCurve(userId, now) {
  const since = Math.floor((now - CURVE_DAYS * DAY) / DAY) * DAY + DAY;
  const days = new Map();
  let rescued = 0;
  const rows = getDatabase().prepare('SELECT created_at, status, trail_json FROM requests WHERE user_id = ? AND created_at >= ? ORDER BY created_at');
  for (const row of rows.iterate(userId, since)) {
    const bucket = Math.floor(row.created_at / DAY) * DAY;
    if (!days.has(bucket)) days.set(bucket, { bucket, requests: 0, firstTry: 0 });
    const day = days.get(bucket);
    // A hedge that lost a race was not a wrong choice, only a slower one.
    const missed = JSON.parse(row.trail_json || '[]').some((step) => step.outcome !== 'success' && step.outcome !== 'hedge_lost');
    day.requests += 1;
    if (row.status === 'success' && !missed) day.firstTry += 1;
    if (row.status === 'success' && missed) rescued += 1;
  }
  const curve = [...days.values()].map((day) => ({ ...day, rate: day.firstTry / day.requests }));
  const share = (items) => {
    const requests = items.reduce((sum, day) => sum + day.requests, 0);
    return requests ? items.reduce((sum, day) => sum + day.firstTry, 0) / requests : null;
  };
  // Thirds of the range, so one odd day does not decide the headline.
  const third = Math.max(1, Math.floor(curve.length / 3));
  return {
    curve,
    rescued,
    firstTryThen: curve.length >= 3 ? share(curve.slice(0, third)) : null,
    firstTryNow: curve.length ? share(curve.slice(-third)) : null,
  };
}

// What it knows per kind of task, with the same fading and the same evidence
// weighting routing itself uses, across every copy of a model.
function taskKnowledge(userId, now) {
  const rows = getDatabase().prepare(`SELECT s.*, m.canonical_id, m.model_id, m.display_name FROM model_task_stats s
    JOIN models m ON m.id = s.model_ref JOIN provider_accounts a ON a.id = m.account_id WHERE a.user_id = ?`).all(userId);
  const cells = new Map();
  for (const row of rows) {
    const faded = experience.faded(row, now);
    const key = `${row.task}|${row.canonical_id}`;
    if (!cells.has(key)) cells.set(key, { task: row.task, model: row.canonical_id, name: nameOf(row), successes: 0, failures: 0, liked: 0, disliked: 0 });
    const cell = cells.get(key);
    for (const field of ['successes', 'failures', 'liked', 'disliked']) cell[field] += faded[field];
  }
  return [...cells.values()].map((cell) => {
    const seen = experience.standing(cell);
    return {
      task: cell.task,
      model: cell.model,
      name: cell.name,
      answers: Math.round(cell.successes + cell.failures),
      rate: seen.rate,
      standing: seen.standing,
      liked: Math.round(cell.liked),
      disliked: Math.round(cell.disliked),
    };
  }).filter((cell) => cell.answers > 0);
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
    taskWeight.set(cell.task, (taskWeight.get(cell.task) || 0) + cell.answers);
    modelWeight.set(cell.model, (modelWeight.get(cell.model) || 0) + cell.answers);
  }
  const busiest = (weights, limit) => [...weights.entries()].sort((a, b) => b[1] - a[1]).slice(0, limit).map(([key]) => key);
  const taskIds = busiest(taskWeight, MAP_TASKS);
  // Each task's best model always gets a column, then the busiest ones.
  const best = taskIds.map((task) => {
    const [top] = cells.filter((cell) => cell.task === task).sort((a, b) => b.standing - a.standing || b.answers - a.answers);
    return { task, answers: taskWeight.get(task), model: top.model, name: top.name, rate: top.rate, modelAnswers: top.answers };
  });
  const modelIds = [...new Set([...best.map((item) => item.model), ...busiest(modelWeight, MAP_MODELS)])].slice(0, MAP_MODELS);
  const names = new Map(cells.map((cell) => [cell.model, cell.name]));

  return {
    answers: [...taskWeight.values()].reduce((sum, value) => sum + value, 0),
    tasksKnown: taskWeight.size,
    modelsKnown: modelWeight.size,
    measurements: models.reduce((sum, model) => sum + model.samples, 0),
    rescued: curve.rescued,
    firstTryThen: curve.firstTryThen,
    firstTryNow: curve.firstTryNow,
    curve: curve.curve,
    best,
    map: {
      tasks: taskIds,
      models: modelIds.map((model) => ({ model, name: names.get(model) })),
      cells: cells.filter((cell) => taskIds.includes(cell.task) && modelIds.includes(cell.model))
        .map(({ standing, ...cell }) => cell),
    },
    fastest: models.filter((model) => model.latencyMs !== null).sort((a, b) => a.latencyMs - b.latencyMs).slice(0, 5),
    discoveries: facts,
  };
}

module.exports = { learning };
