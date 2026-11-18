'use strict';

// The address an app should use, by where the app runs, and whether it can
// get through from there today. NeoRouter works this out so nobody has to
// know about loopback addresses or Docker networking.
const { getConfig } = require('../../config/env');
const network = require('./network');

const LOOPBACK = /^https?:\/\/(127\.0\.0\.1|localhost|\[::1\])(:\d+)?$/i;
// An address only reachable at home: an IP address, a .local or .lan name, or no dot at all.
const PRIVATE_HOST = /^(\d{1,3}(\.\d{1,3}){3}|\[[0-9a-f:]+\]|[^.]+|.+\.(local|lan|home|internal|localhost))$/i;

/**
 * The internet address of a NeoRouter run on its own domain, or null.
 * Set by NEOROUTER_PUBLIC_URL, or seen when the dashboard is opened over
 * https at a real domain (behind a reverse proxy).
 */
function hostedAddress(origin) {
  const { publicUrl } = getConfig();
  if (publicUrl) return publicUrl.replace(/\/+$/, '');
  try {
    const url = new URL(origin);
    if (url.protocol === 'https:' && !PRIVATE_HOST.test(url.hostname)) return url.origin;
  } catch { /* no usable origin */ }
  return null;
}

/**
 * [{ id, address, reachable, fix, note }] for local / docker / network / internet.
 * fix: what makes an unreachable place work ('open_network': let containers
 * and devices in; 'public_url': NeoRouter needs an internet address).
 * note: what the app side may also need ('docker_host': Linux containers
 * need host.docker.internal mapped to the host).
 * origin: the address this dashboard was opened at.
 */
function places(origin) {
  // On its own domain, apps reach it there; this server's loopback, Docker
  // and home network mean nothing to the people connecting their apps.
  const hosted = hostedAddress(origin);
  if (hosted) return [{ id: 'internet', address: `${hosted}/v1`, reachable: true, fix: null, note: null }];
  const { port, publicUrl } = getConfig();
  const status = network.status();
  const open = status.scope === 'network';
  const lan = network.lanAddress();
  // Opened from another device already: that address works for that device.
  const browserAddress = origin && !LOOPBACK.test(origin) && origin !== publicUrl ? origin : null;
  // Docker Desktop (macOS, Windows) passes host.docker.internal to this computer's own loopback.
  const desktopDocker = status.platform === 'darwin' || status.platform === 'win32';
  const v1 = (base) => `${base.replace(/\/+$/, '')}/v1`;
  return [
    { id: 'local', address: v1(`http://127.0.0.1:${port}`), reachable: true, fix: null, note: null },
    {
      id: 'docker',
      address: v1(`http://host.docker.internal:${port}`),
      reachable: open || desktopDocker,
      fix: open || desktopDocker ? null : 'open_network',
      note: status.platform === 'linux' && !status.container ? 'docker_host' : null,
    },
    {
      id: 'network',
      address: browserAddress ? v1(browserAddress) : lan ? v1(`http://${lan}:${port}`) : null,
      reachable: open,
      fix: open ? null : 'open_network',
      note: null,
    },
    { id: 'internet', address: publicUrl ? v1(publicUrl) : null, reachable: Boolean(publicUrl), fix: publicUrl ? null : 'public_url', note: null },
  ];
}

module.exports = { places, hostedAddress };
