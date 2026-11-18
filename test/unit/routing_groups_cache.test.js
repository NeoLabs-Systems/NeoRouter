'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const groups = require('../../server/services/routing/groups');
const { RoutingPlan, parseModel, routeAllowedByKey } = require('../../server/services/routing/router');
const affinity = require('../../server/services/routing/cache_affinity');
const usage = require('../../server/services/usage/usage_service');
const { costOf } = require('../../server/services/budget/budget_service');
const { normalizeRequest, normalizeUsage } = require('../../server/capabilities/chat/canonical');
const anthropic = require('../../server/capabilities/chat/translators/anthropic_messages');
const keys = require('../../server/services/router_keys/router_key_service');

migrate();
const db = getDatabase();
db.prepare("INSERT INTO users (id, username, email, password_hash, role) VALUES ('u', 'u', 'u@test.local', 'x', 'admin'), ('v', 'v', 'v@test.local', 'x', 'user')").run();
for (const id of ['a', 'b']) {
  db.prepare("INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode) VALUES (?, 'u', 'openai', ?, ?, 'paid')").run(id, id, id);
  db.prepare("INSERT INTO models (account_id, model_id, canonical_id, price_in, price_out, available) VALUES (?, 'm', 'm', 1, 2, 1)").run(id);
}
const key = keys.authenticate(keys.create('u', { name: 'test', mode: 'both' }).secret);
const request = { messages: [{ role: 'user', content: 'hello' }] };
const makePlan = (model, extra = {}) => new RoutingPlan({ userId: 'u', key, request, needs: {}, inputTokens: 1000, requestedModel: model, random: () => 0, ...extra });

test('groups isolate members, preserve key policy, fall back and reject cross-space access/cycles', () => {
  const fallback = groups.save('u', { name: 'fallback', accounts: ['b'], allowMetered: true });
  const primary = groups.save('u', { name: 'primary', accounts: ['a'], fallback: 'fallback', allowMetered: true });
  const plan = makePlan('pool:primary');
  assert.equal(plan.next().entry.accountId, 'a');
  assert.equal(plan.next().entry.accountId, 'b');
  assert.equal(plan.group.name, 'fallback');
  assert.equal(plan.next(), null);
  const load = require('../../server/services/routing/load_tracker');
  const limits = require('../../server/services/quota/account_limits');
  limits.set('a', { concurrent: 1 });
  limits.set('b', { concurrent: 1 });
  const releaseA = load.lease(plan.entries.find((entry) => entry.accountId === 'a'));
  const releaseB = load.lease(plan.entries.find((entry) => entry.accountId === 'b'));
  const waiting = makePlan('pool:primary');
  assert.equal(waiting.next(), null);
  assert.ok(waiting.freesUpAt() > Date.now());
  releaseA();
  assert.equal(waiting.next().entry.accountId, 'a', 'primary capacity is reconsidered after fallback was full');
  releaseB();
  limits.set('a', { concurrent: null });
  limits.set('b', { concurrent: null });

  assert.throws(() => groups.save('u', { ...fallback, fallback: 'primary' }, fallback.id), { code: 'GROUP_CYCLE' });
  assert.throws(() => groups.save('v', { name: 'other', accounts: ['a'] }), { code: 'UNKNOWN_ACCOUNT' });
  assert.equal(groups.resolve('v', primary.name), null);
  const limited = { ...key, allowed_models_json: JSON.stringify(['pool:primary', 'a/*']) };
  assert.equal(routeAllowedByKey('pool:primary', limited), true);
  assert.equal(routeAllowedByKey('pool:fallback', limited), false);
  const restricted = makePlan('pool:primary', { key: limited });
  assert.equal(restricted.next().entry.accountId, 'a');
  assert.equal(restricted.next(), null, 'fallback never widens allowed models');
  const byKey = makePlan('auto', { key: { ...key, routing_group: primary.name } });
  assert.equal(byKey.next().entry.accountId, 'a');
  assert.equal(makePlan('auto', { routingGroup: 'fallback' }).next().entry.accountId, 'b');
  assert.equal(parseModel('pool:primary').auto, true);
  const unavailable = groups.save('u', { name: 'unavailable', models: ['absent/*'], allowMetered: true, allowOutside: true });
  const rescue = makePlan(`pool:${unavailable.name}`);
  assert.equal(rescue.next(), null);
  assert.equal(rescue.enableLastResort(), true);
  assert.ok(rescue.next(), 'last-resort pass can use other models without bypassing key policy');
});

test('cache usage is priced, recorded and learned without retaining conversation text', () => {
  const entry = makePlan('pool:primary').next().entry;
  const tokens = normalizeUsage({ input_tokens: 10, cache_read_input_tokens: 80, cache_creation_input_tokens: 10, output_tokens: 5 });
  assert.equal(tokens.prompt_tokens, 100);
  assert.equal(costOf({ ...entry, priceIn: 2, priceOut: 4, priceCachedIn: 0.2, priceCacheWrite: 3 }, tokens), 86 / 1e6);
  assert.equal(require('../../server/services/budget/budget_service').estimateMaxCost({ ...entry, priceIn: 2, priceOut: 4, priceCacheWrite: 3 }, 100, 5), 320 / 1e6);
  assert.equal(costOf({ ...entry, priceIn: 2, priceOut: 4 }, tokens), 220 / 1e6, 'unknown cache prices use full input price');
  affinity.remember(key.id, 'conversation', entry);
  assert.equal(affinity.affinity(key.id, 'conversation'), entry.ref);
  assert.equal(affinity.affinity('other-key', 'conversation'), null);
  assert.equal(affinity.bonus(entry, 0, 10000), 0);
  assert.equal(makePlan('pool:primary', { sessionId: 'conversation' }).rank({ sample: false })[0].details.factors.cacheAffinity, 0);
  usage.record({ id: 'r', user_id: 'u', router_key_id: key.id, created_at: Date.now(), api_format: 'openai', requested_model: 'pool:primary', model_ref: entry.ref, model_label: entry.id, account_id: entry.accountId, status: 'success', http_status: 200, input_tokens: 100, cached_input_tokens: 80, output_tokens: 5, cost_usd: 0, is_free: 0, latency_ms: 100, attempts: 1, stream: 0, trail_json: '[]', routing_group: 'primary' });
  const ranked = makePlan('pool:primary', { sessionId: 'conversation' }).rank({ sample: false });
  assert.ok(ranked[0].details.factors.cacheAffinity > 0);
  assert.equal(usage.logs('u')[0].cachedInputTokens, 80);
  const analytics = usage.analytics('u');
  assert.equal(analytics.byProvider[0].cacheHitRate, 1);
  assert.equal(analytics.byRoutingGroup[0].key, 'primary');
});

test('Anthropic cache breakpoints and usage survive JSON and streaming translation', async () => {
  const canonical = normalizeRequest({ ...request, prompt_cache_key: 'session-cache' });
  assert.equal(canonical.prompt_cache_key, 'session-cache');
  assert.equal(require('../../server/providers/adapters/codex').buildBody(canonical, 'm', '').prompt_cache_key, 'session-cache');
  const body = { system: [{ type: 'text', text: 'system', cache_control: { type: 'ephemeral', ttl: '1h' } }], tools: [{ name: 'tool', input_schema: { type: 'object' }, cache_control: { type: 'ephemeral' } }], messages: [{ role: 'user', content: 'hello' }] };
  const roundTrip = anthropic.fromCanonical(anthropic.toCanonical(body));
  assert.deepEqual(roundTrip.system, body.system);
  assert.deepEqual(roundTrip.tools[0].cache_control, body.tools[0].cache_control);
  const rawUsage = { input_tokens: 10, cache_read_input_tokens: 80, cache_creation_input_tokens: 10, output_tokens: 5 };
  const completion = anthropic.responseToCanonical({ content: [{ type: 'text', text: 'ok' }], usage: rawUsage }, 'm');
  assert.equal(completion.usage.prompt_tokens, 100);
  assert.deepEqual(anthropic.responseFromCanonical(completion, 'm').usage, rawUsage);
  async function* events() {
    yield { data: JSON.stringify({ type: 'message_start', message: { usage: rawUsage } }) };
    yield { data: JSON.stringify({ type: 'message_delta', usage: { output_tokens: 5 } }) };
    yield { data: JSON.stringify({ type: 'message_stop' }) };
  }
  const chunks = [];
  for await (const chunk of anthropic.streamToCanonical(events(), 'm')) chunks.push(chunk);
  assert.deepEqual(chunks.at(-1).usage, completion.usage);
});

test('full paid slots with nothing free in the group fall through to the fallback group before waiting', () => {
  const load = require('../../server/services/routing/load_tracker');
  const limits = require('../../server/services/quota/account_limits');
  db.prepare("INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode) VALUES ('flat', 'u', 'openai', 'flat', 'flat', 'paid'), ('spare', 'u', 'openai', 'spare', 'spare', 'paid')").run();
  for (const id of ['flat', 'spare']) db.prepare("INSERT INTO models (account_id, model_id, canonical_id, price_in, price_out, available) VALUES (?, 'm', 'm', 1, 2, 1)").run(id);
  groups.save('u', { name: 'spill', accounts: ['spare'], allowMetered: true });
  groups.save('u', { name: 'slots', accounts: ['flat'], fallback: 'spill', allowMetered: true });
  limits.set('flat', { concurrent: 1 });
  const plan = makePlan('pool:slots');
  const flat = plan.entries.find((entry) => entry.accountId === 'flat');
  for (const entry of plan.entries) if (entry.accountId === 'flat') { entry.isFlatRate = true; entry.overflowPolicy = 'auto'; }
  const release = load.lease(flat);
  try {
    const choice = plan.next();
    assert.equal(choice?.entry.accountId, 'spare');
  } finally { release(); limits.set('flat', { concurrent: null }); }
});


test('a smart alias spills at the account limit and keeps the new conversation route warm', () => {
  const load = require('../../server/services/routing/load_tracker');
  const limits = require('../../server/services/quota/account_limits');
  groups.save('u', { name: 'work', accounts: ['a', 'b'], preferredAccount: 'a', allowMetered: true });
  const first = makePlan('pool:work').next().entry;
  assert.equal(first.accountId, 'a');
  limits.set('a', { concurrent: 1 });
  const release = load.lease(first);
  try {
    const burst = makePlan('pool:work');
    for (const entry of burst.entries.filter((entry) => ['a', 'b'].includes(entry.accountId))) {
      entry.isPlan = true;
      if (entry.accountId === 'a') {
        entry.isFlatRate = true;
        entry.overflowPolicy = 'auto';
      }
    }
    const overflow = burst.next().entry;
    assert.equal(overflow.accountId, 'b');
    assert.equal(overflow.routingReason, 'preferred_busy');
    affinity.remember(burst.affinityScope, 'overflow-session', overflow);
  } finally {
    release();
    limits.set('a', { concurrent: null });
  }
  assert.equal(makePlan('pool:work', { sessionId: 'overflow-session' }).next().entry.accountId, 'b');
  assert.equal(makePlan('pool:work').next().entry.accountId, 'a');

  const strict = groups.save('u', { name: 'strict', accounts: ['a'] });
  const plan = makePlan('pool:strict');
  assert.equal(plan.next(), null, 'new groups exclude metered models by default');
  assert.equal(plan.enableLastResort(), false);
  groups.save('u', { ...strict, allowMetered: true }, strict.id);
  const limited = makePlan('pool:strict');
  assert.equal(limited.next().entry.accountId, 'a');
  assert.equal(limited.next(), null);
  assert.equal(limited.enableLastResort(), false, 'a last resort cannot escape the selected connections');
});


test('an explicit routing style takes priority over the preferred account', () => {
  const profiles = require('../../server/services/routing/profiles');
  const style = profiles.create('u', {
    name: 'Subscriptions first',
    weights: profiles.PRESETS.balanced.weights,
    constraints: { kinds: { free: 0.5, plan: 1, paid: 0.5 } },
  });
  groups.save('u', { name: 'styled-work', accounts: ['a', 'b'], preferredAccount: 'a', profile: style.id, allowMetered: true });
  const plan = makePlan('pool:styled-work');
  plan.entries.find((entry) => entry.accountId === 'b').isPlan = true;
  assert.equal(plan.next().entry.accountId, 'b');
});

test('a subagent group uses its available subscription before waiting for a busy flat-rate plan', () => {
  const load = require('../../server/services/routing/load_tracker');
  const limits = require('../../server/services/quota/account_limits');
  groups.save('u', { name: 'helpers', accounts: ['a', 'b'] });
  groups.save('u', { name: 'parent', accounts: ['a'], subagentGroup: 'helpers' });
  limits.set('a', { concurrent: 4 });
  const leases = [];
  try {
    const counts = { a: 0, b: 0 };
    for (let i = 0; i < 10; i += 1) {
      const plan = i % 2 === 0
        ? makePlan('pool:parent', { workload: 'subagent' })
        : makePlan('pool:helpers');
      for (const entry of plan.entries.filter((entry) => ['a', 'b'].includes(entry.accountId))) {
        entry.isPlan = true;
        if (entry.accountId === 'a') {
          entry.isFlatRate = true;
          entry.overflowPolicy = 'auto';
        }
      }
      const choice = plan.next().entry;
      assert.equal(choice.routingGroup, 'helpers');
      counts[choice.accountId] += 1;
      leases.push(load.lease(choice));
    }
    assert.deepEqual(counts, { a: 4, b: 6 });
  } finally {
    for (const release of leases) release();
    limits.set('a', { concurrent: null });
  }
});
