'use strict';

// Pools with a member that has no public address. Alex hosts; Bea's own
// address does not answer, so she takes part through Alex (the relay); Cem
// has a public address. All three run on one test server, and every call
// goes over real HTTP and WebSockets, as between three machines.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const WebSocket = require('ws');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const federation = require('../../server/routes/federation');
const pools = require('../../server/services/pools/pool_service');
const identity = require('../../server/services/pools/identity');
const relay = require('../../server/services/pools/relay');
const { helloText } = require('../../server/services/pools/relay/frames');

let server;
let base;
let poolId;
const mocks = {};
const people = {};
const SECRET_PROMPT = 'the-secret-recipe-42';

const api = (who, method, path, body) => {
  const call = request(base)[method](`/api/v1${path}`).set('Authorization', `Bearer ${people[who].token}`);
  return body ? call.send(body) : call;
};
const chat = (who, model, extra = {}) => request(base).post('/v1/chat/completions')
  .set('Authorization', `Bearer ${people[who].poolKey}`).send({ model, messages: [{ role: 'user', content: SECRET_PROMPT }], ...extra });

async function waitFor(check, what, timeoutMs = 10_000) {
  const until = Date.now() + timeoutMs;
  for (;;) {
    const value = await check();
    if (value) return value;
    if (Date.now() > until) throw new Error(`Timed out waiting for ${what}`);
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}

// The id of a partner's model in this person's catalog, once it is there.
const partnerModel = (who, partner, model) => waitFor(async () => {
  const models = (await api(who, 'get', '/models?all=1')).body.models || [];
  return models.find((item) => item.providerSlug === `pool-${partner}` && item.id.endsWith(`/${model}`) && item.available !== false)?.id;
}, `${who} to see ${partner}'s ${model}`);

async function register(name) {
  const response = await request(base).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: 'correct-horse-battery' });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  people[name] = { id: response.body.user.id, token: response.body.session.token };
}

const memberIdOf = (who) => identity.ensure(people[who].id).memberId;
const invite = async () => (await api('alex', 'post', `/pools/${poolId}/invites`)).body.link;

test.before(async () => {
  const app = createApp();
  server = app.listen(0);
  await new Promise((resolve) => server.once('listening', resolve));
  relay.hub.attach(server);
  base = `http://127.0.0.1:${server.address().port}`;
  for (const who of ['alex', 'bea', 'cem']) mocks[who] = await createMockProvider({ models: [`ok-${who}`] });
  await register('alex');
  await api('alex', 'patch', '/admin/settings', { registrationOpen: true });
  await register('bea');
  await register('cem');
  for (const who of ['alex', 'bea', 'cem']) {
    const added = await api(who, 'post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'mock', baseUrl: mocks[who].url, secret: 'good-key', billingMode: 'free_tier' });
    assert.equal(added.status, 201, JSON.stringify(added.body));
    people[who].account = added.body.account.id;
    const key = await api(who, 'post', '/keys', { name: 'pool', poolAccess: 'pool' });
    people[who].poolKey = key.body.secret;
  }
});

test.beforeEach(() => federation.limiter.clear());

test.after(async () => {
  relay.connector.stopAllForTests();
  for (const mock of Object.values(mocks)) await mock.close();
  server.closeAllConnections?.();
  await new Promise((resolve) => server.close(resolve));
});

test('a NeoRouter whose address does not answer joins through the host, on protocol v2', async () => {
  const created = await api('alex', 'post', '/pools', { name: 'Friends', selfName: 'alex', sharedAccountIds: [people.alex.account] });
  poolId = created.body.poolId;
  const link = await invite();
  const preview = await api('bea', 'post', '/pools/preview', { link });
  assert.equal(preview.body.relay, true, 'the host offers to relay');
  // Bea's own address (a closed port) does not answer, so the host relays her.
  await pools.join(people.bea.id, { link, selfName: 'bea', selfUrl: 'http://127.0.0.1:1', sharedAccountIds: [people.bea.account] });
  const bea = (await api('bea', 'get', '/pools')).body.pools[0];
  assert.equal(bea.relay.viaHost, true);
  assert.equal(bea.relay.connected, true, 'connected to the host before introducing herself');
  const seenByHost = (await api('alex', 'get', '/pools')).body.pools[0].partners.find((partner) => partner.name === 'bea');
  assert.equal(seenByHost.viaHost, true);
  // Older NeoRouters only take http(s) addresses, so they leave her out; the protocol stays v2.
  const card = relay.address.format(poolId, memberIdOf('bea'));
  assert.match(card, /^relay:\/\//);
  assert.equal(require('../../server/services/pools/protocol').VERSION, 2);
});

test('the host uses the relayed member\'s models, and the host sees only sealed frames', async () => {
  const model = await partnerModel('alex', 'bea', 'ok-bea');
  const seen = [];
  const { connections } = relay.hub.forTests();
  const beaConn = connections.get(`${poolId}|${memberIdOf('bea')}`);
  const deliver = beaConn.deliver;
  beaConn.deliver = (frame) => { seen.push(JSON.stringify(frame)); return deliver(frame); };
  try {
    const before = mocks.bea.calls.length;
    const plain = await chat('alex', model);
    assert.equal(plain.status, 200, JSON.stringify(plain.body));
    assert.equal(plain.body.choices[0].message.content, 'hello from ok-bea');
    assert.equal(mocks.bea.calls.length, before + 1);
    const streamed = await chat('alex', model, { stream: true });
    assert.equal(streamed.status, 200);
    assert.match(streamed.text, /hello/);
  } finally {
    beaConn.deliver = deliver;
  }
  assert.ok(seen.length > 0, 'frames went through the hub');
  const wire = seen.join('\n');
  assert.ok(!wire.includes(SECRET_PROMPT), 'the prompt never travels in the clear');
  assert.ok(!wire.includes('nrt_'), 'neither does the partner key');
});

test('a request the host replays is refused, and cannot make the member reuse a key', async () => {
  const model = await partnerModel('alex', 'bea', 'ok-bea');
  const { connections, localEndpoints, route } = relay.hub.forTests();
  const beaConn = connections.get(`${poolId}|${memberIdOf('bea')}`);
  const deliver = beaConn.deliver;
  let kept = null;
  const heads = [];
  beaConn.deliver = (frame) => { if (frame.t === 'open') kept = frame; return deliver(frame); };
  const alexConn = localEndpoints.get(`${people.alex.id}|${poolId}`).conn;
  const alexDeliver = alexConn.deliver;
  alexConn.deliver = (frame) => { if (frame.t === 'head' || frame.t === 'fail') heads.push(frame); return alexDeliver(frame); };
  try {
    assert.equal((await chat('alex', model)).status, 200);
    const firstSalt = heads.find((frame) => frame.t === 'head').r;
    const before = mocks.bea.calls.length;
    heads.length = 0;
    // The host sends the same sealed request again.
    route(alexConn, { t: 'open', s: kept.s, to: memberIdOf('bea'), seq: kept.seq, d: kept.d });
    const answer = await waitFor(() => heads[0], 'Bea to answer the replay');
    assert.equal(answer.t, 'fail');
    assert.equal(answer.code, 'replayed');
    assert.equal(mocks.bea.calls.length, before, 'the replayed request never ran');
    assert.ok(firstSalt, 'each answer carries the answerer\'s own fresh salt');
  } finally {
    beaConn.deliver = deliver;
    alexConn.deliver = alexDeliver;
  }
});

test('a later member list cannot swap the sealing key of a member we know', () => {
  const crypto = require('node:crypto');
  const store = require('../../server/services/pools/store');
  const known = store.member(people.bea.id, poolId, memberIdOf('alex'));
  const fake = crypto.generateKeyPairSync('ed25519');
  const fakeBox = crypto.generateKeyPairSync('x25519').publicKey.export({ format: 'der', type: 'spki' }).toString('base64');
  const fakePublic = fake.publicKey.export({ format: 'der', type: 'spki' }).toString('base64');
  const fakeSig = crypto.sign(null, Buffer.from(`neorouter-pool-box:${fakeBox}`), fake.privateKey).toString('base64');
  pools.forTests.rememberMember(people.bea.id, poolId, { ...known, publicKey: fakePublic, boxKey: fakeBox, boxSig: fakeSig });
  const after = store.member(people.bea.id, poolId, memberIdOf('alex'));
  assert.equal(after.publicKey, known.publicKey);
  assert.equal(after.boxKey, known.boxKey);
});

test('a member with a public address links with the relayed one through the host', async () => {
  const joined = await api('cem', 'post', '/pools/join', { link: await invite(), selfName: 'cem', sharedAccountIds: [people.cem.account] });
  assert.equal(joined.status, 200, JSON.stringify(joined.body));
  const cem = joined.body.pools[0];
  assert.equal(cem.relay.viaHost, false, 'Cem answers directly');
  const toBea = await partnerModel('cem', 'bea', 'ok-bea');
  const viaRelay = await chat('cem', toBea);
  assert.equal(viaRelay.status, 200, JSON.stringify(viaRelay.body));
  assert.equal(viaRelay.body.choices[0].message.content, 'hello from ok-bea');
  // Bea reaches Cem directly.
  const toCem = await partnerModel('bea', 'cem', 'ok-cem');
  const direct = await chat('bea', toCem);
  assert.equal(direct.status, 200, JSON.stringify(direct.body));
});

test('a relayed member answers only the calls partners may make', async () => {
  const endpoint = relay.hub.localEndpoint(people.alex.id, poolId);
  const refused = await endpoint.request(memberIdOf('bea'), { method: 'GET', path: '/api/v1/keys', headers: { authorization: `Bearer ${people.bea.token}` } });
  assert.equal(refused.status, 403);
  assert.equal((await refused.json()).error.code, 'POOL_RELAY_REFUSED');
  const wrongKey = await endpoint.request(memberIdOf('bea'), { method: 'GET', path: '/v1/models', headers: { authorization: 'Bearer nrt_00000000_notarealkeynotarealkey' } });
  assert.equal(wrongKey.status, 401, 'the usual key check still applies');
});

test('logging in to the relay needs a valid signature by a current member', async () => {
  const attempt = (sign) => new Promise((resolve) => {
    const ws = new WebSocket(`${base.replace('http', 'ws')}${relay.hub.PATH}`);
    ws.on('message', (data) => {
      const frame = JSON.parse(data.toString());
      if (frame.t === 'challenge') ws.send(JSON.stringify({ t: 'hello', pool: poolId, member: memberIdOf('cem'), sig: sign(frame.nonce) }));
      if (frame.t === 'ready') { ws.close(); resolve('ready'); }
    });
    ws.on('close', (code) => resolve(code));
  });
  const valid = (nonce) => identity.sign(people.cem.id, helloText({ nonce, pool: poolId, member: memberIdOf('cem'), host: memberIdOf('alex') }));
  assert.equal(await attempt(() => Buffer.alloc(64).toString('base64')), 4403, 'a forged signature is refused');
  assert.equal(await attempt((nonce) => identity.sign(people.bea.id, helloText({ nonce, pool: poolId, member: memberIdOf('cem'), host: memberIdOf('alex') }))), 4403, 'someone else\'s key is refused');
  assert.equal(await attempt(valid), 'ready');
});

test('a dropped connection comes back by itself', async () => {
  const { connections } = relay.hub.forTests();
  const dropped = connections.get(`${poolId}|${memberIdOf('bea')}`);
  dropped.ws.terminate();
  await waitFor(() => connections.get(`${poolId}|${memberIdOf('bea')}`) !== dropped && relay.hub.isOnline(poolId, memberIdOf('bea')), 'Bea to reconnect');
  const model = await partnerModel('alex', 'bea', 'ok-bea');
  const again = await waitFor(async () => {
    const response = await chat('alex', model);
    return response.status === 200 ? response : null;
  }, 'a chat through the relay after reconnecting');
  assert.equal(again.body.choices[0].message.content, 'hello from ok-bea');
});

test('removing a relayed member reaches her through the host', async () => {
  const beaMember = memberIdOf('bea');
  const removed = await api('alex', 'delete', `/pools/${poolId}/members/${beaMember}`);
  assert.equal(removed.status, 200, JSON.stringify(removed.body));
  await waitFor(async () => (await api('bea', 'get', '/pools')).body.pools.length === 0, 'Bea to hear she was removed');
  await waitFor(() => !relay.hub.isOnline(poolId, beaMember), 'her relay connection to end');
});

test('with the host\'s switch off, nobody is relayed and new joiners need their own address', async () => {
  const off = await api('alex', 'put', `/pools/${poolId}/relay`, { enabled: false });
  assert.equal(off.status, 200, JSON.stringify(off.body));
  assert.equal(off.body.pools[0].relay.offered, false);
  const preview = await api('bea', 'post', '/pools/preview', { link: await invite() });
  assert.equal(preview.body.relay, false);
  const refused = await pools.join(people.bea.id, { link: await invite(), selfName: 'bea', selfUrl: 'not a url', sharedAccountIds: [] }).catch((error) => error);
  assert.equal(refused.code, 'POOL_URL_UNKNOWN');
  await api('alex', 'put', `/pools/${poolId}/relay`, { enabled: true });
});
