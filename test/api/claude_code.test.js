'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

// A stand-in for the `claude` binary: reports the token, configuration folder
// and server credentials it was started with. The env must be set before the
// app loads.
const bin = fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-fake-claude-'));
const fake = path.join(bin, 'claude');
fs.writeFileSync(fake, `#!/bin/sh
if [ "$1" = "auth" ]; then
  case "$CLAUDE_CODE_OAUTH_TOKEN" in good-*) echo '{"loggedIn":true,"authMethod":"oauth_token"}'; exit 0;; esac
  echo '{"loggedIn":false}'; exit 1
fi
cat > /dev/null
TEXT="token=$CLAUDE_CODE_OAUTH_TOKEN dir=$CLAUDE_CONFIG_DIR apikey=\${ANTHROPIC_API_KEY:-none} cwd=$(pwd)"
printf '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"%s"}}}\\n' "$TEXT"
printf '{"type":"result","subtype":"success","is_error":false,"result":"%s","usage":{"input_tokens":5,"output_tokens":3}}\\n' "$TEXT"
`, { mode: 0o755 });
process.env.NEOROUTER_CLAUDE_PATH = fake;
// A server-wide credential that must never reach a user's Claude run.
process.env.ANTHROPIC_API_KEY = 'server-wide-secret';
require('../support/env');

const test = require('node:test');
const assert = require('node:assert/strict');
const request = require('supertest');
const { createApp } = require('../../server/app');

let app;
const users = {};

async function register(name, admin) {
  if (admin) await request(app).patch('/api/v1/admin/settings').set('Authorization', `Bearer ${admin}`).send({ registrationOpen: true });
  const session = (await request(app).post('/api/v1/auth/register').send({ username: name, email: `${name}@example.test`, password: 'correct-horse-battery' })).body.session.token;
  const api = (method, url) => request(app)[method](`/api/v1${url}`).set('Authorization', `Bearer ${session}`);
  const added = await api('post', '/providers/accounts').send({ definitionId: 'claude_code', secret: `good-${name}`, billingMode: 'plan' });
  assert.equal(added.status, 201, JSON.stringify(added.body));
  const secret = (await api('post', '/keys').send({ name: 'k', allowFree: true, allowPlan: true })).body.secret;
  return { session, api, secret, accountId: added.body.account.id };
}

test.before(async () => {
  app = createApp();
  users.alice = await register('alice');
  users.bob = await register('bob', users.alice.session);
});

test.after(() => fs.rmSync(bin, { recursive: true, force: true }));

const ask = (user) => request(app).post('/v1/chat/completions').set('Authorization', `Bearer ${user.secret}`)
  .send({ model: 'auto', messages: [{ role: 'user', content: 'hi' }] });

test('every user runs Claude Code with their own token and folder, never server credentials', async () => {
  const alice = (await ask(users.alice)).body.choices[0].message.content;
  const bob = (await ask(users.bob)).body.choices[0].message.content;
  assert.match(alice, /token=good-alice /);
  assert.match(bob, /token=good-bob /);
  const dir = (text) => /dir=(\S+)/.exec(text)[1];
  assert.notEqual(dir(alice), dir(bob));
  assert.ok(dir(alice).endsWith(users.alice.accountId));
  assert.equal((fs.statSync(dir(alice)).mode & 0o777), 0o700, 'the folder is private to the server user');
  assert.match(alice, /apikey=none/, 'a server-wide ANTHROPIC_API_KEY is stripped');
  assert.doesNotMatch(alice, /cwd=\/Users\/neo\/NeoRouter/, 'runs in an empty folder, not the app folder');
});

test('a rejected token is not saved', async () => {
  const bad = await users.alice.api('post', '/providers/accounts').send({ definitionId: 'claude_code', secret: 'bad-token', billingMode: 'plan' });
  assert.equal(bad.status, 422);
});

test('removing the connection removes its Claude Code folder', async () => {
  const text = (await ask(users.bob)).body.choices[0].message.content;
  const dir = /dir=(\S+)/.exec(text)[1];
  assert.equal((await users.bob.api('delete', `/providers/accounts/${users.bob.accountId}`)).status, 204);
  await new Promise((resolve) => setTimeout(resolve, 100));
  assert.equal(fs.existsSync(dir), false);
});
