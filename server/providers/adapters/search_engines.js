'use strict';

// Web search engines behind one method: search({ query, maxResults,
// includeDomains, excludeDomains, recency }) → { results: [{ title, url,
// snippet, content, published }] }. Every engine speaks its own dialect;
// the definition names it (`search.engine`).
const { BaseAdapter } = require('./base_adapter');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS } = require('../provider_error');

const MAX_CONTENT = 4000;
const RECENCY_DAYS = { day: 1, week: 7, month: 31, year: 366 };

const clip = (text, max = MAX_CONTENT) => {
  const value = String(text || '').replace(/\s+/g, ' ').trim();
  return value.length > max ? `${value.slice(0, max)}…` : value;
};

function sinceDate(recency) {
  const days = RECENCY_DAYS[recency];
  return days ? new Date(Date.now() - days * 86_400_000).toISOString().slice(0, 10) : null;
}

function hostOf(url) {
  try { return new URL(url).hostname.replace(/^www\./, ''); } catch { return ''; }
}

const onDomain = (url, domains) => domains.some((domain) => {
  const host = hostOf(url);
  const wanted = String(domain).replace(/^www\./, '').toLowerCase();
  return host === wanted || host.endsWith(`.${wanted}`);
});

// For engines without domain filters: site: operators plus a local filter.
function withSiteOperators(query, includeDomains = [], excludeDomains = []) {
  const include = includeDomains.slice(0, 5).map((domain) => `site:${domain}`).join(' OR ');
  const exclude = excludeDomains.slice(0, 10).map((domain) => `-site:${domain}`).join(' ');
  return [query, include && `(${include})`, exclude].filter(Boolean).join(' ');
}

const ENGINES = {
  async tavily(adapter, { query, maxResults, includeDomains, excludeDomains, recency }, ctx) {
    const body = { query, max_results: maxResults, search_depth: 'basic', include_raw_content: false };
    if (includeDomains.length) body.include_domains = includeDomains;
    if (excludeDomains.length) body.exclude_domains = excludeDomains;
    if (recency) body.time_range = recency;
    const { data, headers } = await adapter.http('/search', { method: 'POST', body, ...ctx });
    return { headers, results: (data?.results || []).map((item) => ({ title: item.title, url: item.url, snippet: clip(item.content, 400), content: clip(item.content), published: item.published_date || null })) };
  },

  async exa(adapter, { query, maxResults, includeDomains, excludeDomains, recency }, ctx) {
    const body = { query, numResults: maxResults, type: 'auto', contents: { text: { maxCharacters: MAX_CONTENT } } };
    if (includeDomains.length) body.includeDomains = includeDomains;
    if (excludeDomains.length) body.excludeDomains = excludeDomains;
    if (recency) body.startPublishedDate = `${sinceDate(recency)}T00:00:00.000Z`;
    const { data, headers } = await adapter.http('/search', { method: 'POST', body, ...ctx });
    return { headers, results: (data?.results || []).map((item) => ({ title: item.title, url: item.url, snippet: clip(item.text, 400), content: clip(item.text), published: item.publishedDate || null })) };
  },

  async linkup(adapter, { query, includeDomains, excludeDomains, recency }, ctx) {
    const body = { q: query, depth: 'standard', outputType: 'searchResults' };
    if (includeDomains.length) body.includeDomains = includeDomains;
    if (excludeDomains.length) body.excludeDomains = excludeDomains;
    if (recency) body.fromDate = sinceDate(recency);
    const { data, headers } = await adapter.http('/search', { method: 'POST', body, ...ctx });
    return { headers, results: (data?.results || []).filter((item) => item.type !== 'image').map((item) => ({ title: item.name, url: item.url, snippet: clip(item.content, 400), content: clip(item.content), published: null })) };
  },

  async parallel(adapter, { query, maxResults, includeDomains, excludeDomains, recency }, ctx) {
    const sourcePolicy = {};
    if (includeDomains.length) sourcePolicy.include_domains = includeDomains;
    if (excludeDomains.length) sourcePolicy.exclude_domains = excludeDomains;
    if (recency) sourcePolicy.after_date = sinceDate(recency);
    const body = { objective: query, search_queries: [query], mode: 'fast', advanced_settings: { max_results: maxResults, ...(Object.keys(sourcePolicy).length ? { source_policy: sourcePolicy } : {}) } };
    const { data, headers } = await adapter.http('/search', { method: 'POST', body, ...ctx });
    return { headers, results: (data?.results || []).map((item) => { const text = (item.excerpts || []).join('\n'); return { title: item.title, url: item.url, snippet: clip(text, 400), content: clip(text), published: item.publish_date || null }; }) };
  },

  async firecrawl(adapter, { query, maxResults, includeDomains, excludeDomains, recency }, ctx) {
    const body = { query, limit: maxResults, sources: [{ type: 'web' }] };
    // Firecrawl takes either list, not both.
    if (includeDomains.length) body.includeDomains = includeDomains;
    else if (excludeDomains.length) body.excludeDomains = excludeDomains;
    if (recency) body.tbs = `qdr:${recency.charAt(0)}`;
    const { data, headers } = await adapter.http('/search', { method: 'POST', body, ...ctx });
    return { headers, results: (data?.data?.web || []).map((item) => ({ title: item.title, url: item.url, snippet: clip(item.description, 400), content: clip(item.markdown || item.description), published: null })) };
  },

  async serper(adapter, { query, maxResults, includeDomains, excludeDomains, recency }, ctx) {
    const body = { q: withSiteOperators(query, includeDomains, excludeDomains), num: maxResults };
    if (recency) body.tbs = `qdr:${recency.charAt(0)}`;
    const { data, headers } = await adapter.http('/search', { method: 'POST', body, ...ctx });
    return { headers, results: (data?.organic || []).map((item) => ({ title: item.title, url: item.link, snippet: clip(item.snippet, 400), content: clip(item.snippet), published: item.date || null })) };
  },

  async brave(adapter, { query, maxResults, includeDomains, excludeDomains, recency }, ctx) {
    const params = new URLSearchParams({ q: withSiteOperators(query, includeDomains, excludeDomains), count: String(Math.min(20, maxResults)), extra_snippets: 'true' });
    if (recency) params.set('freshness', { day: 'pd', week: 'pw', month: 'pm', year: 'py' }[recency]);
    const { data, headers } = await adapter.http(`/web/search?${params}`, { headers: { Accept: 'application/json' }, ...ctx });
    return { headers, results: (data?.web?.results || []).map((item) => ({ title: item.title, url: item.url, snippet: clip(item.description, 400), content: clip([item.description, ...(item.extra_snippets || [])].join(' ')), published: item.page_age || null })) };
  },

  async jina(adapter, { query, includeDomains, excludeDomains }, ctx) {
    const headers = { Accept: 'application/json', 'X-Respond-With': 'no-content' };
    if (includeDomains.length === 1) headers['X-Site'] = includeDomains[0];
    const { data, headers: responseHeaders } = await adapter.http(`/?q=${encodeURIComponent(withSiteOperators(query, includeDomains.length === 1 ? [] : includeDomains, excludeDomains))}`, { headers, ...ctx });
    return { headers: responseHeaders, results: (data?.data || []).map((item) => ({ title: item.title, url: item.url, snippet: clip(item.description || item.content, 400), content: clip(item.content || item.description), published: item.date || item.publishedTime || null })) };
  },

  async searxng(adapter, { query, includeDomains, excludeDomains, recency }, ctx) {
    const params = new URLSearchParams({ q: withSiteOperators(query, includeDomains, excludeDomains), format: 'json' });
    if (recency) params.set('time_range', recency);
    const { data, headers } = await adapter.http(`/search?${params}`, { headers: { Accept: 'application/json' }, ...ctx });
    if (typeof data === 'string') throw new ProviderError(KINDS.BAD_REQUEST, 'SearXNG answered with HTML: enable the json format in its settings.yml (search.formats).');
    return { headers, results: (data?.results || []).map((item) => ({ title: item.title, url: item.url, snippet: clip(item.content, 400), content: clip(item.content), published: item.publishedDate || null })) };
  },
};

class SearchAdapter extends BaseAdapter {
  get engine() {
    return this.definition.search?.engine;
  }

  supports(method) {
    return method === 'search';
  }

  async listModels() {
    return (this.definition.models?.static || []).map((entry) => normalizeModel(entry));
  }

  // A real one-result search: the only reliable check, at the cost of one credit.
  async validate() {
    try {
      await this.search({ query: 'NeoRouter', maxResults: 1 }, { signal: AbortSignal.timeout(20_000), timeoutMs: 20_000 });
      return { status: 'healthy', message: null };
    } catch (error) {
      if (error.kind === KINDS.AUTH || error.kind === KINDS.PAYMENT) return { status: 'invalid', message: error.message };
      return { status: 'unknown', message: error.message };
    }
  }

  async search({ query, maxResults = 5, includeDomains = [], excludeDomains = [], recency = null }, { signal, timeoutMs } = {}) {
    const run = ENGINES[this.engine];
    if (!run) throw new ProviderError(KINDS.BAD_REQUEST, `Unknown search engine ${this.engine}.`);
    // Ask for a few more than needed: domain filtering below may drop some.
    const wanted = Math.min(20, Math.max(1, maxResults));
    const { results, headers } = await run(this, { query, maxResults: Math.min(20, wanted + (includeDomains.length || excludeDomains.length ? 5 : 0)), includeDomains, excludeDomains, recency }, { signal, timeoutMs });
    const seen = new Set();
    const filtered = results
      .filter((item) => item.url && /^https?:\/\//.test(item.url))
      .filter((item) => !includeDomains.length || onDomain(item.url, includeDomains))
      .filter((item) => !excludeDomains.length || !onDomain(item.url, excludeDomains))
      .filter((item) => !seen.has(item.url) && seen.add(item.url))
      .slice(0, wanted)
      .map((item) => ({ ...item, title: clip(item.title || hostOf(item.url), 300) }));
    return { results: filtered, headers };
  }
}

module.exports = { SearchAdapter, withSiteOperators, onDomain, ENGINES };
