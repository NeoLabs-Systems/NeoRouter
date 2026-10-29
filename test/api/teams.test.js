'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');
const { createMockProvider } = require('../support/mock_provider');

let app;
let mock;
const users = {};

function api(who, method, path, body, space) {
  let call = request(app)[method](`/api/v1${path}`).set('Authorization', `Bearer ${users[who].token}`);
  if (space) call = call.set('X-NeoRouter-Space', users[space].id);
  return body ? call.send(body) : call;
}

async function register(name) {
  const response = await request(app).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: 'correct-horse-battery' });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  users[name] = { id: response.body.user.id, token: response.body.session.token };
}

async function invite(permissions, extra = {}) {
  const response = await api('owner', 'post', '/teams/invites', { label: 'test', permissions, ...extra });
  assert.equal(response.status, 201, JSON.stringify(response.body));
  return response.body.link;
}

test.before(async () => {
  app = createApp();
  mock = await createMockProvider({ models: ['ok-alpha'] });
  await register('owner');
  await api('owner', 'patch', '/admin/settings', { registrationOpen: true });
  await register('member');
  await register('outsider');
  await api('owner', 'patch', '/admin/settings', { registrationOpen: false });
  const added = await api('owner', 'post', '/providers/accounts', { definitionId: 'custom_openai', slug: 'mock', baseUrl: mock.url, secret: 'good-key', billingMode: 'free_tier' });
  assert.equal(added.status, 201, JSON.stringify(added.body));
});

test.after(async () => {
  await mock.close();
});

test('invite links store only a hash and can be previewed', async () => {
  const link = await invite(['keys.create']);
  assert.match(link, /\/app\/\?invite=[A-Za-z0-9_-]{32}$/);
  const token = new URL(link).searchParams.get('invite');
  const db = require('../../server/db/database').getDatabase();
  assert.equal(db.prepare('SELECT COUNT(*) n FROM team_invites WHERE token_hash = ?').get(token).n, 0);
  const preview = await api('member', 'post', '/teams/invites/preview', { link });
  assert.equal(preview.status, 200);
  assert.equal(preview.body.owner.username, 'owner');
  assert.deepEqual(preview.body.permissions, ['keys.view', 'keys.create'], 'create brings view with it');
  const own = await api('owner', 'post', '/teams/invites/preview', { link });
  assert.equal(own.body.error.code, 'INVITE_SELF');
});

test('a single-use link works once and a revoked link not at all', async () => {
  const link = await invite(['keys.view'], { singleUse: true });
  const joined = await api('member', 'post', '/teams/invites/redeem', { link });
  assert.equal(joined.status, 200, JSON.stringify(joined.body));
  assert.equal(joined.body.memberOf.owner.username, 'owner');
  const again = await api('outsider', 'post', '/teams/invites/redeem', { link });
  assert.equal(again.body.error.code, 'INVITE_USED');
  const second = await invite(['keys.view']);
  const already = await api('member', 'post', '/teams/invites/preview', { link: second });
  assert.equal(already.body.error.code, 'ALREADY_MEMBER');
  const summary = await api('owner', 'get', '/teams');
  const open = summary.body.invites.find((item) => item.status === 'active');
  await api('owner', 'delete', `/teams/invites/${open.id}`);
  const revoked = await api('outsider', 'post', '/teams/invites/redeem', { link: second });
  assert.equal(revoked.body.error.code, 'INVITE_REVOKED');
});

test('members only get what the owner allowed', async () => {
  const listed = await api('member', 'get', '/keys', null, 'owner');
  assert.equal(listed.status, 200);
  const denied = await api('member', 'post', '/keys', { name: 'nope' }, 'owner');
  assert.equal(denied.status, 403);
  assert.equal(denied.body.error.code, 'PERMISSION_DENIED');
  const providers = await api('member', 'get', '/providers/accounts', null, 'owner');
  assert.equal(providers.status, 403);
  const teamPage = await api('member', 'get', '/teams', null, 'owner');
  assert.equal(teamPage.body.error.code, 'OWN_SPACE_ONLY');
});

test('a key a member creates belongs to the owner and shows who made it', async () => {
  const owner = await api('owner', 'put', `/teams/members/${users.member.id}/permissions`, { permissions: ['keys.create', 'providers.view'] });
  assert.deepEqual(owner.body.members[0].permissions, ['keys.view', 'keys.create', 'providers.view']);
  const created = await api('member', 'post', '/keys', { name: 'from member', mode: 'free_only' }, 'owner');
  assert.equal(created.status, 201, JSON.stringify(created.body));
  assert.equal(created.body.key.createdBy.username, 'member');
  const ownerKeys = await api('owner', 'get', '/keys');
  assert.ok(ownerKeys.body.keys.some((key) => key.id === created.body.key.id && key.createdBy.username === 'member'));
  const memberKeys = await api('member', 'get', '/keys');
  assert.equal(memberKeys.body.keys.length, 0, 'the member’s own space stays empty');
  const chat = await request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${created.body.secret}`)
    .send({ model: 'mock/ok-alpha', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(chat.status, 200, JSON.stringify(chat.body));
  const logs = await api('owner', 'get', '/usage/logs');
  assert.equal(logs.body.logs.length, 1, 'usage counts for the owner');
});

test('provider secrets never reach a member', async () => {
  const response = await api('member', 'get', '/providers/accounts', null, 'owner');
  assert.equal(response.status, 200);
  assert.ok(!JSON.stringify(response.body).includes('good-key'));
});

test('a space you are not in looks like it does not exist', async () => {
  const response = await api('outsider', 'get', '/keys', null, 'owner');
  assert.equal(response.status, 404);
  assert.equal(response.body.error.code, 'SPACE_NOT_FOUND');
});

test('releasing a member ends their access; leaving works too', async () => {
  const released = await api('owner', 'delete', `/teams/members/${users.member.id}`);
  assert.equal(released.body.members.length, 0);
  const gone = await api('member', 'get', '/keys', null, 'owner');
  assert.equal(gone.status, 404);
  const link = await invite(['activity.view']);
  await api('outsider', 'post', '/teams/invites/redeem', { link });
  const left = await api('outsider', 'post', '/teams/leave');
  assert.equal(left.body.memberOf, null);
  const audit = await api('owner', 'get', '/admin/team-audit');
  assert.ok(audit.body.events.some((event) => event.action === 'member.leave'));
});

test('an account can hold at most 25 open invite links', async () => {
  const summary = await api('owner', 'get', '/teams');
  const open = summary.body.invites.filter((item) => item.status === 'active').length;
  for (let index = open; index < 25; index += 1) await invite(['keys.view']);
  const blocked = await api('owner', 'post', '/teams/invites', { permissions: ['keys.view'] });
  assert.equal(blocked.body.error.code, 'INVITE_LIMIT');
});
