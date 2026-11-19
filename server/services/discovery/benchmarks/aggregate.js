'use strict';

const { canonicalId } = require('../../catalog/model_identity');
const { measurements, SOURCES } = require('./sources');
const mean = (values) => values.reduce((sum, value) => sum + value, 0) / values.length;

// Midrank percentiles keep incompatible raw scales separate. Ties share a
// rank; one measured model has no peers and sits at the midpoint (50).
function percentiles(values) {
  const sorted = [...new Set(values)].sort((a, b) => a - b);
  const counts = new Map();
  for (const value of values) counts.set(value, (counts.get(value) || 0) + 1);
  let below = 0;
  return new Map(sorted.map((value) => {
    const ties = counts.get(value);
    const rank = values.length === 1 ? 50 : 100 * (below + (ties - 1) / 2) / (values.length - 1);
    below += ties;
    return [value, rank];
  }));
}

function aggregate(snapshot, identity = canonicalId) {
  const groups = new Map();
  for (const record of measurements(snapshot)) {
    const canonical = identity(record.id, record.capability);
    const key = `${record.capability}:${canonical}`;
    if (!groups.has(key)) groups.set(key, { id: key, canonicalId: canonical, name: record.name || record.id, capability: record.capability, releaseDate: record.releaseDate || null, observations: new Map() });
    const group = groups.get(key);
    for (const [metric, value] of Object.entries(record.metrics)) {
      const metricId = `${record.sourceId}:${metric}`;
      // Host aliases in one source must not multiply that source's vote.
      if (!group.observations.has(metricId)) group.observations.set(metricId, { id: metricId, sourceId: record.sourceId, metric, values: new Set() });
      group.observations.get(metricId).values.add(value);
    }
  }
  const peers = new Map();
  for (const group of groups.values()) {
    for (const observation of group.observations.values()) {
      observation.value = mean([...observation.values]);
      const key = `${group.capability}:${observation.id}`;
      if (!peers.has(key)) peers.set(key, []);
      peers.get(key).push(observation.value);
    }
  }
  const rankings = new Map([...peers].map(([key, values]) => [key, percentiles(values)]));
  const models = [...groups.values()].map(({ observations, ...group }) => {
    const bySource = new Map();
    const scores = [...observations.values()].map(({ values: _values, ...score }) => {
      const percentile = rankings.get(`${group.capability}:${score.id}`).get(score.value);
      if (!bySource.has(score.sourceId)) bySource.set(score.sourceId, []);
      bySource.get(score.sourceId).push(percentile);
      return { ...score, percentile, peers: peers.get(`${group.capability}:${score.id}`).length };
    });
    const sourceScores = [...bySource].map(([sourceId, values]) => ({ sourceId, score: mean(values) }));
    return { ...group, score: mean(sourceScores.map((source) => source.score)), sourceCount: sourceScores.length, metricCount: scores.length, sourceScores, scores };
  }).sort(compareRank);
  return {
    models,
    sources: SOURCES.map(({ id, name, url }) => ({ id, name, url, updatedAt: snapshot.fetchedAt?.[id] || null, modelCount: models.filter((model) => model.sourceScores.some((source) => source.sourceId === id)).length })),
  };
}

function compareRank(a, b) {
  return b.score - a.score || b.sourceCount - a.sourceCount || a.name.localeCompare(b.name) || a.id.localeCompare(b.id);
}

module.exports = { aggregate, percentiles, compareRank };
