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

// The provider picks the content type, and media links open on the app's own
// origin, so only plain image, audio and video types are served as such.
// Anything else (HTML, SVG, scripts) is sent as a download.
const SAFE_TYPE = /^(image\/(png|jpeg|webp|gif|avif)|audio\/[a-z0-9.+-]+|video\/(mp4|webm|quicktime))$/;

function safeContentType(mime) {
  const type = String(mime || '').split(';')[0].trim().toLowerCase();
  return SAFE_TYPE.test(type) ? type : null;
}

function serveHeaders(res, mime) {
  const type = safeContentType(mime);
  res.set('Content-Type', type || 'application/octet-stream');
  if (!type) res.set('Content-Disposition', 'attachment');
  res.set('X-Content-Type-Options', 'nosniff');
  res.set('Content-Security-Policy', "default-src 'none'; sandbox");
}

function publicUrl(req, id) {
  const { getConfig } = require('../../config/env');
  const origin = getConfig().publicUrl || `${req.protocol}://${req.get('host')}`;
  return `${origin.replace(/\/+$/, '')}/v1/media/${id}`;
}

module.exports = { put, get, publicUrl, safeContentType, serveHeaders };
