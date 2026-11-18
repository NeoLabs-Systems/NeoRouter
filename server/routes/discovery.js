'use strict';
const router = require('express').Router();
const { z } = require('zod');
const { requirePermission } = require('../middleware/space');
const { HttpError } = require('../middleware/error_handler');
const benchmarks = require('../services/discovery/benchmark_service');
const news = require('../services/discovery/news_service');
const benchmarkEvidence = require('../services/benchmarks/evidence');
const benchmarkConsent = require('../services/benchmarks/consent');
const benchmarkPolicy = require('../services/benchmarks/policy');
const POLICY = require('../services/discovery/policy');
const query = z.object({
  query: z.string().max(200).default(''), capability: z.string().max(50).default('chat'),
  source: z.string().max(80).default(''), free: z.enum(['0', '1']).default('0'),
  offset: z.coerce.number().int().min(0).max(1000000).default(0),
  limit: z.coerce.number().int().min(1).max(POLICY.maxPageSize).default(POLICY.pageSize),
  asOf: z.coerce.number().int().min(0).max(Number.MAX_SAFE_INTEGER).optional(),
  compare: z.string().max(1000).default(''),
});
router.use(requirePermission('providers.view'));
function options(req) {
  const result = query.safeParse(req.query);
  if (!result.success) throw new HttpError(400, 'INVALID_QUERY', 'Invalid discovery filters.');
  return { ...result.data, free: result.data.free === '1', compare: result.data.compare.split(',').filter(Boolean) };
}
router.get('/benchmarks', (req, res) => res.json(benchmarks.list(options(req))));
router.get('/internal-benchmarks', (req, res) => {
  const { limit, offset } = options(req);
  const rows = benchmarkEvidence.list();
  res.json({ enabled: benchmarkConsent.globalSettings().enabled, minSamples: benchmarkPolicy.minSamples,
    total: rows.length, limit, offset, results: rows.slice(offset, offset + limit) });
});
router.get('/news', (req, res) => res.json(news.list(options(req))));
module.exports = router;
