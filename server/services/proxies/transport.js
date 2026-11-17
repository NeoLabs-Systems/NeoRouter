'use strict';

const tls = require('node:tls');
const { isIP } = require('node:net');
const { Agent, ProxyAgent, Pool } = require('undici');
const { SocksClient } = require('socks');
const { decryptString } = require('../../utils/crypto');
const { POLICY } = require('./policy');

function connectionFailed() {
  const error = new Error('Proxy connection failed');
  error.code = 'PROXY_CONNECT_FAILED';
  return error;
}

function createDispatcher(row, onLatency) {
  const proxy = new URL(decryptString(row.secret_encrypted));
  if (proxy.protocol.startsWith('http')) {
    const auth = proxy.username
      ? `Basic ${Buffer.from(`${decodeURIComponent(proxy.username)}:${decodeURIComponent(proxy.password)}`).toString('base64')}`
      : undefined;
    proxy.username = '';
    proxy.password = '';
    return new ProxyAgent({
      uri: proxy.toString(), token: auth, connections: POLICY.connections,
      proxyTls: { timeout: POLICY.connectMs }, requestTls: { timeout: POLICY.connectMs },
      clientFactory: (origin, options) => new Pool(origin, {
        ...options, connections: POLICY.connections, headersTimeout: POLICY.connectMs,
      }),
      factory: (origin, options) => new Pool(origin, {
        ...options,
        connect(connectOptions, callback) {
          const began = Date.now();
          let completed = false;
          options.connect(connectOptions, (error, socket) => {
            if (completed) return;
            completed = true;
            // Tunnel failures precede the upstream request, so retrying is safe.
            if (error) return callback(connectionFailed(), null);
            onLatency(Date.now() - began);
            callback(null, socket);
          });
        },
      }),
    });
  }

  return new Agent({
    connections: POLICY.connections,
    connect(options, callback) {
      const began = Date.now();
      const host = options.hostname.replace(/^\[|\]$/g, '');
      SocksClient.createConnection({
        command: 'connect', timeout: POLICY.connectMs,
        proxy: {
          host: proxy.hostname.replace(/^\[|\]$/g, ''), port: Number(proxy.port), type: 5,
          userId: decodeURIComponent(proxy.username), password: decodeURIComponent(proxy.password),
        },
        destination: { host, port: Number(options.port || (options.protocol === 'https:' ? 443 : 80)) },
      }).then(({ socket }) => {
        onLatency(Date.now() - began);
        if (options.protocol !== 'https:') return callback(null, socket);
        // Keep normal certificate and hostname verification, also for IP URLs.
        const secure = tls.connect({ socket, host, servername: isIP(host) ? undefined : host });
        const timer = setTimeout(() => secure.destroy(new Error('Proxy TLS timeout')), POLICY.connectMs);
        const failed = () => {
          clearTimeout(timer);
          callback(connectionFailed(), null);
        };
        secure.once('error', failed);
        secure.once('secureConnect', () => {
          clearTimeout(timer);
          secure.removeListener('error', failed);
          callback(null, secure);
        });
      }).catch(() => callback(connectionFailed(), null));
    },
  });
}

module.exports = { createDispatcher };
