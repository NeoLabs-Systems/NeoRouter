'use strict';

// The frames on a relay connection (JSON text over a WebSocket), and the
// limits both ends hold each other to.
//
// Logging in (between a member and the host):
//   host → member   { t: 'challenge', nonce }
//   member → host   { t: 'hello', pool, member, sig }   sig: the member's pool key over HELLO
//   host → member   { t: 'ready', online: [memberId] }   then { t: 'presence', member, online }
// A stream (one request and its answer), passed along by the host:
//   asker → host → answerer   { t: 'open', s, to | from, seq, d }   sealed request
//   answerer → asker          { t: 'head', s, seq, d, r } then 'data' / 'end'   sealed status, body, end
//                             (r: the answerer's fresh salt for its key, in the clear)
//   either side               { t: 'cancel', s } / { t: 'fail', s, code }   in the clear, end the stream
// d is always sealed between the two members (seal.js); the host only reads
// t, s, to/from and seq.
const LIMITS = Object.freeze({
  frameBytes: 12 * 1024 * 1024,
  requestBodyBytes: 8 * 1024 * 1024,
  chunkBytes: 64 * 1024,
  streamsPerConnection: 64,
  streamIdleMs: 10 * 60_000,
  // A sealed request older or newer than this is refused; ids are remembered twice as long.
  requestSkewMs: 5 * 60_000,
  // The host stops passing a stream on when a member does not read fast enough.
  bufferedBytes: 16 * 1024 * 1024,
  // Frames before logging in are small.
  helloBytes: 4096,
  helloMs: 10_000,
  heartbeatMs: 25_000,
});

const STREAM_TYPES = new Set(['open', 'head', 'data', 'end', 'cancel', 'fail']);
const SEALED_TYPES = new Set(['open', 'head', 'data', 'end']);
const { UUID } = require('./address');
const STREAM_ID = /^[A-Za-z0-9_-]{22}$/;

/** The text that a member signs to log in. */
const helloText = ({ nonce, pool, member, host }) => `neorouter-pool-relay-hello|${nonce}|${pool}|${member}|${host}`;

function parse(text) {
  if (typeof text !== 'string' || text.length > LIMITS.frameBytes) return null;
  let frame;
  try { frame = JSON.parse(text); } catch { return null; }
  return frame && typeof frame === 'object' && typeof frame.t === 'string' ? frame : null;
}

/** Whether a stream frame is well formed (ids, sequence number, sealed data where needed). */
function validStreamFrame(frame) {
  if (!STREAM_TYPES.has(frame.t) || !STREAM_ID.test(String(frame.s || ''))) return false;
  if (frame.to !== undefined && !UUID.test(String(frame.to))) return false;
  if (frame.from !== undefined && !UUID.test(String(frame.from))) return false;
  if (frame.t === 'head' && !STREAM_ID.test(String(frame.r || ''))) return false;
  if (SEALED_TYPES.has(frame.t)) return Number.isInteger(frame.seq) && frame.seq >= 0 && frame.seq < 2 ** 31 && typeof frame.d === 'string';
  return frame.code === undefined || /^[a-z_]{1,40}$/.test(String(frame.code));
}

const encode = (frame) => JSON.stringify(frame);

module.exports = { LIMITS, helloText, parse, validStreamFrame, encode };
