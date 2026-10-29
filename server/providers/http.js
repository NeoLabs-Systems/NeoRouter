'use strict';

const { classifyHttpError, fromNetworkError, ProviderError, KINDS } = require('./provider_error');
const { isPrivateDestination } = require('./net_guard');

function combineSignals(signal, timeoutMs) {
  const signals = [];
  if (signal) signals.push(signal);
  if (timeoutMs) signals.push(AbortSignal.timeout(timeoutMs));
  if (signals.length === 0) return undefined;
  return signals.length === 1 ? signals[0] : AbortSignal.any(signals);
}

function isRawBody(body) {
  return body instanceof FormData || body instanceof Uint8Array || body instanceof ArrayBuffer || body instanceof Blob;
}

async function readBody(response) {
  const text = await response.text();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

const MAX_REDIRECTS = 5;

// With publicOnly, every hop (the URL and each redirect) must resolve to a
// public address, so an account cannot reach the server's own network.
async function send(url, init, publicOnly) {
  if (!publicOnly) return fetch(url, { ...init, redirect: 'follow' });
  let current = url;
  let options = init;
  for (let hop = 0; ; hop += 1) {
    if (await isPrivateDestination(current)) {
      throw new ProviderError(KINDS.BAD_REQUEST, 'This address is on a local or private network. Only the instance admin can use those.');
    }
    const response = await fetch(current, { ...options, redirect: 'manual' });
    const location = response.status >= 300 && response.status < 400 ? response.headers.get('location') : null;
    if (!location) return response;
    if (hop >= MAX_REDIRECTS) throw new ProviderError(KINDS.NETWORK, 'Too many redirects.');
    current = new URL(location, current).toString();
    if (response.status === 303 || ((response.status === 301 || response.status === 302) && options.method === 'POST')) {
      options = { ...options, method: 'GET', body: undefined };
    }
  }
}

// Performs one upstream HTTP call. Non-2xx responses become classified
// ProviderErrors; network failures and timeouts are classified too.
async function request(url, { method = 'GET', headers = {}, body, signal, timeoutMs, raw = false, publicOnly = false } = {}) {
  let response;
  try {
    response = await send(url, {
      method,
      // FormData and binary bodies (audio uploads) go out as they are.
      headers: body !== undefined && !isRawBody(body) ? { 'Content-Type': 'application/json', ...headers } : headers,
      body: body === undefined ? undefined : isRawBody(body) ? body : JSON.stringify(body),
      signal: combineSignals(signal, timeoutMs),
    }, publicOnly);
  } catch (error) {
    if (error instanceof ProviderError) throw error;
    throw fromNetworkError(error);
  }
  if (!response.ok) {
    let payload = null;
    try { payload = await readBody(response); } catch { payload = null; }
    throw classifyHttpError(response.status, payload, response.headers);
  }
  if (raw) return response;
  let data;
  try {
    data = await readBody(response);
  } catch (error) {
    throw fromNetworkError(error);
  }
  return { data, headers: response.headers, status: response.status };
}

// Async generator over Server-Sent Events. Yields { event, data } where data
// is the raw string; callers decide how to parse it.
async function* readSse(response) {
  const decoder = new TextDecoder();
  let buffer = '';
  let event = null;
  let dataLines = [];
  try {
    for await (const chunk of response.body) {
      buffer += decoder.decode(chunk, { stream: true });
      let index;
      while ((index = buffer.search(/\r?\n/)) >= 0) {
        const line = buffer.slice(0, index);
        buffer = buffer.slice(index + (buffer[index] === '\r' ? 2 : 1));
        if (line === '') {
          if (dataLines.length) yield { event, data: dataLines.join('\n') };
          event = null;
          dataLines = [];
          continue;
        }
        if (line.startsWith(':')) continue;
        const colon = line.indexOf(':');
        const field = colon === -1 ? line : line.slice(0, colon);
        const value = colon === -1 ? '' : line.slice(colon + 1).replace(/^ /, '');
        if (field === 'event') event = value;
        else if (field === 'data') dataLines.push(value);
      }
    }
  } catch (error) {
    throw fromNetworkError(error);
  }
  if (dataLines.length) yield { event, data: dataLines.join('\n') };
}

// Some providers answer 200 with an error object embedded in the body or in
// the first stream event. Treat those as real failures so routing learns.
function assertNoEmbeddedError(payload) {
  if (payload && typeof payload === 'object' && payload.error && !payload.choices && !payload.candidates) {
    const status = Number(payload.error.code || payload.error.status) || 502;
    const error = classifyHttpError(status >= 400 ? status : 502, payload, null);
    throw error;
  }
}

function emptyResponseError() {
  return new ProviderError(KINDS.EMPTY, 'The provider returned an empty response.');
}

module.exports = { request, readSse, assertNoEmbeddedError, emptyResponseError, combineSignals };
