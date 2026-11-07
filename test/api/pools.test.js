'use strict';

// Pools between NeoRouters. Three people (Alex, Bea, Cem) take part from one
// test server: each has their own space, signing key and provider, and every
// pool call travels over real HTTP to the server's own address, exactly as it
// would between three machines.
require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');
const { getDatabase } = require('../../server/db/database');
const federation = require('../../server/routes/federation');
const sharePolicy = require('../../server/services/pools/share_policy');

let server;
let base;
const mocks = {};
const people = {};

const api = (who, method, path, body) => {
  const call = request(base)[method](`/api/v1${path}`).set('Authorization', `Bearer ${people[who].token}`);
  return body ? call.send(body) : call;
};
const chat = (who, keyName, model, content = 'hi', extra = {}) => request(base).post('/v1/chat/completions')
  .set('Authorization', `Bearer ${people[who].keys[keyName]}`).send({ model, messages: [{ role: 'user', content }], ...extra });

async function register(name) {
  const response = await request(base).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: 'correct-horse-battery' });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  people[name] = { id: response.body.user.id, token: response.body.session.token, keys: {} };
}

async function addKey(who, name, input = {}) {
  const response = await api(who, 'post', '/keys', { name, ...input });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  people[who].keys[name] = response.body.secret;
}

// The id of the partner's model in this person's catalog.
async function partnerModel(who, partner, model) {
  const response = await api(who, 'get', '/models?all=1');
  const entry = response.body.models.find((item) => item.providerSlug === `pool-${partner}` && item.id.endsWith(`/${model}`));
  assert.ok(entry, `${who} sees ${partner}'s ${model}: ${response.body.models.map((item) => item.id).join(', ')}`);
  return entry.id;
}

// The first pool of each person (these tests use one, plus a second at the end).
const summary = async (who) => (await api(who, 'get', '/pools')).body.pools[0] || null;
let poolId = null;

test.before(async () => {
  const app = createApp();
  server = app.listen(0);
  await new Promise((resolve) => server.once('listening', resolve));
  base = `http://127.0.0.1:${server.address().port}`;
  mocks.alex = await createMockProvider({ models: ['ok-alex'] });
  mocks.bea = await createMockProvider({ models: ['echo-bea', 'ok-bea'] });
  mocks.cem = await createMockProvider({ models: ['ok-cem'] });
  await register('alex');
  await api('alex', 'patch', '/admin/settings', { registrationOpen: true });
  await register('bea');
  await register('cem');
  for (const who of ['alex', 'bea', 'cem']) {
    const added = await api(who, 'post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'mock', baseUrl: mocks[who].url, secret: 'good-key', billingMode: 'free_tier' });
    assert.equal(added.status, 201, JSON.stringify(added.body));
    people[who].account = added.body.account.id;
  }
});

test.beforeEach(() => federation.limiter.clear());

test.after(async () => {
  for (const mock of Object.values(mocks)) await mock.close();
  await new Promise((resolve) => server.close(resolve));
});

async function invite() {
  const response = await api('alex', 'post', `/pools/${poolId}/invites`);
  assert.equal(response.status, 201, JSON.stringify(response.body));
  return response.body.link;
}

async function join(who, link) {
  const response = await api(who, 'post', '/pools/join', { link, selfName: who, sharedAccountIds: [people[who].account] });
  assert.equal(response.status, 200, JSON.stringify(response.body));
  return response.body.pools.find((pool) => pool.id === response.body.poolId);
}

test('creating a pool and inviting: the link is single-use and only its hash is stored', async () => {
  const created = await api('alex', 'post', '/pools', { name: 'Friends', selfName: 'alex', sharedAccountIds: [people.alex.account] });
  assert.equal(created.status, 201, JSON.stringify(created.body));
  poolId = created.body.poolId;
  const mine = created.body.pools[0];
  assert.equal(mine.isHost, true);
  assert.deepEqual(mine.partners, []);
  assert.deepEqual(mine.sharedAccountIds, [people.alex.account]);
  const link = await invite();
  assert.match(link, /\/pool\/join\?pool=[A-Za-z0-9_-]{32}$/);
  const token = new URL(link).searchParams.get('pool');
  assert.equal(getDatabase().prepare('SELECT COUNT(*) n FROM pool_invites WHERE token_hash = ?').get(token).n, 0);
  const preview = await api('bea', 'post', '/pools/preview', { link });
  assert.equal(preview.status, 200, JSON.stringify(preview.body));
  assert.equal(preview.body.pool.name, 'Friends');
  assert.equal(preview.body.host.name, 'alex');
  const joined = await join('bea', link);
  assert.equal(joined.name, 'Friends');
  assert.equal(joined.partners.length, 1);
  assert.equal(joined.partners[0].name, 'alex');
  assert.equal(joined.partners[0].state, 'online', JSON.stringify(joined.partners[0]));
  const reused = await api('cem', 'post', '/pools/join', { link, selfName: 'cem', sharedAccountIds: [] });
  assert.equal(reused.body.error.code, 'POOL_INVITE_INVALID');
});

test('a third member is introduced to everyone and all three are linked', async () => {
  await join('cem', await invite());
  for (const who of ['alex', 'bea', 'cem']) {
    const view = await summary(who);
    assert.deepEqual(view.partners.map((partner) => partner.state), ['online', 'online'], `${who}: ${JSON.stringify(view.partners)}`);
  }
  // Partner keys are not on the keys page, and partners are not listed as connections.
  const keys = await api('bea', 'get', '/keys');
  assert.equal(keys.body.keys.filter((key) => key.name.startsWith('Pool:')).length, 0);
  const accounts = await api('bea', 'get', '/providers/accounts');
  assert.deepEqual(accounts.body.accounts.map((account) => account.definitionId), ['custom_openai']);
});

test('a pool key uses a partner’s free model; an own key never does', async () => {
  await addKey('alex', 'pool', { poolAccess: 'pool' });
  await addKey('alex', 'own');
  const model = await partnerModel('alex', 'bea', 'ok-bea');
  const before = mocks.bea.calls.length;
  const ok = await chat('alex', 'pool', model, 'hi', { user: 'end-user-42' });
  assert.equal(ok.status, 200, JSON.stringify(ok.body));
  assert.equal(ok.body.choices[0].message.content, 'hello from ok-bea');
  const call = mocks.bea.calls.at(-1);
  assert.equal(mocks.bea.calls.length, before + 1);
  assert.equal(call.model, 'ok-bea');
  const own = await chat('alex', 'own', model);
  assert.notEqual(own.status, 200);
  assert.equal(mocks.bea.calls.length, before + 1, 'an own key never reaches a partner');
  // The model list matches: a pool key lists the partner's model, an own key does not.
  const listed = async (keyName) => (await request(base).get('/v1/models').set('Authorization', `Bearer ${people.alex.keys[keyName]}`)).body.data;
  const poolList = await listed('pool');
  const viaBea = poolList.find((item) => item.id === model);
  assert.ok(viaBea, `a pool key lists ${model}`);
  assert.match(viaBea.name, /via bea/);
  assert.equal(viaBea.neorouter.free, true);
  assert.ok(poolList.some((item) => item.id.startsWith('auto')), 'autos are listed too');
  assert.equal((await listed('own')).some((item) => item.id === model), false, 'an own key does not list partner models');
});

test('the partner sees counts only: no per-request log, no route headers, no content', async () => {
  const model = await partnerModel('alex', 'bea', 'ok-bea');
  const response = await chat('alex', 'pool', model);
  assert.equal(response.status, 200);
  const logs = await api('bea', 'get', '/usage/logs');
  assert.equal(logs.body.logs.length, 0, 'partner requests are not listed one by one');
  const view = await summary('bea');
  const alex = view.partners.find((partner) => partner.name === 'alex');
  assert.ok(alex.today.given.requests >= 2, JSON.stringify(alex.today));
  const alexView = await summary('alex');
  assert.ok(alexView.partners.find((partner) => partner.name === 'bea').today.taken.requests >= 2);
  // Rows kept on Bea's side hold no error text and no trail messages.
  const rows = getDatabase().prepare('SELECT r.error_message, r.trail_json FROM requests r JOIN router_keys k ON k.id = r.router_key_id WHERE k.peer_link_id IS NOT NULL').all();
  assert.ok(rows.length >= 2);
  for (const row of rows) {
    assert.equal(row.error_message, null);
    assert.ok(!row.trail_json.includes('message'));
  }
});

test('private values become placeholders before they reach a partner, and come back in the answer', async () => {
  const model = await partnerModel('alex', 'bea', 'echo-bea');
  const response = await chat('alex', 'pool', model, 'Please write to jane.doe@example.com');
  assert.equal(response.status, 200, JSON.stringify(response.body));
  const sent = JSON.stringify(mocks.bea.calls.at(-1).messages);
  assert.ok(!sent.includes('jane.doe@example.com'), sent);
  assert.match(sent, /\[EMAIL_1\]/);
  assert.match(response.body.choices[0].message.content, /jane\.doe@example\.com/);
});

test('very sensitive requests stay on own connections', async () => {
  const secret = 'Deploy with password: Sup3r!Secret#9';
  const model = await partnerModel('alex', 'bea', 'ok-bea');
  const before = mocks.bea.calls.length;
  const kept = await chat('alex', 'pool', model, secret);
  assert.equal(kept.status, 503, JSON.stringify(kept.body));
  assert.match(kept.body.error.message, /very sensitive data/);
  assert.equal(mocks.bea.calls.length, before, 'never sent to the partner');
  const auto = await chat('alex', 'pool', 'auto', secret);
  assert.equal(auto.status, 200, JSON.stringify(auto.body));
  assert.equal(auto.headers['x-neorouter-kept-private'], 'secret');
  assert.equal(mocks.bea.calls.length, before);
  // Switched off, the same request may go to the partner (with the secret hidden).
  await api('alex', 'put', '/pools/privacy', { keepSensitivePrivate: false });
  const sent = await chat('alex', 'pool', model, secret);
  assert.equal(sent.status, 200, JSON.stringify(sent.body));
  assert.ok(!JSON.stringify(mocks.bea.calls.at(-1).messages).includes('Sup3r!Secret#9'));
  await api('alex', 'put', '/pools/privacy', { keepSensitivePrivate: true });
});

test('a partner request is never passed on, and the hop header is refused on ordinary keys', async () => {
  // Bea lists only her own shared models to Alex, not Cem's.
  const listed = await api('alex', 'get', '/models?all=1');
  const viaBea = listed.body.models.filter((model) => model.providerSlug === 'pool-bea');
  assert.ok(viaBea.length > 0);
  assert.ok(!viaBea.some((model) => model.id.startsWith('pool-bea/pool-')), viaBea.map((model) => model.id).join(', '));
  await addKey('bea', 'own');
  const hop = await request(base).post('/v1/chat/completions').set('Authorization', `Bearer ${people.bea.keys.own}`).set('X-NeoRouter-Hop', '1')
    .send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(hop.status, 403);
  assert.equal(hop.body.error.code, 'POOL_HOP_REFUSED');
  assert.equal(sharePolicy.rejection({ definitionId: 'neorouter_peer', isFree: true, accountId: 'x' }), 'peer_no_forward');
});

test('what is shared: unshared connections and used-up shares are refused', async () => {
  const model = await partnerModel('alex', 'bea', 'ok-bea');
  await api('bea', 'put', `/pools/${poolId}/accounts/${people.bea.account}/share`, { shared: false });
  const unshared = await chat('alex', 'pool', model);
  assert.notEqual(unshared.status, 200);
  await api('bea', 'put', `/pools/${poolId}/accounts/${people.bea.account}/share`, { shared: true });
  const used = (await summary('bea')).partners.find((partner) => partner.name === 'alex').today.given.requests;
  const capped = await api('bea', 'put', `/pools/${poolId}/share`, { requestsPerDay: used + 1 });
  assert.equal(capped.status, 200, JSON.stringify(capped.body));
  assert.equal((await chat('alex', 'pool', model)).status, 200);
  const over = await chat('alex', 'pool', model);
  assert.notEqual(over.status, 200, 'the day’s share is used up');
  await api('bea', 'put', `/pools/${poolId}/share`, { requestsPerDay: 300 });
});

test('each connection shares the kinds ticked for it; paid models only when ticked, at no cost to the partner', async () => {
  mocks.beaPaid = await createMockProvider({ models: ['paid-bea'] });
  const added = await api('bea', 'post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'paidmock', baseUrl: mocks.beaPaid.url, secret: 'good-key', billingMode: 'paid' });
  assert.equal(added.status, 201, JSON.stringify(added.body));
  const paidAccount = added.body.account.id;
  const offered = (await api('bea', 'get', '/pools')).body.shareable.find((account) => account.id === paidAccount);
  assert.deepEqual([offered.freeModels, offered.planModels, offered.paidModels], [0, 0, 1]);
  const listsPaid = async () => {
    await api('alex', 'post', `/pools/${poolId}/sync`);
    return (await api('alex', 'get', '/models?all=1')).body.models.some((item) => item.providerSlug === 'pool-bea' && item.id.endsWith('/paid-bea'));
  };
  const share = (body) => api('bea', 'put', `/pools/${poolId}/accounts/${paidAccount}/share`, body);
  // Shared with the default (free models only): its paid model stays home, and so with "plan".
  await share({ shared: true });
  assert.deepEqual((await summary('bea')).shareKinds[paidAccount], ['free']);
  assert.equal(await listsPaid(), false);
  await share({ kinds: ['free', 'plan'] });
  assert.equal(await listsPaid(), false);
  // Paid ticked: Alex sees it as free (Bea pays) and Bea's connection serves it.
  const on = await share({ kinds: ['paid'] });
  assert.equal(on.status, 200, JSON.stringify(on.body));
  assert.deepEqual(on.body.pools[0].shareKinds[paidAccount], ['paid']);
  assert.equal(await listsPaid(), true);
  const model = await partnerModel('alex', 'bea', 'paid-bea');
  const entry = (await api('alex', 'get', '/models?all=1')).body.models.find((item) => item.id === model);
  assert.equal(entry.isFree, true, 'free for the partner');
  const before = mocks.beaPaid.calls.length;
  const served = await chat('alex', 'pool', model);
  assert.equal(served.status, 200, JSON.stringify(served.body));
  assert.equal(mocks.beaPaid.calls.length, before + 1);
  // Saving the list keeps each connection's kinds; leaving it out, or no kinds, stops sharing it.
  await api('bea', 'put', `/pools/${poolId}/share`, { sharedAccountIds: [people.bea.account, paidAccount] });
  assert.deepEqual((await summary('bea')).shareKinds[paidAccount], ['paid']);
  await api('bea', 'put', `/pools/${poolId}/share`, { sharedAccountIds: [people.bea.account] });
  assert.notEqual((await chat('alex', 'pool', model)).status, 200);
  assert.equal((await summary('bea')).shareKinds[paidAccount], undefined);
  await share({ kinds: ['paid'] });
  await share({ kinds: [] });
  assert.equal((await summary('bea')).shareKinds[paidAccount], undefined);
  assert.equal((await share({})).status, 400, 'a change needs shared or kinds');
  assert.equal((await share({ kinds: ['everything'] })).status, 400);
});

test('the pool activity counts what partners used of ours and we of theirs', async () => {
  const bea = (await api('bea', 'get', `/pools/${poolId}/activity?days=30`)).body;
  assert.equal(bea.days, 30);
  assert.ok(bea.given.requests > 0, JSON.stringify(bea.given));
  assert.ok(bea.given.byPartner.some((row) => row.name === 'alex'));
  assert.ok(bea.given.byConnection.some((row) => row.key === people.bea.account));
  assert.ok(bea.given.byModel.length > 0);
  const alex = (await api('alex', 'get', `/pools/${poolId}/activity`)).body;
  assert.equal(alex.days, 7, 'unknown ranges fall back to a week');
  assert.ok(alex.taken.byPartner.some((row) => row.name === 'bea'), JSON.stringify(alex.taken));
  assert.equal((await api('alex', 'get', '/pools/00000000-0000-0000-0000-000000000000/activity')).status, 404);
});

test('the quick switch pauses giving and using, and turning it on restores everything', async () => {
  const toBea = await partnerModel('alex', 'bea', 'ok-bea');
  await addKey('bea', 'pool', { poolAccess: 'pool' });
  const toAlex = await partnerModel('bea', 'alex', 'ok-alex');
  const paused = await api('alex', 'put', `/pools/${poolId}/paused`, { paused: true });
  assert.equal(paused.body.pools[0].paused, true);
  assert.notEqual((await chat('alex', 'pool', toBea)).status, 200, 'Alex uses no partner while paused');
  assert.notEqual((await chat('bea', 'pool', toAlex)).status, 200, 'and gives nothing');
  await api('alex', 'put', `/pools/${poolId}/paused`, { paused: false });
  const again = await chat('alex', 'pool', toBea);
  assert.equal(again.status, 200, JSON.stringify(again.body));
  const back = await chat('bea', 'pool', toAlex);
  assert.equal(back.status, 200, JSON.stringify(back.body));
});

test('messages with a wrong signature are refused', async () => {
  const forged = await request(base).post('/federation/v1/leave').send({ body: { v: 2, type: 'leave', from: '00000000-0000-0000-0000-000000000000', to: 'x', ts: Date.now(), nonce: 'n1', data: {} }, signature: 'AAAA' });
  assert.equal(forged.status, 404);
  const alexMember = getDatabase().prepare('SELECT member_id FROM pool_identities WHERE user_id = ?').get(people.alex.id).member_id;
  const beaMember = getDatabase().prepare('SELECT member_id FROM pool_identities WHERE user_id = ?').get(people.bea.id).member_id;
  const wrong = await request(base).post('/federation/v1/leave').send({ body: { v: 2, type: 'leave', from: beaMember, to: alexMember, ts: Date.now(), nonce: 'n2', data: { pool: poolId } }, signature: Buffer.alloc(64).toString('base64') });
  assert.equal(wrong.status, 401);
  assert.equal((await summary('alex')).partners.length, 2, 'nothing changed');
});

test('leaving removes both directions at once; the host can remove and close', async () => {
  const left = await api('cem', 'post', `/pools/${poolId}/leave`);
  assert.equal(left.status, 200, JSON.stringify(left.body));
  assert.deepEqual(left.body.pools, []);
  assert.equal(getDatabase().prepare('SELECT COUNT(*) n FROM peer_links WHERE user_id = ?').get(people.cem.id).n, 0);
  for (const who of ['alex', 'bea']) assert.deepEqual((await summary(who)).partners.map((partner) => partner.name).sort(), [who === 'alex' ? 'bea' : 'alex']);
  const beaMember = getDatabase().prepare('SELECT member_id FROM pool_identities WHERE user_id = ?').get(people.bea.id).member_id;
  const removed = await api('alex', 'delete', `/pools/${poolId}/members/${beaMember}`);
  assert.equal(removed.status, 200, JSON.stringify(removed.body));
  assert.equal(removed.body.pools[0].partners.length, 0);
  assert.equal(await summary('bea'), null, 'Bea was told and left the pool');
  const afterwards = await chat('bea', 'pool', 'auto');
  assert.equal(afterwards.status, 200, `a pool key keeps working on own models after the pool is gone: ${JSON.stringify(afterwards.body)}`);
  assert.equal(getDatabase().prepare("SELECT COUNT(*) n FROM router_keys WHERE peer_link_id IS NOT NULL").get().n, 0, 'no partner key is left anywhere');
  assert.equal(getDatabase().prepare("SELECT COUNT(*) n FROM provider_accounts WHERE definition_id = 'neorouter_peer'").get().n, 0);
  await api('alex', 'post', `/pools/${poolId}/leave`);
  assert.equal(await summary('alex'), null);
});

test('one person can be in several pools, each with its own sharing and switch', async () => {
  const first = await api('alex', 'post', '/pools', { name: 'Work', selfName: 'alex', sharedAccountIds: [people.alex.account] });
  const second = await api('alex', 'post', '/pools', { name: 'Family', selfName: 'alex', sharedAccountIds: [] });
  assert.equal(second.status, 201, JSON.stringify(second.body));
  assert.deepEqual(second.body.pools.map((pool) => pool.name), ['Work', 'Family']);
  const work = first.body.poolId;
  const family = second.body.poolId;
  const workInvite = (await api('alex', 'post', `/pools/${work}/invites`)).body.link;
  const familyInvite = (await api('alex', 'post', `/pools/${family}/invites`)).body.link;
  const inWork = await api('bea', 'post', '/pools/join', { link: workInvite, selfName: 'bea', sharedAccountIds: [people.bea.account] });
  const inFamily = await api('bea', 'post', '/pools/join', { link: familyInvite, selfName: 'bea', sharedAccountIds: [] });
  assert.equal(inWork.status, 200, JSON.stringify(inWork.body));
  assert.equal(inFamily.status, 200, JSON.stringify(inFamily.body));
  const twice = await api('bea', 'post', '/pools/preview', { link: (await api('alex', 'post', `/pools/${work}/invites`)).body.link });
  assert.equal(twice.body.alreadyMember, true, 'the preview says Bea is already in that pool');
  // Bea shares her connection in Work only: Alex reaches it through Work, not Family.
  const view = (await api('alex', 'get', '/pools')).body.pools;
  assert.deepEqual(view.map((pool) => pool.partners.map((partner) => partner.state)), [['online'], ['online']]);
  const models = (await api('alex', 'get', '/models?all=1')).body.models.filter((model) => model.providerSlug.startsWith('pool-'));
  assert.equal(models.filter((model) => model.id.endsWith('/ok-bea')).length, 1, `only the Work link lists Bea's model: ${models.map((model) => model.id)}`);
  // Pausing Family leaves Work running.
  await api('alex', 'put', `/pools/${family}/paused`, { paused: true });
  const workModel = models.find((model) => model.id.endsWith('/ok-bea')).id;
  assert.equal((await chat('alex', 'pool', workModel)).status, 200);
  for (const id of [work, family]) await api('alex', 'post', `/pools/${id}/leave`);
  assert.deepEqual((await api('bea', 'get', '/pools')).body.pools, [], 'closing both pools reached Bea');
});

test('an invite link opens a page that sends the invited person to their own NeoRouter', async () => {
  const created = await api('alex', 'post', '/pools', { name: 'Quiz <Night>', selfName: 'alex', sharedAccountIds: [] });
  const link = (await api('alex', 'post', `/pools/${created.body.poolId}/invites`)).body.link;
  const url = new URL(link);
  assert.equal(url.pathname, '/pool/join');
  const page = await request(base).get(`${url.pathname}${url.search}`).set('Accept-Language', 'en-US');
  assert.equal(page.status, 200);
  assert.match(page.text, /<meta property="og:title" content="Invite to the pool &quot;Quiz &lt;Night&gt;&quot;">/, 'the pool name is in the link preview, escaped');
  assert.match(page.text, /join-pool=/, 'it forwards to the own NeoRouter with the invite');
  const german = await request(base).get(`${url.pathname}${url.search}&lang=de`);
  assert.match(german.text, /Einladung in den Pool/);
  const invalid = await request(base).get('/pool/join?pool=' + 'x'.repeat(32));
  assert.match(invalid.text, /no longer valid|gilt nicht mehr/);
  // The page's link still works for joining directly in the app.
  const preview = await api('bea', 'post', '/pools/preview', { link });
  assert.equal(preview.body.pool.name, 'Quiz <Night>');
  await api('alex', 'post', `/pools/${created.body.poolId}/leave`);
});
