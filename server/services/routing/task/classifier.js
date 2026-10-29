'use strict';

const { readSignals } = require('./signals');
const { KINDS, HARD_WORDS, SHORT_MESSAGE_TOKENS, hits, saturate } = require('./kinds');
const { logScale } = require('../scoring');

// What a chat request asks for: a mix of task kinds (kinds.js), the skills
// that mix calls on, and how hard it looks. Rule-based and instant, like the
// lightweight classifiers in front of hosted routers; a learned classifier
// could replace classify() with the same result shape.
const MIN_KIND_SCORE = 0.15;
const LENGTH_RANGE = [8, 3000];
const KIND_BY_ID = new Map(KINDS.map((kind) => [kind.id, kind]));

function difficultyOf(signals, ranked) {
  const length = logScale(Math.max(1, signals.lastUserTokens), LENGTH_RANGE) ?? 0;
  const scored = ranked.filter((item) => item.id !== 'chat');
  const total = scored.reduce((sum, item) => sum + item.score, 0);
  const kindPrior = total ? scored.reduce((sum, item) => sum + item.score * KIND_BY_ID.get(item.id).difficulty, 0) / total : 0.3;
  const hard = saturate(hits(HARD_WORDS, signals.lastUser), 2);
  const deep = signals.turns > 8 ? 0.1 : 0;
  const value = 0.35 * length + 0.4 * kindPrior + 0.25 * hard + deep;
  // Small talk stays easy whatever else the request carries.
  if (ranked[0]?.id === 'chat' && ranked[0].score >= 1) return Math.min(value, 0.1);
  return Math.min(1, Math.max(0, value));
}

/**
 * @param {object} request canonical chat request
 * @param {{ inputTokens?: number, hint?: string|null }} options
 *   hint: a task kind the client named (X-NeoRouter-Task), which wins
 * @returns {{ primary: string, kinds: object, skills: object, difficulty: number, hinted: boolean }}
 */
function classify(request, { inputTokens = 0, hint = null } = {}) {
  const signals = readSignals(request, { inputTokens });
  const scores = new Map(KINDS.map((kind) => [kind.id, Math.max(0, Math.min(1, kind.detect(signals) || 0))]));
  const hinted = Boolean(hint && KIND_BY_ID.has(hint));
  if (hinted) {
    for (const [id, score] of scores) scores.set(id, id === hint ? 1 : score * 0.5);
  }
  const ranked = [...scores].map(([id, score]) => ({ id, score })).filter((item) => item.score >= MIN_KIND_SCORE).sort((a, b) => b.score - a.score);
  // A short message that is nothing else ("ok, do it") is light conversation.
  if (!ranked.some((item) => item.id !== 'agent') && signals.lastUserTokens < SHORT_MESSAGE_TOKENS && !signals.toolResults) ranked.unshift({ id: 'chat', score: 0.5 });
  // The skills the mix calls on, each kind in proportion to how clearly it shows.
  const skills = {};
  for (const { id, score } of ranked) {
    for (const [skill, weight] of Object.entries(KIND_BY_ID.get(id).skills)) skills[skill] = (skills[skill] || 0) + score * weight;
  }
  const top = Math.max(0, ...Object.values(skills));
  for (const skill of Object.keys(skills)) skills[skill] = Number((skills[skill] / top).toFixed(3));
  return {
    primary: ranked[0]?.id || 'general',
    kinds: Object.fromEntries(ranked.map((item) => [item.id, Number(item.score.toFixed(3))])),
    skills,
    difficulty: Number(difficultyOf(signals, ranked).toFixed(3)),
    hinted,
  };
}

const TASK_KINDS = KINDS.map((kind) => kind.id);

module.exports = { classify, TASK_KINDS };
