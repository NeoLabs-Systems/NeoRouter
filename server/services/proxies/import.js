'use strict';
const crypto = require('node:crypto');

// Accept common proxy-list formats without retaining failed lines or credentials.
function parse(line, protocol = 'socks5') {
  let input = line.trim();
  if (!input.includes('://') && !input.includes('@')) {
    const parts = input.includes(',') || input.includes('\t') ? input.split(/[,\t]/).map((part) => part.trim()) : input.split(':');
    if (parts.length === 2) input = `${parts[0]}:${parts[1]}`;
    if (parts.length === 4) input = `${encodeURIComponent(parts[2])}:${encodeURIComponent(parts[3])}@${parts[0]}:${parts[1]}`;
  }
  const url = new URL(input.includes('://') ? input : `${protocol}://${input}`);
  const supported = ['socks5:', 'socks5h:', 'http:', 'https:'].includes(url.protocol);
  const invalidPort = (url.protocol.startsWith('socks') && !url.port) || (url.port && Number(url.port) < 1) || Number(url.port) > 65535;
  if (!supported || !url.hostname || invalidPort || (url.pathname && url.pathname !== '/') || url.search || url.hash) throw new Error('Invalid proxy');
  decodeURIComponent(url.username);
  decodeURIComponent(url.password);
  url.pathname = '';
  return { url: url.toString(), endpoint: `${url.protocol}//${url.host}`, fingerprint: crypto.createHash('sha256').update(url.toString()).digest('hex') };
}

module.exports = { parse };
