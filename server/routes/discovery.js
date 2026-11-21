'use strict';
const router = require('express').Router();
const { z } = require('zod');
const { requirePermission } = require('../middleware/space');
const { HttpError } = require('../middleware/error_handler');
const news = require('../services/discovery/news_service');
const POLICY = require('../services/discovery/policy');
const query = z.object({
  query: z.string().max(200).default(''), capability: z.string().max(50).default('chat'),
  offset: z.coerce.number().int().min(0).max(1000000).default(0),
  limit: z.coerce.number().int().min(1).max(POLICY.maxPageSize).default(POLICY.pageSize),
  asOf: z.coerce.number().int().min(0).max(Number.MAX_SAFE_INTEGER).optional(),
});
router.use(requirePermission('providers.view'));
function options(req) {
  const result = query.safeParse(req.query);
  if (!result.success) throw new HttpError(400, 'INVALID_QUERY', 'Invalid discovery filters.');
  return result.data;
}
router.get('/news', (req, res) => res.json(news.list(options(req))));
module.exports = router;
