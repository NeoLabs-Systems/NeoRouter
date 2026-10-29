'use strict';

// System One models: small, fast classifiers that answer a yes/no question
// (is this unsafe? is this a prompt injection?) instead of writing text. Most
// providers serve them through chat completions with model-specific output
// formats; this module talks to each family and returns the OpenAI
// /v1/moderations result shape, so clients get one format whatever ran.
const { ProviderError, KINDS } = require('../../providers/provider_error');

// Llama Guard / NemoGuard hazard codes (MLCommons taxonomy) → OpenAI categories.
const HAZARDS = {
  S1: 'violence', S2: 'illicit', S3: 'sexual', S4: 'sexual/minors', S5: 'harassment', S6: 'illicit',
  S7: 'privacy', S8: 'intellectual_property', S9: 'illicit/violent', S10: 'hate', S11: 'self-harm',
  S12: 'sexual', S13: 'elections', S14: 'illicit',
};
const CATEGORIES = ['harassment', 'harassment/threatening', 'hate', 'hate/threatening', 'illicit', 'illicit/violent', 'self-harm', 'self-harm/intent', 'self-harm/instructions', 'sexual', 'sexual/minors', 'violence', 'violence/graphic', 'prompt_injection', 'privacy', 'intellectual_property', 'elections'];

const DEFAULT_POLICY = `Classify the user content against this safety policy.
Violations: instructions or encouragement for violence, weapons, terrorism, crime, drugs, hacking or fraud; sexual content involving minors; explicit sexual content; hate or harassment against protected groups; self-harm encouragement or instructions; sharing other people's private data.
Allowed: everything else, including discussion, news, fiction and education about these topics without actionable harm.
Answer only with JSON: {"violation": 0 or 1, "category": "<short category or none>", "rationale": "<one sentence>"}`;

function family(modelId) {
  const id = String(modelId).toLowerCase();
  if (/prompt-guard/.test(id)) return 'prompt_guard';
  if (/safeguard/.test(id)) return 'safeguard';
  if (/llama-guard/.test(id)) return 'llama_guard';
  if (/nemoguard|content-safety|safety-guard/.test(id)) return 'nemoguard';
  if (/topic-control/.test(id)) return 'topic_control';
  if (/shieldgemma/.test(id)) return 'shieldgemma';
  if (/moderation/.test(id)) return 'native';
  // Decision models (POST /v1/systemone), not classifiers.
  if (/(^|\/)jev|decide/.test(id)) return 'decision';
  return 'generic';
}

function emptyResult() {
  return {
    flagged: false,
    categories: Object.fromEntries(CATEGORIES.map((name) => [name, false])),
    category_scores: Object.fromEntries(CATEGORIES.map((name) => [name, 0])),
  };
}

function flag(result, category, score = 1) {
  const name = CATEGORIES.includes(category) ? category : 'illicit';
  result.flagged = true;
  result.categories[name] = true;
  result.category_scores[name] = Math.max(result.category_scores[name] || 0, score);
}

// Free-text categories ("Illicit behavior", "Violence") → nearest OpenAI name.
function categoryFromText(text) {
  const value = String(text || '').toLowerCase();
  if (/minor|child/.test(value)) return 'sexual/minors';
  if (/sex/.test(value)) return 'sexual';
  if (/self.?harm|suicide/.test(value)) return 'self-harm';
  if (/hate/.test(value)) return 'hate';
  if (/harass|bully/.test(value)) return 'harassment';
  if (/weapon|bomb|terror|violent crime/.test(value)) return 'illicit/violent';
  if (/violen/.test(value)) return 'violence';
  if (/privacy|personal data|pii/.test(value)) return 'privacy';
  if (/injection|jailbreak/.test(value)) return 'prompt_injection';
  return 'illicit';
}

function messagesFor(kind, input, policy) {
  if (kind === 'safeguard' || kind === 'generic') {
    return [{ role: 'system', content: policy || DEFAULT_POLICY }, { role: 'user', content: input }];
  }
  if (kind === 'topic_control') {
    return [{ role: 'system', content: policy || 'You are a general assistant. Allowed topics: anything legal and safe.' }, { role: 'user', content: input }];
  }
  return [{ role: 'user', content: input }];
}

function parse(kind, text) {
  const result = emptyResult();
  const raw = String(text || '').trim();
  if (kind === 'prompt_guard') {
    // Prompt Guard 2 answers with the probability that the text is an attack.
    const score = Number(raw);
    if (!Number.isFinite(score)) throw new ProviderError(KINDS.INVALID_OUTPUT, 'Prompt Guard returned no score.');
    result.category_scores.prompt_injection = score;
    if (score >= 0.5) flag(result, 'prompt_injection', score);
    return result;
  }
  if (kind === 'llama_guard' || kind === 'shieldgemma') {
    // "safe" or "unsafe\nS9,S2" (ShieldGemma: "Yes"/"No").
    if (/^(unsafe|yes)\b/i.test(raw)) {
      const codes = raw.match(/S\d{1,2}/g) || [];
      if (!codes.length) flag(result, 'illicit');
      for (const code of codes) flag(result, HAZARDS[code] || 'illicit');
    } else if (!/^(safe|no)\b/i.test(raw)) {
      throw new ProviderError(KINDS.INVALID_OUTPUT, 'The guard model returned an unexpected verdict.');
    }
    return result;
  }
  if (kind === 'nemoguard' || kind === 'topic_control') {
    // JSON ({"User Safety": "unsafe", "Safety Categories": "Violence, ..."}) or "User Safety: unsafe".
    let verdict = null;
    let categories = '';
    try {
      const json = JSON.parse(raw.replace(/^```(json)?|```$/g, ''));
      verdict = json['User Safety'] || json.user_safety || json['Response Safety'];
      categories = json['Safety Categories'] || json.categories || '';
    } catch {
      verdict = /user safety:\s*(\w+)/i.exec(raw)?.[1] || (/off-topic|unsafe/i.test(raw) ? 'unsafe' : /on-topic|safe/i.test(raw) ? 'safe' : null);
      categories = /safety categories:\s*(.+)/i.exec(raw)?.[1] || '';
    }
    if (!verdict) throw new ProviderError(KINDS.INVALID_OUTPUT, 'The guard model returned an unexpected verdict.');
    if (/unsafe|off-topic/i.test(verdict)) {
      const names = String(categories).split(/,\s*/).filter(Boolean);
      if (!names.length) flag(result, 'illicit');
      for (const name of names) flag(result, categoryFromText(name));
    }
    return result;
  }
  // gpt-oss-safeguard and any instruction model: the JSON contract above.
  const match = /\{[\s\S]*\}/.exec(raw);
  let json = null;
  try { json = match ? JSON.parse(match[0]) : null; } catch { json = null; }
  if (!json || (json.violation === undefined && json.flagged === undefined)) {
    throw new ProviderError(KINDS.INVALID_OUTPUT, 'The classifier did not answer in the expected JSON format.');
  }
  if (Number(json.violation ?? (json.flagged ? 1 : 0)) === 1) flag(result, categoryFromText(json.category));
  result.rationale = json.rationale || null;
  return result;
}

// OpenAI-style moderation endpoints (Mistral, OpenAI): categories are already named.
function fromNative(results) {
  return results.map((item) => {
    const result = emptyResult();
    for (const [name, value] of Object.entries(item.categories || {})) {
      const scoreValue = Number(item.category_scores?.[name] ?? (value ? 1 : 0));
      const key = CATEGORIES.includes(name) ? name : categoryFromText(name);
      result.category_scores[key] = Math.max(result.category_scores[key] || 0, scoreValue);
      if (value) flag(result, key, scoreValue);
    }
    if (item.flagged) result.flagged = true;
    return result;
  });
}

/**
 * Classifies each input with one model. Uses the provider's moderation
 * endpoint when the model is a native moderation model and the adapter has
 * one; otherwise one chat call per input.
 */
async function classify(adapter, { inputs, policy }, { modelId, signal, timeoutMs }) {
  const kind = family(modelId);
  if (kind === 'native' && adapter.supports('moderate')) {
    const { results, headers } = await adapter.moderate({ input: inputs }, { modelId, signal, timeoutMs });
    return { results: fromNative(results), headers };
  }
  const results = [];
  let headers = null;
  let inputTokens = 0;
  for (const input of inputs) {
    const canonical = { messages: messagesFor(kind, input, policy), max_tokens: kind === 'prompt_guard' ? 16 : 200, temperature: 0 };
    if (kind === 'safeguard' || kind === 'generic') canonical.response_format = { type: 'json_object' };
    const { response, headers: responseHeaders } = await adapter.chat(canonical, { modelId, signal, timeoutMs });
    headers = responseHeaders;
    inputTokens += response.usage?.prompt_tokens || 0;
    results.push(parse(kind, response.choices?.[0]?.message?.content));
  }
  return { results, headers, inputTokens };
}

module.exports = { classify, parse, family, fromNative, CATEGORIES, DEFAULT_POLICY };
