'use strict';

// Skills a model can be strong or weak at, and the benchmarks that measure
// them. The metadata service turns each model's benchmark scores into a
// score per skill on the same scale as its overall intelligence; routing then
// weighs the skills a request calls on (classifier.js).
const SKILLS = Object.freeze(['coding', 'math', 'reasoning', 'knowledge', 'instruction', 'agentic', 'long_context']);

// Artificial Analysis benchmark → the skills it measures.
const BENCHMARK_SKILLS = Object.freeze({
  livecodebench: ['coding'],
  scicode: ['coding'],
  terminalbench_hard: ['coding', 'agentic'],
  aime_25: ['math'],
  aime: ['math'],
  math_500: ['math'],
  gpqa: ['reasoning'],
  hle: ['reasoning'],
  mmlu_pro: ['knowledge'],
  ifbench: ['instruction'],
  tau2: ['agentic'],
  lcr: ['long_context'],
});

// How far a known skill moves the estimate away from overall intelligence
// when the request's skills are fully covered by benchmarks.
const SPECIFIC_SHARE = 0.6;

/** Skill scores from benchmark scores already mapped to the intelligence scale. */
function skillsFromBenchmarks(mapped) {
  const sums = {};
  for (const [benchmark, value] of Object.entries(mapped)) {
    for (const skill of BENCHMARK_SKILLS[benchmark] || []) {
      sums[skill] = sums[skill] || { total: 0, count: 0 };
      sums[skill].total += value;
      sums[skill].count += 1;
    }
  }
  return Object.fromEntries(Object.entries(sums).map(([skill, { total, count }]) => [skill, Number(Math.min(1, total / count).toFixed(4))]));
}

/**
 * The model's intelligence for this task: its overall score, moved towards
 * its scores on the skills the task calls on, as far as benchmarks cover them.
 */
function taskIntelligence(general, modelSkills, taskSkills) {
  if (!modelSkills || !taskSkills) return general;
  let total = 0;
  let covered = 0;
  let sum = 0;
  for (const [skill, weight] of Object.entries(taskSkills)) {
    total += weight;
    const value = modelSkills[skill];
    if (!Number.isFinite(value)) continue;
    covered += weight;
    sum += weight * value;
  }
  if (!covered) return general;
  const share = SPECIFIC_SHARE * (covered / total);
  return (1 - share) * general + share * (sum / covered);
}

// How capable a model has to be for a task, on the intelligence scale, from
// its difficulty: a greeting needs little, a hard proof a frontier model.
const NEEDED_RANGE = [0.3, 0.95];

function neededIntelligence(difficulty) {
  if (!Number.isFinite(difficulty)) return null;
  const [low, high] = NEEDED_RANGE;
  return low + (high - low) * Math.min(1, Math.max(0, difficulty));
}

/**
 * Intelligence as the router weighs it: full credit up to what the task
 * needs, and beyond that only as far as the style cares about intelligence
 * (`headroom`, 0 to 1). Models that are good enough then compete on price,
 * speed and reliability, as in Azure's model router and RouteLLM.
 */
function enoughIntelligence(intelligence, need, headroom) {
  if (!need) return intelligence;
  const share = Math.min(1, Math.max(0, headroom));
  return (1 - share) * (Math.min(intelligence, need) / need) + share * intelligence;
}

module.exports = { SKILLS, BENCHMARK_SKILLS, skillsFromBenchmarks, taskIntelligence, neededIntelligence, enoughIntelligence };
