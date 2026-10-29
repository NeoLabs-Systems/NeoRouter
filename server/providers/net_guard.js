'use strict';

const dns = require('node:dns').promises;
const net = require('node:net');

// Local and private network ranges. Only the instance admin may point a
// provider at these (Ollama, LM Studio, a LAN box); everyone else is limited
// to public hosts so an account URL cannot reach the server's own network.
const blocked = new net.BlockList();
for (const [address, prefix] of [
  ['0.0.0.0', 8], ['10.0.0.0', 8], ['100.64.0.0', 10], ['127.0.0.0', 8], ['169.254.0.0', 16],
  ['172.16.0.0', 12], ['192.0.0.0', 24], ['192.168.0.0', 16], ['198.18.0.0', 15], ['224.0.0.0', 4], ['240.0.0.0', 4],
]) blocked.addSubnet(address, prefix, 'ipv4');
for (const [address, prefix] of [['::', 128], ['::1', 128], ['fc00::', 7], ['fe80::', 10], ['ff00::', 8]]) blocked.addSubnet(address, prefix, 'ipv6');

function isPrivateAddress(address) {
  const mapped = /^::ffff:(\d+\.\d+\.\d+\.\d+)$/i.exec(address);
  if (mapped) return blocked.check(mapped[1], 'ipv4');
  const family = net.isIP(address);
  if (!family) return true;
  return blocked.check(address, family === 4 ? 'ipv4' : 'ipv6');
}

// Resolves the host and returns true when any address it points at is local
// or private. A host that does not resolve is left to fail as a network error.
async function isPrivateDestination(url) {
  let host;
  try { host = new URL(url).hostname.replace(/^\[|\]$/g, ''); } catch { return true; }
  if (net.isIP(host)) return isPrivateAddress(host);
  let addresses;
  try { addresses = await dns.lookup(host, { all: true, verbatim: true }); } catch { return false; }
  return addresses.some(({ address }) => isPrivateAddress(address));
}

module.exports = { isPrivateAddress, isPrivateDestination };
