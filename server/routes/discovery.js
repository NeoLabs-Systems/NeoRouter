'use strict';
const router = require('express').Router();
const { z } = require('zod');
const { requirePermission } = require('../middleware/space');
const { HttpError } = require('../middleware/error_handler');
const benchmarks = require('../services/discovery/benchmark_service');
const { compareRank } = require('../services/discovery/benchmarks/aggregate');
const news = require('../services/discovery/news_service');
const benchmarkEvidence = require('../services/benchmarks/evidence');
const benchmarkConsent = require('../services/benchmarks/consent');
const benchmarkPolicy = require('../services/benchmarks/policy');
const catalog = require('../services/catalog/catalog_service');
const POLICY = require('../services/discovery/policy');
const query = z.object({
  query: z.string().max(200).default(''), capability: z.string().max(50).default('chat'),
  source: z.string().max(80).default(''), free: z.enum(['0', '1']).default('0'),
  offset: z.coerce.number().int().min(0).max(1000000).default(0),
  limit: z.coerce.number().int().min(1).max(POLICY.maxPageSize).default(POLICY.pageSize),
  asOf: z.coerce.number().int().min(0).max(Number.MAX_SAFE_INTEGER).optional(),
  compare: z.string().max(1000).default(''),
  task: z.string().max(50).optional(),
});
router.use(requirePermission('providers.view'));
function options(req) {
  const result = query.safeParse(req.query);
  if (!result.success) throw new HttpError(400, 'INVALID_QUERY', 'Invalid discovery filters.');
  return { ...result.data, free: result.data.free === '1', compare: result.data.compare.split(',').filter(Boolean) };
}
router.get('/benchmarks', (req, res) => res.json(benchmarks.list(options(req))));
router.get('/internal-benchmarks', (req, res) => {
  const { limit, offset, task: requestedTask } = options(req);
  const rows = benchmarkEvidence.list();
  const enabled = benchmarkConsent.globalSettings().enabled;
  const minSamples = benchmarkPolicy.minSamples;
  if (req.query.groupBy === 'model') {
    const byModel = new Map();
    for (const row of rows) {
      if (!byModel.has(row.model)) byModel.set(row.model, []);
      byModel.get(row.model).push(row);
    }
    const listings = new Map();
    if (rows.length) {
      for (const entry of catalog.listForUser(req.space.ownerId, { includeUnavailable: true })) {
        const previous = listings.get(entry.canonicalId);
        if (!previous || (previous.name === previous.modelId && entry.name !== entry.modelId)) listings.set(entry.canonicalId, entry);
      }
    }
    let page;
    let total = byModel.size;
    let ranking = {};
    if (requestedTask !== undefined) {
      const coverage = new Map();
      for (const { task } of rows) coverage.set(task, (coverage.get(task) || 0) + 1);
      const tasks = [...coverage.keys()].sort((a, b) => coverage.get(b) - coverage.get(a) || a.localeCompare(b));
      const task = coverage.has(requestedTask) ? requestedTask : tasks[0] ?? null;
      const ranked = rows.filter((row) => row.task === task).map((row) => ({
        row, id: row.model, score: row.score, name: listings.get(row.model)?.name || row.model, sourceCount: row.evaluators.length,
      })).sort(compareRank);
      page = ranked.slice(offset, offset + limit).map(({ row }) => row);
      total = ranked.length;
      ranking = { task, tasks };
    } else {
      page = [...byModel.values()].slice(offset, offset + limit).flat();
    }
    const results = page.map((row) => ({ ...row, name: listings.get(row.model)?.name,
      evaluators: row.evaluators.map(({ model, method, version, samples, score }) => ({
        method,
        name: method === 'judge' ? listings.get(model)?.name : undefined,
        version, samples, score,
      })),
    }));
    return res.json({ enabled, minSamples, total, limit, offset, ...ranking, results });
  }
  res.json({ enabled, minSamples,
    total: rows.length, limit, offset, results: rows.slice(offset, offset + limit) });
});
router.get('/news', (req, res) => res.json(news.list(options(req))));
module.exports = router;
