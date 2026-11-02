'use strict';

const ui = require('./ui');

// What a failed command means to the person who ran it, by error code. The
// error's own message says what happened; this adds the headline and the
// next thing to try.
const GUIDANCE = Object.freeze({
  CLI_INPUT: { title: "That command didn't work", next: 'neorouter help', plain: true },
  SERVER_NOT_READY: { title: "NeoRouter started but isn't answering", next: 'neorouter logs' },
});

// A mistake in what was typed, not a problem with NeoRouter.
function inputError(message) {
  const error = new Error(message);
  error.code = 'CLI_INPUT';
  return error;
}

function printError(error, { details = false } = {}) {
  const write = (line = '') => process.stderr.write(`${line}\n`);
  if (error?.code === 'CLI_CANCELLED') {
    write(`\n  ${ui.paint.muted('Cancelled.')}`);
    return;
  }
  const guidance = GUIDANCE[error?.code] || { title: 'Something went wrong', next: 'neorouter doctor' };
  write('');
  write(`  ${ui.paint.error('✕')} ${ui.paint.bold(guidance.title)}`);
  if (error?.message) write(String(error.message).split('\n').map((line) => `    ${line}`).join('\n'));
  write('');
  write(`  ${ui.paint.muted('Try')} ${ui.paint.command(guidance.next)}${ui.paint.muted(
    guidance.plain ? '' : '  ·  still stuck? neorouter repair tries the usual fixes',
  )}`);
  if (details) {
    write('');
    write(`  ${ui.paint.muted(`Technical details${error?.code ? ` (${error.code})` : ''}:`)}`);
    write(ui.paint.muted(String(error?.stack || error).split('\n').map((line) => `    ${line}`).join('\n')));
  } else if (!guidance.plain) {
    write(`  ${ui.paint.muted('Technical details: add --details')}`);
  }
  write('');
}

module.exports = {
  inputError,
  printError,
};
