#!/usr/bin/env node
'use strict';

require('../runtime/env').loadEnvironment();

const { getConfig } = require('./config/env');
const { createApp } = require('./app');
const scheduler = require('./workers/scheduler');
const { createLogger } = require('./utils/logger');
const { masterKey } = require('./utils/crypto');

const logger = createLogger('server');

process.on('uncaughtException', (err) => {
  logger.error('Uncaught exception', { error: err && err.stack ? err.stack : String(err) });
  process.exit(1);
});
process.on('unhandledRejection', (reason) => {
  logger.error('Unhandled rejection', { reason: reason && reason.stack ? reason.stack : String(reason) });
});

// Provider keys are encrypted with this key; refuse to start without it.
masterKey();
const config = getConfig();
const app = createApp();
const server = app.listen(config.port, config.host, () => {
  logger.info('NeoRouter listening', { host: config.host, port: config.port });
});
server.requestTimeout = 0;
server.headersTimeout = 65_000;
scheduler.start();
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => {
    scheduler.stop();
    server.close(() => process.exit(0));
  });
}
