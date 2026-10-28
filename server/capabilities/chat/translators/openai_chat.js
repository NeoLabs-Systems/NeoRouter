'use strict';

const { normalizeRequest } = require('../canonical');

// OpenAI Chat Completions is the canonical format, so this translator only
// normalizes the request and relabels the model the client asked for.
function toCanonical(body) {
  return normalizeRequest(body);
}

function responseFromCanonical(completion, model) {
  return { ...completion, model };
}

async function* streamFromCanonical(chunks, model) {
  for await (const chunk of chunks) {
    yield { data: { ...chunk, model } };
  }
  yield { data: '[DONE]' };
}

module.exports = { toCanonical, responseFromCanonical, streamFromCanonical };
