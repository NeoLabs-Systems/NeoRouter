'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const sensitivity = require('../../server/services/processing/sensitivity');
const protocol = require('../../server/services/pools/protocol');
const sharePolicy = require('../../server/services/pools/share_policy');
const peerPrivacy = require('../../server/services/pools/peer_privacy');
const traffic = require('../../server/services/pools/traffic');

migrate(getDatabase(), { quiet: true });

const level = (content) => sensitivity.assess({ messages: [{ role: 'user', content }] });

test('very sensitive: real secrets, cards, seed phrases, ID numbers and bulk personal data', () => {
  for (const [text, reason] of [
    ['password: hunter2xyz', 'secret'],
    ['Mein Kennwort lautet Sommer2026', 'secret'],
    ['export GITHUB_TOKEN=ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789', 'secret'],
    ['-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABFwAAAAdzc2gtcn\n-----END OPENSSH PRIVATE KEY-----', 'secret'],
    ['card 4111 1111 1111 1111', 'card'],
    ['abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about', 'seed_phrase'],
    ['Steuer-ID 86095742719', 'government_id'],
    ['DOB 1980-02-01, DOB 1975-04-03, DOB 1990-06-05', 'personal_records'],
  ]) {
    const result = level(text);
    assert.equal(result.level, 'high', text);
    assert.ok(result.reasons.includes(reason), `${text}: ${result.reasons}`);
  }
});

test('not very sensitive: ordinary chat, code, placeholders, one-time codes and single contact details', () => {
  for (const text of [
    'hello, how are you?',
    'Mein Passwort ist sehr wichtig',
    'Here is code: const password = process.env.PASSWORD;',
    'password: ${DB_PASSWORD}',
    'token is SESSION_TOKEN',
    'Your code is 482913',
    'the pin is 4711',
    'order 4111111111111112',
  ]) assert.equal(level(text).level, 'none', text);
  for (const text of ['write to bob@example.com', 'my iban DE89370400440532013000']) assert.equal(level(text).level, 'low', text);
});

test('the scan covers tool arguments and text parts, never reporting values', () => {
  const result = sensitivity.assess({
    messages: [
      { role: 'user', content: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', content: null, tool_calls: [{ function: { name: 'login', arguments: '{"password":"Sup3r!Secret#9"}' } }] },
    ],
  });
  assert.equal(result.level, 'high');
  assert.ok(!JSON.stringify(result).includes('Sup3r'));
});

test('signed envelopes: tampering, wrong keys, old messages and replays are refused', () => {
  const db = getDatabase();
  db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u1', 'one', 'one@example.test', 'x', 'user')").run();
  db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u2', 'two', 'two@example.test', 'x', 'user')").run();
  const identity = require('../../server/services/pools/identity');
  const one = identity.ensure('u1');
  const two = identity.ensure('u2');
  const message = protocol.envelope('u1', 'leave', { a: 1 }, two.memberId);
  assert.throws(() => protocol.open(message, { type: 'leave', publicKey: two.publicKey }), /signature/);
  assert.throws(() => protocol.open({ ...message, body: { ...message.body, data: { a: 2 } } }, { type: 'leave', publicKey: one.publicKey }), /signature/);
  assert.throws(() => protocol.open(message, { type: 'remove', publicKey: one.publicKey }), /Unexpected/);
  assert.throws(() => protocol.open(message, { type: 'leave', publicKey: one.publicKey, now: Date.now() + 10 * 60_000 }), /too old/);
  assert.equal(protocol.open(message, { type: 'leave', publicKey: one.publicKey }).data.a, 1);
  assert.throws(() => protocol.open(message, { type: 'leave', publicKey: one.publicKey }), /already received/);
  assert.equal(protocol.canonical({ b: 1, a: [{ d: 2, c: 3 }] }), '{"a":[{"c":3,"d":2}],"b":1}');
  const ticket = { pool: 'p', to: two.memberId };
  const signature = protocol.signObject('u1', ticket);
  assert.equal(protocol.verifyObject(one.publicKey, ticket, signature), true);
  assert.equal(protocol.verifyObject(one.publicKey, { ...ticket, to: 'someone else' }, signature), false);
});

test('balance: a partner who takes far more than it gives gets gradually lower caps', () => {
  const db = getDatabase();
  db.prepare("INSERT INTO peer_links (id, user_id, pool_id, member_id, name, url, public_key) VALUES ('l1', 'u1', 'p1', 'm', 'P', 'https://p.example', 'k')").run();
  assert.equal(sharePolicy.balance('l1').state, 'new');
  traffic.add('l1', 'given', 90, 0);
  traffic.add('l1', 'taken', 30, 0);
  assert.equal(sharePolicy.balance('l1').factor, 1, 'below the minimum volume nothing changes');
  traffic.add('l1', 'given', 210, 0);
  const lopsided = sharePolicy.balance('l1');
  assert.equal(lopsided.state, 'giving_more');
  assert.equal(lopsided.factor, Number((3 / 10).toFixed(3)));
  const caps = sharePolicy.capsFor('l1', sharePolicy.shareSettings(null));
  assert.equal(caps.requests, Math.floor(sharePolicy.DEFAULT_SHARE.requestsPerDay * lopsided.factor));
  traffic.add('l1', 'given', 100000, 0);
  assert.equal(sharePolicy.balance('l1').factor, sharePolicy.BALANCE.floor, 'never below the floor');
});

test('share settings are clamped to safe bounds', () => {
  const settings = sharePolicy.shareSettings({ share: { requestsPerDay: -5, concurrent: 999, reserve: 'x' } });
  assert.equal(settings.requestsPerDay, 1);
  assert.equal(settings.concurrent, 20);
  assert.equal(settings.reserve, sharePolicy.DEFAULT_SHARE.reserve);
});

test('the pool shield minimum: hidden secrets, placeholders for contact data, nothing else when the shield is off', () => {
  const { DEFAULT_SETTINGS } = require('../../server/services/processing/processing_service');
  const off = peerPrivacy.withPoolMinimum(DEFAULT_SETTINGS);
  assert.equal(off.privacy.enabled, true);
  assert.equal(off.privacy.modes.secret, 'hide');
  assert.equal(off.privacy.modes.card, 'hide');
  assert.equal(off.privacy.modes.email, 'restore');
  assert.equal(off.privacy.modes.person, 'off', 'names stay as they are when the shield is off');
  const on = peerPrivacy.withPoolMinimum({ ...DEFAULT_SETTINGS, privacy: { ...DEFAULT_SETTINGS.privacy, enabled: true, modes: { ...DEFAULT_SETTINGS.privacy.modes, secret: 'restore', person: 'restore' } } });
  assert.equal(on.privacy.modes.secret, 'hide', 'secrets never come back from a partner round trip');
  assert.equal(on.privacy.modes.person, 'restore', 'own choices above the minimum stay');
});
