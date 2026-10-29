'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { classify } = require('../../server/services/routing/task/classifier');
const { taskIntelligence, skillsFromBenchmarks } = require('../../server/services/routing/task/skills');

const ask = (content, extra = {}) => classify({ messages: [{ role: 'user', content }], ...extra });

test('prompts are recognised by kind, in English and German', () => {
  const cases = [
    ['hi', 'chat'],
    ['Danke!', 'chat'],
    ['Write a python function that parses ISO dates', 'coding'],
    ['Fix this: TypeError: x is undefined at foo (app.js:12:5)', 'coding'],
    ['Prove that the square root of 2 is irrational', 'math'],
    ['Berechne das Integral von x^2', 'math'],
    ['Why is the sky blue? Explain step by step', 'reasoning'],
    ['Schreib mir ein Gedicht über den Herbst', 'writing'],
    ['Übersetze das ins Englische: Guten Morgen', 'translation'],
    ['Fasse diesen Artikel zusammen', 'summarization'],
    ['Extract the names and dates from this text as JSON', 'extraction'],
    ['What is the capital of France?', 'knowledge'],
  ];
  for (const [prompt, kind] of cases) assert.equal(ask(prompt).primary, kind, prompt);
});

test('difficulty separates small talk from hard work', () => {
  assert.ok(ask('hi').difficulty <= 0.1);
  const hard = ask('Design a scalable architecture for a chat backend with Kafka and Postgres, consider edge cases and failure modes');
  assert.ok(hard.difficulty >= 0.45, String(hard.difficulty));
  // An agent's long system prompt and tools do not make "hi" hard.
  const agent = classify({ messages: [{ role: 'system', content: 'You are an agent. '.repeat(800) }, { role: 'user', content: 'hi' }], tools: [{}, {}] }, { inputTokens: 9000 });
  assert.equal(agent.primary, 'chat');
  assert.ok(agent.difficulty <= 0.1);
});

test('a client hint names the task', () => {
  const hinted = classify({ messages: [{ role: 'user', content: 'Schreib mir ein Gedicht' }] }, { hint: 'coding' });
  assert.equal(hinted.primary, 'coding');
  assert.equal(hinted.hinted, true);
  assert.equal(classify({ messages: [{ role: 'user', content: 'hi' }] }, { hint: 'nonsense' }).hinted, false);
});

test('task intelligence moves towards the skills a task calls on, as far as benchmarks cover them', () => {
  const coder = { coding: 0.9, math: 0.3 };
  assert.ok(taskIntelligence(0.5, coder, { coding: 1 }) > 0.5);
  assert.ok(taskIntelligence(0.5, coder, { math: 1 }) < 0.5);
  assert.equal(taskIntelligence(0.5, null, { coding: 1 }), 0.5, 'no benchmarks: overall intelligence');
  assert.equal(taskIntelligence(0.5, coder, {}), 0.5, 'small talk calls on no skill');
  assert.deepEqual(skillsFromBenchmarks({ livecodebench: 0.8, scicode: 0.6, tau2: 0.5 }), { coding: 0.7, agentic: 0.5 });
});
