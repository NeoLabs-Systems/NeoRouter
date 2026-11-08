'use strict';

// Keeps one installed model running while the admin wants it on: starts it,
// watches it, restarts it after a crash or when it stops answering (waiting a
// little longer each time), and gives up only after repeated failures, at
// which point the service may repair it. States:
//   stopped · starting · running · restarting · failed
const { EventEmitter } = require('node:events');
const engines = require('./engines');
const { createLogger } = require('../../utils/logger');

const logger = createLogger('local-models');

const BACKOFF_MS = [2000, 5000, 15000, 30000, 60000];
// Running this long without trouble forgets earlier failures.
const STABLE_MS = 10 * 60_000;
const HEALTH_EVERY_MS = 30_000;
const UNHEALTHY_LIMIT = 3;

class Runner extends EventEmitter {
  /** context: () => ({ entry, files, hardware }) for each start. */
  constructor(id, context, { backoffMs = BACKOFF_MS } = {}) {
    super();
    this.backoffMs = backoffMs;
    this.id = id;
    this.context = context;
    this.state = 'stopped';
    this.problem = null;
    this.instance = null;
    this.failures = 0;
    this.wanted = false;
    this.startedAt = null;
    this.timer = null;
    this.health = null;
    this.unhealthy = 0;
    this.abort = null;
  }

  set(state, problem = this.problem) {
    const changed = state !== this.state || problem !== this.problem;
    this.state = state;
    this.problem = problem;
    if (changed) this.emit('change', this);
  }

  start() {
    this.wanted = true;
    this.failures = 0;
    if (['starting', 'running'].includes(this.state)) return;
    clearTimeout(this.timer);
    this.launch();
  }

  async launch() {
    if (!this.wanted) return;
    this.set(this.failures ? 'restarting' : 'starting', this.failures ? this.problem : null);
    this.abort = new AbortController();
    let context;
    try {
      context = await this.context();
      const instance = await engines.engineFor(context.entry.engine).start({ ...context, signal: this.abort.signal });
      if (!this.wanted) {
        await instance.proc.stop();
        return;
      }
      this.instance = instance;
      this.startedAt = Date.now();
      instance.proc.once('exit', (info) => this.exited(instance, info));
      this.watch(instance);
      this.set('running', null);
      logger.info('Local model running', { model: this.id, gpu: instance.gpu });
    } catch (error) {
      if (!this.wanted) return;
      logger.warn('Local model failed to start', { model: this.id, error: error.message });
      this.failed({ code: error.code || 'start_failed', message: error.message, output: error.output || null });
    }
  }

  watch(instance) {
    clearInterval(this.health);
    this.unhealthy = 0;
    this.health = setInterval(async () => {
      if (this.instance !== instance) return;
      if (Date.now() - this.startedAt > STABLE_MS) this.failures = 0;
      if (await instance.health()) {
        this.unhealthy = 0;
        return;
      }
      this.unhealthy += 1;
      if (this.unhealthy >= UNHEALTHY_LIMIT) {
        logger.warn('Local model stopped answering; restarting', { model: this.id });
        this.problem = { code: 'not_answering', message: 'The model stopped answering.' };
        instance.proc.stop();
      }
    }, HEALTH_EVERY_MS);
    this.health.unref();
  }

  exited(instance, { code, signal }) {
    if (this.instance !== instance) return;
    clearInterval(this.health);
    this.instance = null;
    if (!this.wanted) return this.set('stopped', null);
    logger.warn('Local model exited unexpectedly', { model: this.id, code, signal });
    this.failed(this.problem?.code === 'not_answering' ? this.problem : { code: 'crashed', message: `The model stopped unexpectedly${signal ? ` (${signal})` : code !== null ? ` (exit ${code})` : ''}.` });
  }

  failed(problem) {
    this.failures += 1;
    if (this.failures >= this.backoffMs.length) {
      this.wanted = false;
      this.set('failed', problem);
      this.emit('gave-up', this);
      return;
    }
    this.set('restarting', problem);
    this.timer = setTimeout(() => this.launch(), this.backoffMs[this.failures - 1]);
    this.timer.unref();
  }

  async stop() {
    this.wanted = false;
    clearTimeout(this.timer);
    clearInterval(this.health);
    this.abort?.abort();
    const instance = this.instance;
    this.instance = null;
    if (instance) await instance.proc.stop();
    this.set('stopped', null);
  }

  // A start refused before it began (e.g. not enough memory).
  refuse(problem) {
    this.wanted = false;
    this.set('failed', problem);
  }
}

module.exports = { Runner, BACKOFF_MS };
