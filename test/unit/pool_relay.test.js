'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const seal = require('../../server/services/pools/relay/seal');
const address = require('../../server/services/pools/relay/address');
const { allowed } = require('../../server/services/pools/relay/endpoint');
const { validStreamFrame } = require('../../server/services/pools/relay/frames');

const POOL = '11111111-1111-4111-8111-111111111111';
const A = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
const B = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';

function member(memberId) {
  const { publicKey, privateKey } = crypto.generateKeyPairSync('x25519');
  const boxKey = publicKey.export({ format: 'der', type: 'spki' }).toString('base64');
  return { memberId, boxKey, own: { privateKey, ownBoxKey: boxKey, ownMemberId: memberId } };
}

const alice = member(A);
const bob = member(B);
const pairAB = seal.pairKey(alice.own, bob, POOL);
const pairBA = seal.pairKey(bob.own, alice, POOL);
const stream = (pair, from, to, streamId = 'stream-1') => seal.direction(pair, { poolId: POOL, streamId, from, to });

test('both members derive the same pair key; another pool or member does not', () => {
  assert.deepEqual(pairAB, pairBA);
  assert.notDeepEqual(seal.pairKey(alice.own, bob, '22222222-2222-4222-8222-222222222222'), pairAB);
  assert.notDeepEqual(seal.pairKey(alice.own, member(B), POOL), pairAB, 'a different sealing key gives a different pair key');
});

test('a sealed frame opens on the other side, in order', () => {
  const out = stream(pairAB, A, B);
  const back = stream(pairBA, A, B);
  const first = out.seal('open', 'hello');
  const second = out.seal('data', 'world');
  assert.equal(back.open('open', first.seq, first.d).toString(), 'hello');
  assert.equal(back.open('data', second.seq, second.d).toString(), 'world');
});

test('changed, replayed, reordered or moved frames do not open', () => {
  const frame = stream(pairAB, A, B).seal('open', 'hello');
  const tampered = Buffer.from(frame.d, 'base64');
  tampered[0] ^= 1;
  assert.throws(() => stream(pairBA, A, B).open('open', 0, tampered.toString('base64')));
  const receiver = stream(pairBA, A, B);
  receiver.open('open', 0, frame.d);
  assert.throws(() => receiver.open('open', 0, frame.d), /order/, 'a replay is refused');
  const out = stream(pairAB, A, B);
  out.seal('data', 'one');
  const two = out.seal('data', 'two');
  assert.throws(() => stream(pairBA, A, B).open('data', two.seq, two.d), /order/, 'a skipped frame is refused');
  assert.throws(() => stream(pairBA, A, B, 'stream-2').open('open', 0, frame.d), 'another stream cannot open it');
  assert.throws(() => stream(pairBA, B, A).open('open', 0, frame.d), 'nor the other direction');
  assert.throws(() => stream(pairBA, A, B).open('data', 0, frame.d), 'nor as another frame type');
  const stranger = member('cccccccc-cccc-4ccc-8ccc-cccccccccccc');
  assert.throws(() => stream(seal.pairKey(stranger.own, alice, POOL), A, B).open('open', 0, frame.d), 'nor a third member (or the host)');
});

test('relay addresses name the pool and the member', () => {
  const value = address.format(POOL, B);
  assert.equal(value, `relay://${POOL}.${B}`);
  assert.deepEqual(address.parse(`${value}/v1/models?x=1`), { poolId: POOL, memberId: B, path: '/v1/models?x=1' });
  assert.equal(address.parse('https://router.example.com'), null);
  assert.equal(address.parse('relay://evil.example.com'), null);
  // Older NeoRouters accept only http(s) addresses, so they leave such members out.
  assert.ok(!['http:', 'https:'].includes(new URL(value).protocol));
});

test('a relayed member answers only partner calls', () => {
  assert.ok(allowed('GET', '/v1/models'));
  assert.ok(allowed('POST', '/v1/chat/completions'));
  assert.ok(allowed('POST', '/federation/v1/introduce'));
  assert.ok(!allowed('GET', '/api/v1/keys'));
  assert.ok(!allowed('POST', '/federation/v1/join'), 'joining goes to the host, never through it');
  assert.ok(!allowed('POST', '/v1/chat/completions/../../api/v1/admin'));
  assert.ok(!allowed('DELETE', '/v1/models'));
});

test('stream frames are checked before they are passed along', () => {
  assert.ok(validStreamFrame({ t: 'open', s: 'A'.repeat(22), to: B, seq: 0, d: 'x' }));
  assert.ok(!validStreamFrame({ t: 'open', s: 'short', to: B, seq: 0, d: 'x' }));
  assert.ok(!validStreamFrame({ t: 'open', s: 'A'.repeat(22), to: 'not-a-member', seq: 0, d: 'x' }));
  assert.ok(!validStreamFrame({ t: 'data', s: 'A'.repeat(22), seq: -1, d: 'x' }));
  assert.ok(!validStreamFrame({ t: 'hello', s: 'A'.repeat(22) }));
});
