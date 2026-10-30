'use strict';

// OpenAI's open privacy filter (openai/privacy-filter, Apache 2.0): a small
// token classifier that finds names, addresses, account numbers and other
// private values. It runs on this server, so no provider ever sees the raw
// text. An admin turns it on once; the weights download in the background,
// load on first use and unload again after a while without requests.
const fs = require('node:fs');
const path = require('node:path');
const settings = require('../settings/settings_service');
const { DATA_DIR } = require('../../../runtime/paths');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('privacy-model');

const MODEL = 'openai/privacy-filter';
const DTYPE = 'q4';
const SETTING = 'privacy_model_enabled';
const IDLE_MS = 15 * 60_000;
const MAX_CHARS = 20_000;
const MIN_SCORE = 0.5;
// The model's labels → the shield's categories.
const CATEGORY_OF = {
  private_person: 'person',
  private_address: 'address',
  private_email: 'email',
  private_phone: 'phone',
  account_number: 'id',
  private_date: 'birthdate',
  secret: 'secret',
};

const state = { status: 'off', progress: null, error: null };
let loading = null;
let pipe = null;
let idleTimer = null;
// Swapped out by tests; the real one is loaded only when needed.
let loadLibrary = () => import('@huggingface/transformers');

const cacheDir = () => path.join(DATA_DIR, 'models');

function enabled() {
  return settings.getGlobal(SETTING, false) === true;
}

function status() {
  return { enabled: enabled(), model: MODEL, ...state };
}

async function library() {
  const transformers = await loadLibrary();
  transformers.env.cacheDir = cacheDir();
  transformers.env.allowLocalModels = false;
  return transformers;
}

// Downloads (first time) and loads the model; one load at a time.
function load() {
  if (pipe) return Promise.resolve(pipe);
  if (loading) return loading;
  fs.mkdirSync(cacheDir(), { recursive: true });
  state.status = 'loading';
  state.error = null;
  const files = new Map();
  loading = library()
    .then(({ pipeline }) => pipeline('token-classification', MODEL, {
      dtype: DTYPE,
      device: 'cpu',
      progress_callback: (event) => {
        if (event.status !== 'progress' || !event.total) return;
        files.set(event.file, { loaded: event.loaded, total: event.total });
        const all = [...files.values()];
        state.status = 'downloading';
        state.progress = all.reduce((sum, file) => sum + file.loaded, 0) / all.reduce((sum, file) => sum + file.total, 0);
      },
    }))
    .then((loaded) => {
      pipe = loaded;
      state.status = 'ready';
      state.progress = null;
      logger.info('Privacy filter loaded');
      touch();
      return pipe;
    })
    .catch((error) => {
      state.status = 'error';
      state.error = error.message;
      logger.warn('Privacy filter could not load', { error: error.message });
      throw error;
    })
    .finally(() => { loading = null; });
  return loading;
}

// Frees the memory once nobody has used the model for a while.
function touch() {
  clearTimeout(idleTimer);
  idleTimer = setTimeout(() => {
    const loaded = pipe;
    pipe = null;
    state.status = enabled() ? 'idle' : 'off';
    loaded?.dispose?.().catch(() => {});
    logger.info('Privacy filter unloaded after idling');
  }, IDLE_MS);
  idleTimer.unref?.();
}

// Turned on: download right away so the first request does not wait for it,
// then let it idle until it is needed.
function setEnabled(value) {
  settings.setGlobal(SETTING, value === true);
  if (value === true) {
    if (!pipe) load().catch(() => {});
  } else {
    clearTimeout(idleTimer);
    const loaded = pipe;
    pipe = null;
    loaded?.dispose?.().catch(() => {});
    state.status = 'off';
  }
  return status();
}

// At start-up: finish a download that was cut short; otherwise load lazily.
function resume() {
  if (!enabled()) return;
  state.status = 'idle';
  const downloaded = fs.existsSync(path.join(cacheDir(), ...MODEL.split('/'), 'onnx', `model_${DTYPE}.onnx_data`));
  if (!downloaded) load().catch(() => {});
}

/** Private values in `text`, as { text, category } (exact substrings). */
async function detect(text) {
  if (!enabled()) return null;
  const classify = await load();
  touch();
  const input = String(text).slice(0, MAX_CHARS);
  const output = await classify(input, { aggregation_strategy: 'simple' });
  const items = [];
  for (const span of output) {
    const category = CATEGORY_OF[String(span.entity_group || span.entity || '').replace(/^[BIES]-/, '')];
    if (!category || span.score < MIN_SCORE) continue;
    const value = tidy(input, String(span.word || '').trim(), category);
    if (value && value.length >= 2) items.push({ text: value, category });
  }
  return items;
}

const WORD = /[\p{L}\p{N}_]/u;

// Spans follow tokens, which can stop inside a word ("…, IB" of "IBAN",
// "xK2" of "9xK2"): widen them to whole words. A date must hold a digit, so
// "last month" is not a date, and words without digits around one are dropped
// ("10.18.4.27 yesterday").
function tidy(input, value, category) {
  let start = input.indexOf(value);
  if (start < 0) return null;
  let end = start + value.length;
  while (start > 0 && WORD.test(input[start - 1]) && WORD.test(input[start])) start -= 1;
  while (end < input.length && WORD.test(input[end]) && WORD.test(input[end - 1])) end += 1;
  let text = input.slice(start, end);
  if (category === 'birthdate') {
    // An IP address is not a date; the IP rule names it properly.
    if (!/\d/.test(text) || /\b\d{1,3}(\.\d{1,3}){3}\b/.test(text)) return null;
    text = text.replace(/^(?:[^\d\s]+\s+)+(?=\S*\d)/u, '').replace(/(?<=\d\S*)(?:\s+[^\d\s]+)+$/u, '');
  }
  return text;
}

function setLibraryForTests(fake) {
  loadLibrary = fake;
  pipe = null;
  loading = null;
  state.status = 'off';
}

module.exports = { status, setEnabled, resume, detect, enabled, setLibraryForTests, MODEL };
