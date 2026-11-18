'use strict';

const { getConfig } = require('../config/env');

/** This server's address as people reach it: the configured public URL, else the request's host. No trailing slash. */
function publicOrigin(req) {
  return (getConfig().publicUrl || `${req.protocol}://${req.get('host')}`).replace(/\/+$/, '');
}

module.exports = { publicOrigin };
