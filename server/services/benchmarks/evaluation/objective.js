'use strict';
const crypto = require('node:crypto');
const { invalidToolCalls } = require('../../../capabilities/chat/canonical');

// Known-answer calibration is generated locally, not inferred from production
// answers. No user code is ever run, and a production answer is not ground truth.
function calibration() {
  const a = crypto.randomInt(100, 10000), b = crypto.randomInt(100, 10000);
  const value = a + b;
  return [
    { canonical: { messages: [{ role: 'user', content: `Return only a JSON object with property value equal to ${a} + ${b}.` }] },
      answers: [{ role: 'assistant', content: JSON.stringify({ value }) }, { role: 'assistant', content: JSON.stringify({ value: value + 1 }) }], expected: 1 },
    { canonical: { messages: [{ role: 'user', content: 'What exact integer did a private random generator produce? No seed, state or output is supplied.' }] },
      answers: [{ role: 'assistant', content: 'The exact integer is 42. This is certain.' }, { role: 'assistant', content: 'The value cannot be determined without the generator output or sufficient state.' }], expected: 0 },
  ];
}
// Objective constraints are rejection gates, not proof of semantic correctness.
function valid(canonical, message) {
  if (!message || invalidToolCalls(message.tool_calls)) return false;
  if (canonical.tools?.length && (canonical.tool_choice === 'required' || typeof canonical.tool_choice === 'object') && !message.tool_calls?.length) return false;
  if (!message.tool_calls?.length && !(typeof message.content === 'string' && message.content.trim())) return false;
  if (canonical.tool_choice === 'none' && message.tool_calls?.length) return false;
  for (const call of message.tool_calls || []) {
    const definition = canonical.tools?.find((tool) => tool.function?.name === call.function.name);
    if (!definition) return false;
    const required = canonical.tool_choice?.function?.name;
    if (required && call.function.name !== required) return false;
  }
  if (canonical.response_format?.type === 'json_object' || canonical.response_format?.type === 'json_schema') {
    try { const value = JSON.parse(message.content); if (canonical.response_format.type === 'json_object' && (value === null || typeof value !== 'object' || Array.isArray(value))) return false; } catch { return false; }
  }
  return true;
}
module.exports = { VERSION: 'neorouter-constraints/1', calibration, valid };
