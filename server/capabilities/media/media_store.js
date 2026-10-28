'use strict';

// Short-lived storage for generated media, so a client that asks for a URL
// gets one even when the provider returned raw bytes. Ids are random 32-byte
// tokens (the URL is the capability), entries expire after an hour and the
// store is capped in size. Nothing is written to disk.
const crypto = require('node:crypto');

const TTL_MS = 60 * 60 * 1000;
const MAX_BYTES = 256 * 1024 * 1024;
const items = new Map();
let totalBytes = 0;

function sweep(now = Date.now()) {
  for (const [id, item] of items) {
    if (item.expiresAt > now && totalBytes <= MAX_BYTES) break;
    items.delete(id);
    totalBytes -= item.data.length;
  }
}

function put(data, mime) {
  sweep();
  const id = crypto.randomBytes(32).toString('base64url');
  items.set(id, { data, mime, expiresAt: Date.now() + TTL_MS });
  totalBytes += data.length;
  sweep();
  return id;
}

function get(id) {
  const item = items.get(id);
  if (!item || item.expiresAt <= Date.now()) return null;
  return item;
}

function publicUrl(req, id) {
  const { getConfig } = require('../../config/env');
  const origin = getConfig().publicUrl || `${req.protocol}://${req.get('host')}`;
  return `${origin.replace(/\/+$/, '')}/v1/media/${id}`;
}

module.exports = { put, get, publicUrl };
