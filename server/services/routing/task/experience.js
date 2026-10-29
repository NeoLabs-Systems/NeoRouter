'use strict';

const { getDatabase } = require('../../../db/database');

// What NeoRouter learned about a model on one kind of task: answers that
// worked or failed (empty, broken tool calls, cut off) and feedback from
// apps (POST /v1/feedback). Counts fade with a week's half-life, so a model
// that improves earns its way back.
const HALF_LIFE_MS = 7 * 24 * 60 * 60 * 1000;
// Evidence needed before task experience counts as much as the general track record.
const OUTCOME_CONFIDENCE = 8;
const FEEDBACK_CONFIDENCE = 5;

const cache = new Map();
// Task kinds whose rows are all in the cache: routing reads every model of a
// request, so a kind is loaded with one query the first time it comes up.
const loadedTasks = new Set();

function load(ref, task) {
  const key = `${ref}|${task}`;
  if (cache.has(key)) return cache.get(key);
  if (!loadedTasks.has(task)) {
    loadedTasks.add(task);
    for (const row of getDatabase().prepare('SELECT * FROM model_task_stats WHERE task = ?').all(task)) cache.set(`${row.model_ref}|${task}`, row);
    if (cache.has(key)) return cache.get(key);
  }
  const row = { model_ref: ref, task, successes: 0, failures: 0, liked: 0, disliked: 0, updated_at: Date.now() };
  cache.set(key, row);
  return row;
}

function decay(row, now) {
  const factor = 0.5 ** ((now - (row.updated_at || now)) / HALF_LIFE_MS);
  for (const field of ['successes', 'failures', 'liked', 'disliked']) row[field] *= factor;
  row.updated_at = now;
  return row;
}

function save(row) {
  getDatabase().prepare(`INSERT INTO model_task_stats (model_ref, task, successes, failures, liked, disliked, updated_at)
    VALUES (@model_ref, @task, @successes, @failures, @liked, @disliked, @updated_at)
    ON CONFLICT(model_ref, task) DO UPDATE SET successes = excluded.successes, failures = excluded.failures,
      liked = excluded.liked, disliked = excluded.disliked, updated_at = excluded.updated_at`).run(row);
}

function update(ref, task, mutate, now = Date.now()) {
  if (!ref || !task) return;
  const row = decay(load(ref, task), now);
  mutate(row);
  save(row);
}

function recordOutcome(ref, task, ok, now) {
  update(ref, task, (row) => { row[ok ? 'successes' : 'failures'] += 1; }, now);
}

function recordFeedback(ref, task, good, now) {
  update(ref, task, (row) => { row[good ? 'liked' : 'disliked'] += 1; }, now);
}

/**
 * How the model does on this task, for scoring: reliability blended towards
 * its success rate on the task, and a quality factor from feedback (1 = none).
 */
function adjust(ref, task, reliability, now = Date.now()) {
  if (!ref || !task) return { reliability, quality: 1 };
  const row = decay({ ...load(ref, task) }, now);
  const outcomes = row.successes + row.failures;
  const rate = (row.successes + 2) / (outcomes + 3);
  const share = outcomes / (outcomes + OUTCOME_CONFIDENCE);
  const votes = row.liked + row.disliked;
  const liked = (row.liked + 1) / (votes + 2);
  const confidence = votes / (votes + FEEDBACK_CONFIDENCE);
  return {
    reliability: (1 - share) * reliability + share * rate,
    // 0.85–1.15: a model people keep rating down for a task drops behind.
    quality: 1 + confidence * (liked - 0.5) * 0.3,
  };
}

function resetForTests() {
  cache.clear();
  loadedTasks.clear();
}

module.exports = { recordOutcome, recordFeedback, adjust, resetForTests };
