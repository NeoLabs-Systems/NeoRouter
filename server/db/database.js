'use strict';

const Database = require('better-sqlite3');
const { ensureRuntimeDirs } = require('../../runtime/paths');

let connection;

function openDatabase(filename) {
  const database = new Database(filename);
  database.pragma('journal_mode = WAL');
  database.pragma('foreign_keys = ON');
  database.pragma('busy_timeout = 10000');
  database.pragma('synchronous = FULL');
  database.pragma('trusted_schema = OFF');
  return database;
}

function getDatabase() {
  if (!connection || !connection.open) connection = openDatabase(ensureRuntimeDirs().database);
  return connection;
}

function closeDatabase() {
  if (connection && connection.open) connection.close();
  connection = undefined;
}

module.exports = { getDatabase, closeDatabase, openDatabase };
