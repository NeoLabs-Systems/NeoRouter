'use strict';

const crypto = require('node:crypto');
const { BaseAdapter } = require('./base_adapter');
const { request, readSse, emptyResponseError } = require('../http');
const { normalizeModel } = require('../model_info');
const { classifyHttpError, ProviderError, KINDS } = require('../provider_error');
const { contentParts, textOf, completionId, buildResponse } = require('../../capabilities/chat/canonical');

const FINISH = { STOP: 'stop', MAX_TOKENS: 'length', SAFETY: 'content_filter', RECITATION: 'content_filter', PROHIBITED_CONTENT: 'content_filter', BLOCKLIST: 'content_filter', SPII: 'content_filter', MALFORMED_FUNCTION_CALL: 'stop' };
const SCHEMA_KEYS = new Set(['type', 'format', 'description', 'nullable', 'enum', 'properties', 'required', 'items', 'anyOf', 'minimum', 'maximum', 'minItems', 'maxItems', 'minLength', 'maxLength', 'pattern', 'title', 'propertyOrdering']);
// Gemini requires the thought signature of a function call to be echoed back
// on the next turn. OpenAI-style clients drop it, so keep a bounded map from
// tool-call id to signature; unknown ids use Google's documented bypass value.
const SIGNATURE_BYPASS = 'skip_thought_signature_validator';
const signatures = new Map();
const MAX_SIGNATURES = 5000;

function rememberSignature(id, signature) {
  if (!signature) return;
  signatures.set(id, signature);
  if (signatures.size > MAX_SIGNATURES) signatures.delete(signatures.keys().next().value);
}

function sanitizeSchema(schema) {
  if (Array.isArray(schema)) return schema.map(sanitizeSchema);
  if (!schema || typeof schema !== 'object') return schema;
  const result = {};
  for (const [key, value] of Object.entries(schema)) {
    if (!SCHEMA_KEYS.has(key)) continue;
    if (key === 'type' && Array.isArray(value)) {
      const types = value.filter((item) => item !== 'null');
      result.type = types[0] || 'string';
      if (value.includes('null')) result.nullable = true;
    } else if (key === 'properties') {
      result.properties = Object.fromEntries(Object.entries(value || {}).map(([name, inner]) => [name, sanitizeSchema(inner)]));
    } else {
      result[key] = sanitizeSchema(value);
    }
  }
  return result;
}

async function imagePart(part, signal) {
  const url = typeof part.image_url === 'string' ? part.image_url : part.image_url?.url;
  const match = /^data:([^;]+);base64,(.*)$/s.exec(url || '');
  if (match) return { inlineData: { mimeType: match[1], data: match[2] } };
  const response = await fetch(url, { signal: AbortSignal.any([signal || new AbortController().signal, AbortSignal.timeout(15_000)]) });
  const bytes = Buffer.from(await response.arrayBuffer());
  return { inlineData: { mimeType: response.headers.get('content-type') || 'image/png', data: bytes.toString('base64') } };
}

async function toGemini(canonical, modelId, signal) {
  const system = [];
  const contents = [];
  const toolNames = new Map();
  const push = (role, parts) => {
    const last = contents[contents.length - 1];
    if (last && last.role === role) last.parts.push(...parts);
    else contents.push({ role, parts });
  };
  for (const message of canonical.messages) {
    if (message.role === 'system' || message.role === 'developer') {
      system.push(textOf(message.content));
    } else if (message.role === 'assistant') {
      const parts = [];
      const text = textOf(message.content);
      if (text) parts.push({ text });
      for (const call of message.tool_calls || []) {
        toolNames.set(call.id, call.function?.name);
        let args = {};
        try { args = JSON.parse(call.function?.arguments || '{}'); } catch { args = {}; }
        parts.push({ functionCall: { name: call.function?.name, args }, thoughtSignature: signatures.get(call.id) || SIGNATURE_BYPASS });
      }
      if (parts.length) push('model', parts);
    } else if (message.role === 'tool') {
      const raw = textOf(message.content);
      let result;
      try { result = JSON.parse(raw); } catch { result = raw; }
      push('user', [{ functionResponse: { name: toolNames.get(message.tool_call_id) || 'tool', response: { result } } }]);
    } else {
      const parts = [];
      for (const part of contentParts(message.content)) {
        if (part.type === 'text') parts.push({ text: part.text || '' });
        else if (part.type === 'image_url') parts.push(await imagePart(part, signal));
      }
      if (parts.length) push('user', parts);
    }
  }
  const body = { contents };
  if (system.length) body.systemInstruction = { parts: [{ text: system.join('\n\n') }] };
  const generationConfig = {};
  if (canonical.temperature !== undefined) generationConfig.temperature = canonical.temperature;
  if (canonical.top_p !== undefined) generationConfig.topP = canonical.top_p;
  if (canonical.max_tokens !== undefined) generationConfig.maxOutputTokens = canonical.max_tokens;
  if (canonical.stop) generationConfig.stopSequences = Array.isArray(canonical.stop) ? canonical.stop : [canonical.stop];
  if (canonical.seed !== undefined) generationConfig.seed = canonical.seed;
  const format = canonical.response_format;
  if (format?.type === 'json_object') generationConfig.responseMimeType = 'application/json';
  if (format?.type === 'json_schema') {
    generationConfig.responseMimeType = 'application/json';
    if (format.json_schema?.schema) generationConfig.responseSchema = sanitizeSchema(format.json_schema.schema);
  }
  if (canonical.reasoning_effort && /gemini-(2\.5|[3-9])/.test(modelId)) {
    const effort = canonical.reasoning_effort === 'minimal' ? 'low' : canonical.reasoning_effort;
    generationConfig.thinkingConfig = /gemini-2\.5/.test(modelId)
      ? { thinkingBudget: { low: 1024, medium: 8192, high: 24576 }[effort] ?? -1 }
      : { thinkingLevel: effort === 'high' ? 'high' : 'low' };
  }
  if (Object.keys(generationConfig).length) body.generationConfig = generationConfig;
  if (canonical.tools?.length) {
    body.tools = [{
      functionDeclarations: canonical.tools.map((tool) => ({
        name: tool.function.name,
        description: tool.function.description || '',
        parameters: sanitizeSchema(tool.function.parameters || { type: 'object', properties: {} }),
      })),
    }];
    const choice = canonical.tool_choice;
    if (choice === 'required') body.toolConfig = { functionCallingConfig: { mode: 'ANY' } };
    else if (choice === 'none') body.toolConfig = { functionCallingConfig: { mode: 'NONE' } };
    else if (choice?.function?.name) body.toolConfig = { functionCallingConfig: { mode: 'ANY', allowedFunctionNames: [choice.function.name] } };
  }
  return body;
}

function usageOf(metadata) {
  if (!metadata) return null;
  const input = metadata.promptTokenCount || 0;
  const output = (metadata.candidatesTokenCount || 0) + (metadata.thoughtsTokenCount || 0);
  return { prompt_tokens: input, completion_tokens: output, total_tokens: input + output };
}

function partsToDelta(parts, toolCounter) {
  const delta = {};
  for (const part of parts || []) {
    if (part.functionCall) {
      const id = part.functionCall.id || `call_${crypto.randomBytes(8).toString('hex')}`;
      rememberSignature(id, part.thoughtSignature);
      delta.tool_calls = delta.tool_calls || [];
      delta.tool_calls.push({
        index: toolCounter.next++,
        id,
        type: 'function',
        function: { name: part.functionCall.name, arguments: JSON.stringify(part.functionCall.args || {}) },
      });
    } else if (typeof part.text === 'string') {
      if (part.thought) delta.reasoning_content = (delta.reasoning_content || '') + part.text;
      else delta.content = (delta.content || '') + part.text;
    }
  }
  return delta;
}

function assertCandidate(data) {
  if (data?.error) throw classifyHttpError(Number(data.error.code) || 502, data, null);
  if (data?.promptFeedback?.blockReason && !data.candidates?.length) {
    throw classifyHttpError(400, { error: { message: `Prompt blocked: ${data.promptFeedback.blockReason}` } }, null);
  }
}

class GoogleGeminiAdapter extends BaseAdapter {
  authHeaders() {
    return this.secret ? { 'x-goog-api-key': this.secret } : {};
  }

  async listModels() {
    const models = [];
    let token = null;
    for (let page = 0; page < 20; page += 1) {
      const { data } = await this.http(`/models?pageSize=1000${token ? `&pageToken=${encodeURIComponent(token)}` : ''}`, { timeoutMs: 30_000 });
      for (const entry of data?.models || []) {
        const methods = entry.supportedGenerationMethods || [];
        const id = String(entry.name || '').replace(/^models\//, '');
        if (methods.includes('embedContent')) {
          models.push(normalizeModel({ ...entry, id, name: entry.displayName || id, type: 'embedding' }, { contextWindow: entry.inputTokenLimit || null }));
          continue;
        }
        // Gemini Live models only speak the bidirectional WebSocket API.
        if (methods.includes('bidiGenerateContent') && !methods.includes('generateContent')) {
          models.push(normalizeModel({ ...entry, id, name: entry.displayName || id, type: 'live' }, { contextWindow: entry.inputTokenLimit || null }));
          continue;
        }
        if (!methods.includes('generateContent')) continue;
        models.push(normalizeModel({ ...entry, id, name: entry.displayName || id }, {
          tools: !/gemma|tts|image|embedding|aqa/i.test(id),
          vision: !/tts|embedding|aqa/i.test(id),
          json: !/gemma|tts/i.test(id),
          reasoning: entry.thinking === true || /gemini-(2\.5|[3-9])/.test(id),
        }));
      }
      token = data?.nextPageToken;
      if (!token) break;
    }
    return models;
  }

  async chat(canonical, { modelId, signal, timeoutMs }) {
    const { data, headers } = await this.http(`/models/${encodeURIComponent(modelId)}:generateContent`, {
      method: 'POST',
      body: await toGemini(canonical, modelId, signal),
      signal,
      timeoutMs: timeoutMs || this.timeoutMs,
    });
    assertCandidate(data);
    const candidate = data.candidates?.[0];
    if (!candidate) throw emptyResponseError();
    const delta = partsToDelta(candidate.content?.parts, { next: 0 });
    const message = { role: 'assistant', content: delta.content || (delta.tool_calls ? null : '') };
    if (delta.tool_calls) message.tool_calls = delta.tool_calls.map(({ index, ...call }) => call);
    if (delta.reasoning_content) message.reasoning_content = delta.reasoning_content;
    return {
      response: buildResponse({
        id: data.responseId || completionId(),
        model: modelId,
        message,
        finishReason: delta.tool_calls ? 'tool_calls' : FINISH[candidate.finishReason] || 'stop',
        usage: usageOf(data.usageMetadata),
      }),
      headers,
    };
  }

  async *stream(canonical, { modelId, signal, onHeaders }) {
    const response = await request(this.url(`/models/${encodeURIComponent(modelId)}:streamGenerateContent?alt=sse`), {
      method: 'POST',
      headers: this.headers({ Accept: 'text/event-stream' }),
      body: await toGemini(canonical, modelId, signal),
      signal,
      raw: true,
    });
    onHeaders?.(response.headers);
    const id = completionId();
    const created = Math.floor(Date.now() / 1000);
    const counter = { next: 0 };
    let usage = null;
    let finish = null;
    let sawTools = false;
    for await (const { data } of readSse(response)) {
      let payload;
      try { payload = JSON.parse(data); } catch { continue; }
      assertCandidate(payload);
      if (payload.usageMetadata) usage = usageOf(payload.usageMetadata);
      const candidate = payload.candidates?.[0];
      if (!candidate) continue;
      const delta = partsToDelta(candidate.content?.parts, counter);
      if (delta.tool_calls) sawTools = true;
      if (Object.keys(delta).length) {
        yield { id, object: 'chat.completion.chunk', created, model: modelId, choices: [{ index: 0, delta, finish_reason: null }] };
      }
      if (candidate.finishReason) finish = FINISH[candidate.finishReason] || 'stop';
    }
    yield {
      id, object: 'chat.completion.chunk', created, model: modelId,
      choices: [{ index: 0, delta: {}, finish_reason: sawTools ? 'tool_calls' : finish || 'stop' }],
      ...(usage ? { usage } : {}),
    };
  }
}

// Gemini has its own voice names; OpenAI clients send theirs.
const OPENAI_VOICES = { alloy: 'Kore', echo: 'Puck', fable: 'Fenrir', onyx: 'Charon', nova: 'Aoede', shimmer: 'Leda', ash: 'Orus', coral: 'Callirrhoe', sage: 'Schedar', ballad: 'Achird', verse: 'Zephyr' };
const GEMINI_VOICES = new Set(['Zephyr', 'Puck', 'Charon', 'Kore', 'Fenrir', 'Leda', 'Orus', 'Aoede', 'Callirrhoe', 'Autonoe', 'Enceladus', 'Iapetus', 'Umbriel', 'Algieba', 'Despina', 'Erinome', 'Algenib', 'Rasalgethi', 'Laomedeia', 'Achernar', 'Alnilam', 'Schedar', 'Gacrux', 'Pulcherrima', 'Achird', 'Zubenelgenubi', 'Vindemiatrix', 'Sadachbia', 'Sadaltager', 'Sulafat']);

function geminiVoice(voice) {
  const name = String(voice || '');
  const proper = name.charAt(0).toUpperCase() + name.slice(1).toLowerCase();
  if (GEMINI_VOICES.has(proper)) return proper;
  return OPENAI_VOICES[name.toLowerCase()] || 'Kore';
}

// Raw 16-bit PCM ("audio/L16;rate=24000") gets a WAV header so players accept it.
function wavFromPcm(pcm, mime) {
  const rate = Number(/rate=(\d+)/.exec(mime)?.[1]) || 24000;
  const header = Buffer.alloc(44);
  header.write('RIFF', 0); header.writeUInt32LE(36 + pcm.length, 4); header.write('WAVE', 8);
  header.write('fmt ', 12); header.writeUInt32LE(16, 16); header.writeUInt16LE(1, 20); header.writeUInt16LE(1, 22);
  header.writeUInt32LE(rate, 24); header.writeUInt32LE(rate * 2, 28); header.writeUInt16LE(2, 32); header.writeUInt16LE(16, 34);
  header.write('data', 36); header.writeUInt32LE(pcm.length, 40);
  return Buffer.concat([header, pcm]);
}

Object.assign(GoogleGeminiAdapter.prototype, {
  // Gemini transcripts carry no timestamps, so subtitles go elsewhere when possible.
  transcribeTimestamps: false,

  supports(method) {
    return ['chat', 'embed', 'speech', 'transcribe', 'generateImage'].includes(method);
  },

  // Gemini image models ("Nano Banana") answer generateContent with image parts.
  async generateImage({ prompt, n = 1, size }, { modelId, signal, timeoutMs }) {
    const ratio = /^(\d+)x(\d+)$/.exec(String(size || ''));
    const images = [];
    let headers = null;
    for (let index = 0; index < Math.min(n, 4); index += 1) {
      const body = {
        contents: [{ parts: [{ text: prompt }] }],
        generationConfig: { responseModalities: ['TEXT', 'IMAGE'], ...(ratio ? { imageConfig: { aspectRatio: require('./openai_media').aspectRatio(size) } } : {}) },
      };
      const result = await this.http(`/models/${encodeURIComponent(modelId)}:generateContent`, { method: 'POST', body, signal, timeoutMs });
      headers = result.headers;
      assertCandidate(result.data);
      for (const part of result.data.candidates?.[0]?.content?.parts || []) {
        if (part.inlineData?.data && /^image\//.test(part.inlineData.mimeType || '')) images.push({ b64: part.inlineData.data, url: null, mime: part.inlineData.mimeType, revisedPrompt: null });
      }
    }
    if (!images.length) throw new ProviderError(KINDS.EMPTY, 'Gemini returned no image.');
    return { images, headers };
  },

  async embed({ input, dimensions, inputType }, { modelId, signal, timeoutMs }) {
    // Only the first-generation model takes a task type.
    const taskType = /embedding-001$/.test(modelId) ? (inputType === 'passage' ? 'RETRIEVAL_DOCUMENT' : 'RETRIEVAL_QUERY') : undefined;
    const requests = input.map((text) => ({
      model: `models/${modelId}`,
      content: { parts: [{ text }] },
      ...(dimensions ? { outputDimensionality: dimensions } : {}),
      ...(taskType ? { taskType } : {}),
    }));
    const { data, headers } = await this.http(`/models/${encodeURIComponent(modelId)}:batchEmbedContents`, { method: 'POST', body: { requests }, signal, timeoutMs });
    assertCandidate(data);
    const vectors = (data?.embeddings || []).map((item) => item.values);
    if (vectors.length !== input.length || !vectors.every(Array.isArray)) throw new ProviderError(KINDS.INVALID_OUTPUT, 'Gemini returned an unexpected embeddings response.');
    return { vectors, inputTokens: null, headers };
  },

  async speech({ input, voice }, { modelId, signal, timeoutMs }) {
    const body = {
      contents: [{ parts: [{ text: input }] }],
      generationConfig: { responseModalities: ['AUDIO'], speechConfig: { voiceConfig: { prebuiltVoiceConfig: { voiceName: geminiVoice(voice) } } } },
    };
    const { data, headers } = await this.http(`/models/${encodeURIComponent(modelId)}:generateContent`, { method: 'POST', body, signal, timeoutMs });
    assertCandidate(data);
    const part = (data.candidates?.[0]?.content?.parts || []).find((item) => item.inlineData?.data);
    if (!part) throw emptyResponseError();
    let audio = Buffer.from(part.inlineData.data, 'base64');
    let mime = part.inlineData.mimeType || 'audio/wav';
    if (/l16|pcm/i.test(mime)) { audio = wavFromPcm(audio, mime); mime = 'audio/wav'; }
    return { audio, mime, outputTokens: data.usageMetadata?.candidatesTokenCount ?? null, headers };
  },

  // Transcribe models answer with an audioTranscription part; any other
  // Gemini model with audio input transcribes when asked to.
  async transcribe({ file, mime, language, prompt, translate = false }, { modelId, signal, timeoutMs }) {
    const instruction = translate
      ? 'Translate the speech in this audio into English. Reply with the English text only.'
      : `Transcribe this audio exactly${language ? ` (language: ${language})` : ''}. Reply with the transcript only.`;
    const body = {
      contents: [{ parts: [{ text: prompt ? `${instruction} Context: ${prompt}` : instruction }, { inlineData: { mimeType: mime || 'audio/wav', data: Buffer.from(file).toString('base64') } }] }],
    };
    const { data, headers } = await this.http(`/models/${encodeURIComponent(modelId)}:generateContent`, { method: 'POST', body, signal, timeoutMs });
    assertCandidate(data);
    const parts = data.candidates?.[0]?.content?.parts || [];
    const text = parts.map((part) => part.audioTranscription?.text ?? part.text ?? '').join('').trim();
    if (!text) throw emptyResponseError();
    return { text, language: language || null, duration: null, segments: null, inputTokens: data.usageMetadata?.promptTokenCount ?? null, headers };
  },
});

module.exports = { GoogleGeminiAdapter, sanitizeSchema, geminiVoice, wavFromPcm };
