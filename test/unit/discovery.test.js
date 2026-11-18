'use strict';
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { aggregate, percentiles } = require('../../server/services/discovery/benchmarks/aggregate');
const metadata = require('../../server/services/catalog/metadata_service');
const benchmarks = require('../../server/services/discovery/benchmark_service');
const news = require('../../server/services/discovery/news_service');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');

migrate();
const date = '2026-11-17T10:00:00Z';
const models = [
  { slug: 'test-small', name: 'Test Small', intelligenceIndex: 10, benchmarks: { gpqa: 0.2, mmlu_pro: 0.3 } },
  { slug: 'test-large', name: 'Test Large', intelligenceIndex: 30, benchmarks: { gpqa: 0.8, mmlu_pro: 0.9 } },
];

test('benchmark aggregation normalizes scales, gives each source one vote and excludes missing/derived values', () => {
  const result = aggregate({ artificialAnalysis: models,
    openrouterBenchmarks: { models: [
      { slug: 'vendor/test-small', benchmarks: { gpqa: 90 } },
      { slug: 'vendor/test-large', benchmarks: { gpqa: 10 } },
    ] },
    mteb: { embed: { model: 'org/embed', score: 0.7 } },
    artificialAnalysisMedia: { image: [{ slug: 'image-test', elo: 1200 }] }, fetchedAt: {},
  });
  const small = result.models.find((model) => model.id === 'chat:testsmall');
  const large = result.models.find((model) => model.id === 'chat:testlarge');
  assert.equal(small.score, 50, 'three AA tests must not outweigh one OpenRouter test');
  assert.equal(large.score, 50);
  assert.equal(small.sourceCount, 2);
  assert.equal(small.metricCount, 4);
  assert.equal(result.models.find((model) => model.capability === 'embedding').score, 50, 'embedding ranks separately');
  assert.equal(percentiles([1, 1, 1]).get(1), 50, 'ties have the same midpoint rank');
  const derived = aggregate({ artificialAnalysis: [{ slug: 'derived', indexDerived: true, intelligenceIndex: 40, benchmarks: { gpqa: 0, hle: null } }] });
  assert.deepEqual(derived.models[0].scores.map((score) => score.metric), ['gpqa']);
  assert.equal(derived.models[0].scores[0].value, 0, 'measured zero is not missing');
});

test('news observes only benchmarked, confirmed-free strong models and persists deduplicated detections', () => {
  const seed = { artificialAnalysis: models.map((model) => ({ ...model, benchmarks: { ...model.benchmarks } })),
    openrouter: [
      { id: 'vendor/test-small', isFree: true, priceIn: 0, priceOut: 0 },
      { id: 'vendor/test-large', isFree: true, priceIn: 0, priceOut: 0, contextWindow: 32000 },
      { id: 'other/test-large:free', isFree: true, priceIn: 0, priceOut: 0 },
      { id: 'unmeasured', isFree: true, priceIn: 0, priceOut: 0 },
      { id: 'unknown-price', priceIn: null, priceOut: null },
    ], fetchedAt: { openrouter: date, artificialAnalysis: date },
  };
  metadata.resetForTests(seed);
  news.observe(1000);
  assert.equal(news.list().total, 1, 'only the stronger half, with evidence and confirmed prices');
  const first = news.list().items[0];
  assert.equal(first.name, 'Test Large');
  assert.equal(first.detectedAt, 1000);
  assert.equal(first.available, true);
  news.observe(2000);
  assert.equal(news.list().total, 1);
  assert.equal(news.list().items[0].detectedAt, 1000);
  metadata.resetForTests({ ...seed, openrouter: [], fetchedAt: { openrouter: '2026-11-17T11:00:00Z' } });
  news.observe(3000);
  assert.equal(news.list().items[0].available, false, 'retired entries remain historical');
  assert.equal(news.list().items[0].lastSeenAt, Date.parse(date));
  metadata.resetForTests(seed);
  news.observe(4000);
  assert.equal(news.list().items[0].available, true);
  assert.equal(news.list().items[0].detectedAt, 1000, 'reappearing models are not reposted');
  assert.equal(news.list({ query: '%' }).total, 0, 'search uses literal characters');
  assert.equal(benchmarks.list({ query: 'large', free: true }).total, 1);
  assert.equal(benchmarks.list({ compare: ['chat:testsmall', 'chat:testlarge'], limit: 1 }).comparison.length, 2);
  assert.equal(getDatabase().prepare('SELECT COUNT(*) n FROM discovery_news').get().n, 1);
  const firstPage = news.list({ asOf: 1000, limit: 1 });
  metadata.resetForTests({ ...seed,
    artificialAnalysis: [...seed.artificialAnalysis, { slug: 'test-new', name: 'New model', intelligenceIndex: 50, benchmarks: { gpqa: 1 } }],
    openrouter: [...seed.openrouter, { id: 'test-new', isFree: true }],
  });
  news.observe(5000);
  assert.equal(news.list().total, 2);
  assert.equal(news.list({ asOf: firstPage.asOf }).total, 1, 'new detections cannot shift an in-progress timeline');
});

test('public free detection rejects unknown prices and any additional charge', async () => {
  const original = global.fetch;
  global.fetch = async () => new Response(JSON.stringify({ data: [
    { id: 'free', pricing: { prompt: '0', completion: '0', request: '0' } },
    { id: 'unknown', pricing: { prompt: null, completion: '0' } },
    { id: 'empty', pricing: { prompt: '', completion: '' } },
    { id: 'fee', pricing: { prompt: '0', completion: '0', request: '0.01' } },
  ] }));
  try {
    const listings = await require('../../server/services/catalog/metadata/openrouter_public').fetchSource();
    assert.deepEqual(listings.map((model) => model.isFree), [true, false, false, false]);
  } finally { global.fetch = original; }
});
