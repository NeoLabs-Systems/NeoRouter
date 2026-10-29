'use strict';

// Built-in web search for chat. Clients ask for it in their own dialect:
//   OpenAI Chat      web_search_options: {}  (or OpenRouter's plugins: [{ id: 'web' }], model "…:online")
//   OpenAI Responses tools: [{ type: 'web_search' | 'web_search_preview' }]
//   Anthropic        tools: [{ type: 'web_search_20250305', max_uses, allowed_domains, blocked_domains }]
// NeoRouter then offers the routed model a plain `web_search` function,
// runs its calls through the user's search engines (routing, fallback,
// quotas like any other kind) and repeats until the model answers. Inner
// turns are non-streaming; the answer is delivered in the client's format.
const { searchWeb } = require('../media/handlers');
const catalog = require('../../services/catalog/catalog_service');
const requestProcessing = require('../../services/processing/processing_service');
const { textOf } = require('./canonical');

const TOOL_NAME = 'web_search';
const DEFAULT_MAX_USES = 5;
const MAX_USES_CEILING = 20;
const RESULTS_PER_SEARCH = 5;
const EXTRACT_CHARS = 1200;

const WEB_TOOL = {
  type: 'function',
  function: {
    name: TOOL_NAME,
    description: 'Search the web for current information. Returns the top results with title, URL and an extract.',
    parameters: {
      type: 'object',
      properties: { query: { type: 'string', description: 'A focused search query, like one typed into a search engine.' } },
      required: ['query'],
    },
  },
};

const INSTRUCTION = 'You can search the web with the web_search tool. Search when the question needs current or specific facts, '
  + 'then answer from the results and cite the sources you used as markdown links.';

const clampUses = (value) => {
  const number = Number(value);
  return Number.isFinite(number) && number >= 1 ? Math.min(MAX_USES_CEILING, Math.floor(number)) : DEFAULT_MAX_USES;
};

/** What the client asked for, or null. Also returns the body without the marker. */
function requested(format, body = {}) {
  if (format === 'openai_chat') {
    const plugin = (body.plugins || []).find((item) => item?.id === 'web');
    const online = typeof body.model === 'string' && body.model.endsWith(':online');
    if (!body.web_search_options && !plugin && !online) return null;
    const { web_search_options: _options, plugins, ...rest } = body;
    return {
      maxUses: DEFAULT_MAX_USES,
      includeDomains: [],
      excludeDomains: [],
      body: { ...rest, ...(plugins ? { plugins: plugins.filter((item) => item?.id !== 'web') } : {}), ...(online ? { model: body.model.slice(0, -':online'.length) } : {}) },
    };
  }
  const tools = Array.isArray(body.tools) ? body.tools : [];
  const isSearch = format === 'openai_responses'
    ? (tool) => /^web_search(_preview)?(_\d+)?$/.test(tool?.type || '')
    : (tool) => /^web_search_\d+$/.test(tool?.type || '');
  const tool = tools.find(isSearch);
  if (!tool) return null;
  return {
    maxUses: clampUses(tool.max_uses),
    includeDomains: tool.allowed_domains || tool.filters?.allowed_domains || [],
    excludeDomains: tool.blocked_domains || tool.filters?.blocked_domains || [],
    body: { ...body, tools: tools.filter((item) => !isSearch(item)) },
  };
}

function available(userId) {
  return catalog.listForUser(userId, { capability: 'search' }).some((entry) => entry.enabled && entry.accountEnabled);
}

function formatResults(query, results) {
  if (!results.length) return `No results for "${query}".`;
  return results.map((item, index) => [
    `[${index + 1}] ${item.title}`,
    item.url,
    item.published ? `Published: ${item.published}` : null,
    String(item.content || item.snippet || '').slice(0, EXTRACT_CHARS),
  ].filter(Boolean).join('\n')).join('\n\n');
}

// The model only saw placeholders; a query rebuilt with real values must be
// hidden again before it leaves for a search engine.
async function shieldQuery(userId, query) {
  try {
    const processed = await requestProcessing.apply(userId, { messages: [{ role: 'user', content: query }] }, { compressionLevel: 'off' });
    return textOf([...processed.canonical.messages].reverse().find((message) => message.role === 'user')?.content) || query;
  } catch {
    return null;
  }
}

async function runSearch({ req, key, spec, freeOnly, call }) {
  let query = '';
  try { query = String(JSON.parse(call.function?.arguments || '{}').query || '').trim(); } catch { query = ''; }
  if (!query || query.length > 400) return { text: 'Search skipped: the query must be 1–400 characters.', searched: false };
  const safeQuery = await shieldQuery(key.user_id, query);
  if (!safeQuery) return { text: 'Search unavailable: the privacy shield could not check the query.', searched: false };
  try {
    const { result } = await searchWeb({
      req, key, freeOnly, format: 'web_search_tool',
      input: { query: safeQuery, max_results: RESULTS_PER_SEARCH, include_domains: spec.includeDomains, exclude_domains: spec.excludeDomains },
    });
    return { text: formatResults(safeQuery, result.results), searched: true };
  } catch (error) {
    return { text: error.status === 429 ? 'Search unavailable: too many requests right now.' : 'Search unavailable right now.', searched: true };
  }
}

// Collects what the executor would have sent, for inner turns.
function captureResponse() {
  const headers = {};
  return {
    statusCode: 200,
    headersSent: false,
    writableFinished: true,
    headers,
    body: null,
    set(name, value) { headers[String(name).toLowerCase()] = value; return this; },
    get(name) { return headers[String(name).toLowerCase()]; },
    status(code) { this.statusCode = code; return this; },
    json(body) { this.body = body; return this; },
    on() { return this; },
    write() { return true; },
    end() { return this; },
    flushHeaders() {},
  };
}

/**
 * Runs a chat with server-side web search. `execute` is the chat executor,
 * passed in to avoid a require cycle.
 */
async function executeWithSearch({ execute, req, res, key, format, spec, freeOnly, translator }) {
  const original = translator.toCanonical(spec.body);
  const clientTools = original.tools || [];
  const messages = [{ role: 'system', content: INSTRUCTION }, ...original.messages];
  let searches = 0;
  let final = null;
  let lastHeaders = {};
  for (let turn = 0; turn <= spec.maxUses; turn += 1) {
    const allowSearch = searches < spec.maxUses;
    const inner = captureResponse();
    await execute({
      req, res: inner, key, format: 'openai_chat', freeOnly, internal: true,
      canonical: { ...original, messages, stream: false, tools: allowSearch ? [...clientTools, WEB_TOOL] : clientTools.length ? clientTools : undefined },
    });
    lastHeaders = inner.headers;
    const message = inner.body?.choices?.[0]?.message || {};
    const calls = message.tool_calls || [];
    const searchCalls = calls.filter((call) => call.function?.name === TOOL_NAME);
    // An answer, or a call to the client's own tools: the next turn is the client's.
    if (!searchCalls.length || searchCalls.length !== calls.length) {
      final = searchCalls.length ? null : inner.body;
      break;
    }
    messages.push({ role: 'assistant', content: message.content || null, tool_calls: searchCalls });
    for (const call of searchCalls) {
      const output = searches < spec.maxUses
        ? await runSearch({ req, key, spec, freeOnly, call })
        : { text: 'Search limit reached: answer with the results you already have.', searched: false };
      if (output.searched) searches += 1;
      messages.push({ role: 'tool', tool_call_id: call.id, content: output.text });
    }
  }
  res.set('X-NeoRouter-Web-Searches', String(searches));
  // A plain chat answer is already final; other formats (and streams) get
  // one last turn in their own format, with the search results in context.
  if (final && format === 'openai_chat' && !original.stream) {
    for (const [name, value] of Object.entries(lastHeaders)) if (/^x-neorouter-/.test(name)) res.set(name, value);
    return res.json(final);
  }
  return execute({ req, res, key, format, freeOnly, internal: true, canonical: { ...original, messages, tools: clientTools.length ? clientTools : undefined } });
}

module.exports = { requested, available, executeWithSearch, formatResults, WEB_TOOL, TOOL_NAME };
