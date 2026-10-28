'use strict';

const asyncRoute = (handler) => (req, res, next) => Promise.resolve(handler(req, res)).catch(next);
const context = (req) => ({ ipAddress: req.ip, userAgent: req.get('User-Agent') });

module.exports = { asyncRoute, context };
