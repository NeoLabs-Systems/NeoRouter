'use strict';

// The live (speech-to-speech) WebSocket protocols NeoRouter speaks. Clients
// connect to the same path they would use at the provider, with a router key
// instead of the provider's, and NeoRouter relays the session to a provider
// that speaks the same protocol. Messages pass through unchanged except the
// model name in the opening message, so every feature of the protocol works.
// Each dialect says where the model is named, how to open the upstream
// socket and how its errors look.

const GEMINI_PATH = /^\/ws\/google\.ai\.generativelanguage\.(v1alpha|v1beta)\.GenerativeService\.BidiGenerateContent$/;

const toWs = (base) => String(base).replace(/^https:/i, 'wss:').replace(/^http:/i, 'ws:').replace(/\/+$/, '');

function openAiError(message, code) {
  return { type: 'error', error: { type: 'invalid_request_error', code, message } };
}

const DIALECTS = {
  // OpenAI GPT-Live: the model arrives in the first event, session.start.
  openai_live: {
    match: (path) => path === '/v1/live/sessions',
    modelFromUrl: () => null,
    modelFromFirst: (event) => (event?.type === 'session.start' ? String(event.session?.model || 'auto') : null),
    rewriteFirst: (event, modelId) => ({ ...event, session: { ...event.session, model: modelId } }),
    upstream: (adapter) => ({ url: `${toWs(adapter.baseUrl)}/live/sessions`, headers: adapter.headers() }),
    error: openAiError,
  },
  // OpenAI Realtime: the model is a URL parameter; the server speaks first.
  openai_realtime: {
    match: (path) => path === '/v1/realtime',
    modelFromUrl: (url) => url.searchParams.get('model') || 'auto',
    upstream: (adapter, modelId, { request }) => ({
      url: `${toWs(adapter.baseUrl)}/realtime?model=${encodeURIComponent(modelId)}`,
      headers: { ...adapter.headers(), ...(request.headers['openai-beta'] ? { 'OpenAI-Beta': request.headers['openai-beta'] } : {}) },
    }),
    error: openAiError,
  },
  // Gemini Live: the model arrives in the first message, setup.
  gemini_live: {
    match: (path) => GEMINI_PATH.test(path),
    modelFromUrl: () => null,
    modelFromFirst: (message) => (message?.setup ? String(message.setup.model || 'auto').replace(/^models\//, '') : null),
    rewriteFirst: (message, modelId) => ({ ...message, setup: { ...message.setup, model: `models/${modelId}` } }),
    upstream: (adapter, _modelId, { path }) => ({
      url: `${toWs(new URL(adapter.baseUrl).origin)}${path}?key=${encodeURIComponent(adapter.secret || '')}`,
      headers: {},
    }),
    // Gemini reports errors by closing the socket; the reason carries the text.
    error: null,
  },
};

function dialectFor(path) {
  const id = Object.keys(DIALECTS).find((name) => DIALECTS[name].match(path));
  return id ? { id, ...DIALECTS[id] } : null;
}

// Token counts from any of the protocols' usage reports, or null.
function usageOf(event) {
  const gemini = event?.usageMetadata;
  if (gemini) return { input: gemini.promptTokenCount || 0, output: gemini.responseTokenCount ?? gemini.candidatesTokenCount ?? 0 };
  const openai = event?.response?.usage || event?.usage;
  if (openai && (openai.input_tokens !== undefined || openai.output_tokens !== undefined)) return { input: openai.input_tokens || 0, output: openai.output_tokens || 0 };
  return null;
}

module.exports = { dialectFor, usageOf, DIALECTS };
