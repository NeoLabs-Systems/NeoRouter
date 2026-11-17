'use strict';

// Each adapter emits measured results only. New sources can be added without
// changing aggregation, comparisons, news selection or the dashboard.
const SOURCES = Object.freeze([
  {
    id: 'artificialAnalysis', name: 'Artificial Analysis', url: 'https://artificialanalysis.ai/',
    records: (snapshot) => (snapshot.artificialAnalysis || []).map((model) => ({
      id: model.slug, name: model.name, capability: 'chat', releaseDate: model.releaseDate,
      metrics: { ...model.benchmarks, ...(!model.indexDerived && Number.isFinite(model.intelligenceIndex) ? { intelligence_index: model.intelligenceIndex } : {}) },
    })),
  },
  {
    id: 'openrouterBenchmarks', name: 'OpenRouter', url: 'https://openrouter.ai/',
    records: (snapshot) => (snapshot.openrouterBenchmarks?.models || []).map((model) => ({
      id: model.slug, name: model.name, capability: 'chat', metrics: model.benchmarks,
    })),
  },
  {
    id: 'artificialAnalysisMedia', name: 'Artificial Analysis · Arena', url: 'https://artificialanalysis.ai/',
    records: (snapshot) => Object.entries(snapshot.artificialAnalysisMedia || {}).flatMap(([capability, models]) => models.map((model) => ({
      id: model.slug, name: model.name, capability, releaseDate: model.releaseDate, metrics: { arena_elo: model.elo },
    }))),
  },
  {
    id: 'mteb', name: 'MTEB', url: 'https://huggingface.co/spaces/mteb/leaderboard',
    records: (snapshot) => Object.entries(snapshot.mteb || {}).map(([id, model]) => ({
      id, name: model.model || id, capability: 'embedding', metrics: { mteb: model.score },
    })),
  },
]);

function measurements(snapshot) {
  return SOURCES.flatMap((source) => source.records(snapshot).filter((model) => model.id).map((model) => ({
    ...model, sourceId: source.id,
    metrics: Object.fromEntries(Object.entries(model.metrics || {}).filter(([, value]) => Number.isFinite(value))),
  }))).filter((model) => Object.keys(model.metrics).length);
}

module.exports = { SOURCES, measurements };
