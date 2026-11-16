'use strict';

// Throwaway compatibility experiment for issue #10, not a supported driver.
const { Database } = require('bun:sqlite');
class BunDatabase extends Database {
  constructor(filename) {
    super(filename, { strict: true });
    this.open = true;
  }
  pragma(sql) { return super.prepare(`PRAGMA ${sql}`).all(); }
  prepare(sql) {
    const statement = super.prepare(sql);
    const get = statement.get.bind(statement);
    statement.get = (...args) => get(...args) ?? undefined;
    return statement;
  }
  close() {
    super.close(true);
    this.open = false;
  }
}
module.exports = BunDatabase;
