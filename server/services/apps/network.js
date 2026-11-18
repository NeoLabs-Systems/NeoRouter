'use strict';

// Who can reach NeoRouter's API: only this computer (the default, it listens
// on 127.0.0.1) or also containers and other devices (it listens on all
// addresses). An admin switches it from the dashboard; the change is written
// to ~/.neorouter/.env and, when a service manager runs NeoRouter (launchd or
// systemd restart it), NeoRouter restarts itself to apply it.
const os = require('node:os');
const { getConfig } = require('../../config/env');
const { ENV_FILE, SERVER_SERVICE_LABEL } = require('../../../runtime/paths');
const { upsertEnvValue } = require('../../../runtime/env');
const { detectContainer } = require('../../../runtime/container');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('network');
const LOCAL_HOSTS = new Set(['127.0.0.1', 'localhost', '::1']);
const RESTART_DELAY_MS = 800;

const listensBeyondLocal = () => detectContainer() || !LOCAL_HOSTS.has(String(getConfig().host));

// launchd and systemd bring NeoRouter back when it stops.
function canRestartItself() {
  if (detectContainer()) return false;
  return Boolean(process.env.INVOCATION_ID) || process.env.XPC_SERVICE_NAME === SERVER_SERVICE_LABEL;
}

const PRIVATE = /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/;

/** This computer's address in the local network, e.g. 192.168.1.20, or null. */
function lanAddress() {
  const found = [];
  for (const [name, addresses] of Object.entries(os.networkInterfaces())) {
    for (const item of addresses || []) {
      if (item.family === 'IPv4' && !item.internal && PRIVATE.test(item.address)) found.push({ name, address: item.address });
    }
  }
  // Real network cards before Docker's and VPNs' virtual ones.
  found.sort((a, b) => Number(/^(docker|br-|veth|utun|tun|vmnet|bridge)/.test(a.name)) - Number(/^(docker|br-|veth|utun|tun|vmnet|bridge)/.test(b.name)));
  return found[0]?.address || null;
}

function status() {
  return {
    scope: listensBeyondLocal() ? 'network' : 'local',
    port: getConfig().port,
    platform: process.platform,
    container: detectContainer(),
    canRestart: canRestartItself(),
  };
}

/**
 * Sets who can reach NeoRouter. { restarting } when it restarts itself;
 * otherwise { restartNeeded } and the person restarts it (neorouter restart).
 */
function setScope(scope) {
  if (detectContainer()) return { ...status(), restarting: false, restartNeeded: false };
  upsertEnvValue(ENV_FILE, 'NEOROUTER_HOST', scope === 'network' ? '0.0.0.0' : '127.0.0.1');
  logger.info('Network access changed', { scope });
  if (!canRestartItself()) return { ...status(), restarting: false, restartNeeded: true };
  // After the answer is sent: stop, and the service manager starts it again.
  setTimeout(() => process.kill(process.pid, 'SIGTERM'), RESTART_DELAY_MS).unref?.();
  return { ...status(), restarting: true, restartNeeded: false };
}

module.exports = { status, setScope, lanAddress, listensBeyondLocal };
