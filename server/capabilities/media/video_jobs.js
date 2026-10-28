'use strict';

// Video generation is asynchronous: the provider that accepted a job is the
// only one that knows it, so a job is bound to that account for its whole
// life. NeoRouter hands out its own ids (video_…) and polls the provider when
// the client asks for the status, OpenAI Videos API style.
const crypto = require('node:crypto');
const { getDatabase } = require('../../db/database');
const { HttpError } = require('../../middleware/error_handler');
const accounts = require('../../services/providers/account_service');

const TERMINAL = new Set(['completed', 'failed']);
// Polling the provider more often than this only spends its rate limit.
const MIN_POLL_MS = 3000;

function create({ key, requestId, entry, upstreamId, status, progress, request }) {
  const now = Date.now();
  const job = {
    id: `video_${crypto.randomBytes(12).toString('hex')}`,
    user_id: key.user_id,
    router_key_id: key.id,
    request_id: requestId || null,
    capability: 'video',
    model_ref: entry.ref,
    model_label: entry.id,
    account_id: entry.accountId,
    upstream_id: upstreamId,
    status: status || 'queued',
    progress: progress ?? null,
    request_json: JSON.stringify(request || {}),
    result_json: null,
    error_message: null,
    created_at: now,
    updated_at: now,
  };
  getDatabase().prepare(`INSERT INTO media_jobs (id, user_id, router_key_id, request_id, capability, model_ref, model_label, account_id, upstream_id,
    status, progress, request_json, result_json, error_message, created_at, updated_at) VALUES (@id, @user_id, @router_key_id, @request_id,
    @capability, @model_ref, @model_label, @account_id, @upstream_id, @status, @progress, @request_json, @result_json, @error_message,
    @created_at, @updated_at)`).run(job);
  return job;
}

function find(userId, id) {
  const job = getDatabase().prepare('SELECT * FROM media_jobs WHERE id = ? AND user_id = ?').get(id, userId);
  if (!job) throw new HttpError(404, 'VIDEO_NOT_FOUND', `No video job ${id}.`);
  return job;
}

function list(userId, { limit = 20 } = {}) {
  return getDatabase().prepare('SELECT * FROM media_jobs WHERE user_id = ? ORDER BY created_at DESC LIMIT ?').all(userId, Math.min(100, limit));
}

function adapterOf(job) {
  const runtime = accounts.getRuntime(job.account_id);
  if (!runtime) throw new HttpError(410, 'VIDEO_PROVIDER_GONE', 'The provider account that made this video was removed.');
  return accounts.adapterFor(runtime);
}

async function refresh(job) {
  if (TERMINAL.has(job.status) || Date.now() - job.updated_at < MIN_POLL_MS) return job;
  try {
    const state = await adapterOf(job).getVideo(job.upstream_id, { signal: AbortSignal.timeout(30_000) });
    const next = { ...job, status: state.status, progress: state.progress ?? job.progress, error_message: state.error || null, updated_at: Date.now() };
    getDatabase().prepare('UPDATE media_jobs SET status = ?, progress = ?, error_message = ?, updated_at = ? WHERE id = ?')
      .run(next.status, next.progress, next.error_message, next.updated_at, job.id);
    return next;
  } catch (error) {
    if (error instanceof HttpError) throw error;
    // A failed poll is not a failed job; report the last known state.
    return job;
  }
}

async function content(job, signal) {
  if (job.status !== 'completed') throw new HttpError(409, 'VIDEO_NOT_READY', `The video is ${job.status}.`);
  return adapterOf(job).videoContent(job.upstream_id, { signal });
}

function toObject(job) {
  const request = JSON.parse(job.request_json || '{}');
  return {
    id: job.id,
    object: 'video',
    model: job.model_label,
    status: job.status,
    progress: job.progress ?? (job.status === 'completed' ? 100 : 0),
    created_at: Math.floor(job.created_at / 1000),
    seconds: request.seconds ? String(request.seconds) : null,
    size: request.size || null,
    prompt: request.prompt || null,
    error: job.error_message ? { message: job.error_message } : null,
  };
}

module.exports = { create, find, list, refresh, content, toObject };
