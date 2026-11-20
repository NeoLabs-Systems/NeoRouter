'use strict';

require('../support/env');
const test = require('node:test');
const assert = require('node:assert/strict');
const registry = require('../../server/providers/registry');
const { migrate } = require('../../server/db/migrate');
const { getDatabase } = require('../../server/db/database');
const accounts = require('../../server/services/providers/account_service');
const catalog = require('../../server/services/catalog/catalog_service');
const metadata = require('../../server/services/catalog/metadata_service');
const { providerKey } = require('../../server/services/catalog/metadata/models_dev');

// Kourier's listing is bare — id, object, created, owned_by, no limits
// (api.kourier.sh/v1/models, 2026-11-20). The limits the provider documents
// (docs.kourier.sh/docs/models) live in the definition as models.facts.
const BARE = [{ id: 'deepseek-v4.1-flash', object: 'model', created: 1794725295, owned_by: 'kourier' }];

async function sync(listing, { user, account }) {
  migrate();
  const db = getDatabase();
  db.prepare('INSERT INTO users (id, username, email, password_hash, role) VALUES (?, ?, ?, ?, ?)')
    .run(user, user, `${user}@example.com`, 'x', 'admin');
  db.prepare('INSERT INTO provider_accounts (id, user_id, definition_id, slug, label, billing_mode, prepaid) VALUES (?, ?, ?, ?, ?, ?, ?)')
    .run(account, user, 'kourier', 'kourier', 'Kourier', 'paid', 3);
  const definition = registry.get('kourier');
  const adapter = registry.createAdapter(definition, { secret: 'key', baseUrl: definition.baseUrl, options: {} });
  adapter.http = async () => ({ data: { object: 'list', data: listing } });
  const original = accounts.adapterFor;
  accounts.adapterFor = () => adapter;
  try {
    await catalog.syncAccount(account);
    return db.prepare('SELECT context_window, max_output FROM models WHERE account_id = ?').get(account);
  } finally {
    accounts.adapterFor = original;
  }
}

test('a bare listing gets the limits the provider documents', async () => {
  const row = await sync(BARE, { user: 'u1', account: 'k1' });
  assert.deepEqual(row, { context_window: 262144, max_output: 16384 });
});

test('limits the listing itself reports win over the documented ones', async () => {
  const row = await sync([{ ...BARE[0], context_length: 500000, max_output_tokens: 32000 }], { user: 'u2', account: 'k2' });
  assert.deepEqual(row, { context_window: 500000, max_output: 32000 });
});

test("the definition's facts win over models.dev facts for the same provider", async () => {
  metadata.resetForTests({ modelsDev: { [providerKey('kourier')]: { 'deepseek-v4.1-flash': { contextWindow: 1000000, maxOutput: 128000 } } } });
  try {
    const row = await sync(BARE, { user: 'u3', account: 'k3' });
    assert.deepEqual(row, { context_window: 262144, max_output: 16384 });
  } finally {
    metadata.resetForTests({});
  }
});
