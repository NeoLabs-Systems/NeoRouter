'use strict';
const crypto = require('node:crypto');
const transport = require('../transport');
const privacy = require('../privacy');
const judge = require('./judge');
const objective = require('./objective');
const scoring = require('../../routing/scoring');

// One provider call per durable step. A normal request can interrupt any step;
// completed steps survive restarts instead of spending the same quota again.
async function step(row, payload, signal, adapters = transport) {
  const { sample, progress: p } = payload;
  const next = () => ({ state: 'continue', payload });
  const reject = (code) => ({ state: 'rejected', code });
  if (!p.selection) {
    const seen = new Set();
    const eligible = adapters.candidates(row, sample.canonical).filter((entry) => !seen.has(entry.canonicalId) && seen.add(entry.canonicalId));
    if (eligible.length < 4) throw new transport.Deferred('waiting_free_models');
    // Judge choice uses external intelligence only. Internal results never
    // choose or calibrate their own evaluators or candidate rankings.
    for (let i = eligible.length - 1; i > 0; i--) { const j = crypto.randomInt(i + 1); [eligible[i], eligible[j]] = [eligible[j], eligible[i]]; }
    const candidates = eligible.splice(0, 2);
    eligible.sort((a, b) => scoring.trustedIntelligence(b) - scoring.trustedIntelligence(a));
    const judges = eligible.slice(0, 2);
    const publicModel = (entry) => ({ ref: entry.ref, canonicalId: entry.canonicalId });
    p.selection = { candidates: candidates.map(publicModel), judges: judges.map(publicModel) };
    p.answers = []; p.calibrations = []; p.verdicts = []; p.cases = objective.calibration(); p.flip = !!crypto.randomInt(2);
    return next();
  }
  const ask = (model, request) => adapters.call(row, model.ref, request, signal);
  if (!p.reviewed) {
    const result = judge.review(await ask(p.selection.judges[0], judge.reviewRequest(sample)));
    if (!result || !result.safe || !result.deidentified || !result.verifiable) return reject('unsuitable_task');
    p.reviewed = true; return next();
  }
  if (p.answers.length < 2) {
    const answer = await ask(p.selection.candidates[p.answers.length], sample.canonical);
    const clean = await privacy.sanitize(row.user_id, sample.canonical, answer);
    if (!clean || !objective.valid(sample.canonical, clean.response)) return reject('objective_constraint_failed');
    p.answers.push(clean.response); return next();
  }
  if (p.calibrations.length < 4) {
    const index = p.calibrations.length;
    const known = p.cases[index % 2];
    const flipped = Math.floor(index / 2) % 2 === 0 ? p.flip : !p.flip;
    const result = judge.verdict(await ask(p.selection.judges[Math.floor(index / 2)], judge.gradeRequest(known.canonical, known.answers, flipped)), flipped);
    if (!result || result.score !== known.expected) return reject('judge_calibration_failed');
    p.calibrations.push(true); return next();
  }
  if (p.verdicts.length < 4) {
    const index = p.verdicts.length;
    const flipped = index % 2 === 0 ? p.flip : !p.flip;
    const result = judge.verdict(await ask(p.selection.judges[Math.floor(index / 2)], judge.gradeRequest(sample.canonical, p.answers, flipped)), flipped);
    if (!result) return reject('judge_abstained');
    if (p.verdicts.length && result.score !== p.verdicts[0].score) return reject('judge_disagreement');
    p.verdicts.push(result); return next();
  }
  return { state: 'validated', result: { candidates: p.selection.candidates, judges: p.selection.judges.map((model) => model.canonicalId),
    task: sample.task, score: p.verdicts[0].score, confidence: Math.min(...p.verdicts.map((value) => value.confidence)), evaluatorVersion: judge.VERSION } };
}
module.exports = { step };
