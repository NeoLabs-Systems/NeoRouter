'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { parse, family } = require('../../server/capabilities/media/system_one');
const { normalizeModel, parseUnitPricing } = require('../../server/providers/model_info');

// Outputs below were captured from the real models (Groq, NVIDIA, 2026-10-28).
test('Prompt Guard scores become a prompt_injection verdict', () => {
  assert.equal(family('meta-llama/llama-prompt-guard-2-86m'), 'prompt_guard');
  const attack = parse('prompt_guard', '0.999582827091217');
  assert.equal(attack.flagged, true);
  assert.equal(attack.categories.prompt_injection, true);
  assert.equal(parse('prompt_guard', '0.00035239275894127786').flagged, false);
  assert.throws(() => parse('prompt_guard', 'not a number'));
});

test('Llama Guard hazard codes map to OpenAI categories', () => {
  const unsafe = parse(family('meta/llama-guard-4-12b'), 'unsafe\nS9');
  assert.equal(unsafe.flagged, true);
  assert.equal(unsafe.categories['illicit/violent'], true);
  assert.equal(parse('llama_guard', 'safe').flagged, false);
});

test('NemoGuard answers in text or JSON', () => {
  assert.equal(family('nvidia/nemotron-3.5-content-safety'), 'nemoguard');
  assert.equal(parse('nemoguard', 'User Safety: unsafe').flagged, true);
  const json = parse('nemoguard', '{"User Safety": "unsafe", "Safety Categories": "Violence, Hate/Identity Hate"}');
  assert.equal(json.categories.violence, true);
  assert.equal(json.categories.hate, true);
  assert.equal(parse('nemoguard', '{"User Safety": "safe"}').flagged, false);
});

test('gpt-oss-safeguard JSON verdicts keep the rationale', () => {
  const verdict = parse(family('openai/gpt-oss-safeguard-20b'), '{"violation":1,"category":"Illicit behavior","rationale":"Providing instructions for making a pipe bomb is disallowed content."}');
  assert.equal(verdict.flagged, true);
  assert.equal(verdict.categories.illicit, true);
  assert.match(verdict.rationale, /pipe bomb/);
  assert.equal(parse('safeguard', '{"violation":0,"category":"none","rationale":"ok"}').flagged, false);
  assert.throws(() => parse('safeguard', 'I cannot help with that.'));
});

test('video listings carry a per-second price', () => {
  const pricing = parseUnitPricing({ pricing_skus: { cents_per_image_input: '1', cents_per_video_output_second_480p: '2', cents_per_video_output_second_720p: '3' } });
  assert.deepEqual(pricing, { priceUnit: 'second', unitPrice: 0.02 });
  assert.equal(parseUnitPricing({ pricing_skus: { duration_seconds_480p: '0.05', duration_seconds_720p: '0.1', reference_duration_seconds_480p: '0.01' } }).unitPrice, 0.05);
  assert.equal(normalizeModel({ id: 'x/y', supported_durations: [4] }).priceUnit, null);
  assert.equal(normalizeModel({ id: 'thinkingmachines/inkling-small' }).kind, 'chat');
  assert.equal(normalizeModel({ id: 'kling-v3' }).kind, 'video');
});
