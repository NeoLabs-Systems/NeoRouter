'use strict';

// The local_models table: which models are installed or downloading, and
// whether the admin wants them running.
const { getDatabase } = require('../../db/database');

function toModel(row) {
  return {
    id: row.id,
    status: row.status,
    enabled: Boolean(row.enabled),
    installedBy: row.installed_by_user_id,
    createdAt: row.created_at,
  };
}

function list() {
  return getDatabase().prepare('SELECT * FROM local_models ORDER BY created_at').all().map(toModel);
}

function get(id) {
  const row = getDatabase().prepare('SELECT * FROM local_models WHERE id = ?').get(id);
  return row ? toModel(row) : null;
}

function add(id, userId) {
  getDatabase().prepare("INSERT INTO local_models (id, status, installed_by_user_id) VALUES (?, 'downloading', ?)").run(id, userId);
}

function update(id, fields) {
  const columns = { status: 'status', enabled: 'enabled' };
  const sets = [];
  const values = [];
  for (const [field, column] of Object.entries(columns)) {
    if (fields[field] === undefined) continue;
    sets.push(`${column} = ?`);
    values.push(field === 'enabled' ? (fields.enabled ? 1 : 0) : fields[field]);
  }
  if (!sets.length) return;
  getDatabase().prepare(`UPDATE local_models SET ${sets.join(', ')}, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`).run(...values, id);
}

function remove(id) {
  getDatabase().prepare('DELETE FROM local_models WHERE id = ?').run(id);
}

module.exports = { list, get, add, update, remove };
