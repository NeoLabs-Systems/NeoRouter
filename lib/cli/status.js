'use strict';

const ui = require('./ui');

const LABEL_WIDTH = 24;

// Plain-language health checks from the facts `neorouter status` gathers.
// Each check is { tone, label, detail, fix? } where fix is a command to run.
function statusChecks(facts) {
  const checks = [];
  checks.push(facts.running
    ? { id: 'server', tone: 'ok', label: 'NeoRouter', detail: `Running at ${facts.url}` }
    : { id: 'server', tone: 'error', label: 'NeoRouter', detail: 'Not running', fix: 'neorouter start' });

  if (facts.runningVersion && facts.runningVersion !== facts.version) {
    checks.push({
      tone: 'warn',
      label: 'Restart needed',
      detail: `${facts.version} is installed, but ${facts.runningVersion} is still running`,
      fix: 'neorouter restart',
    });
  }

  if (facts.inContainer) {
    checks.push({ tone: 'ok', label: 'Starts automatically', detail: 'The container looks after it' });
  } else {
    checks.push(facts.serviceActive
      ? { tone: 'ok', label: 'Starts automatically', detail: 'Turns on when your computer does' }
      : { tone: 'warn', label: 'Starts automatically', detail: 'Not set up on this computer', fix: 'neorouter install' });
  }

  checks.push(facts.configPresent
    ? { tone: 'ok', label: 'Settings', detail: 'Saved' }
    : { tone: 'error', label: 'Settings', detail: 'Not set up yet', fix: 'neorouter setup' });

  checks.push(facts.webClientPresent
    ? { tone: 'ok', label: 'App in your browser', detail: 'Installed' }
    : { tone: 'error', label: 'App in your browser', detail: 'Missing', fix: 'neorouter rebuild-web' });

  if (facts.update) {
    checks.push({
      id: 'version',
      tone: 'warn',
      label: `Version ${facts.version}`,
      detail: `A newer version${facts.update.version ? ` (${facts.update.version})` : ''} is ready`,
      fix: 'neorouter update',
    });
  } else {
    checks.push({ id: 'version', tone: 'ok', label: `Version ${facts.version}`, detail: facts.updateChecked ? 'Up to date' : '' });
  }
  return checks;
}

function needsAttention(check) {
  return check.tone === 'warn' || check.tone === 'error';
}

function headline(facts, checks) {
  if (!facts.running) return { tone: 'error', text: "NeoRouter isn't running" };
  if (checks.some((check) => check.tone === 'error')) return { tone: 'error', text: 'NeoRouter needs attention' };
  const suggestions = checks.filter((check) => check.tone === 'warn').length;
  if (suggestions > 0) {
    return { tone: 'warn', text: `Running — ${suggestions === 1 ? '1 thing needs' : `${suggestions} things need`} a look` };
  }
  return { tone: 'ok', text: "Everything's working" };
}

// Label and detail side by side, or the detail under its label when the
// window is narrow. A failed check says how to fix it.
function printChecks(checks) {
  const narrow = ui.isNarrow();
  const indent = narrow ? 4 : LABEL_WIDTH + 6;
  ui.blank();
  for (const check of checks) {
    if (narrow) {
      ui.item(check.tone, ui.paint.bold(check.label));
      if (check.detail) ui.paragraph(check.detail, { indent, style: ui.paint.muted });
    } else {
      ui.item(check.tone, ui.paint.bold(check.label), check.detail, LABEL_WIDTH);
    }
    if (check.fix) ui.print(`${' '.repeat(indent)}${ui.paint.faint('run')} ${ui.paint.command(check.fix)}`);
  }
}

function printStatus(facts, { details = false } = {}) {
  const checks = statusChecks(facts);
  const top = headline(facts, checks);
  ui.blank();
  ui.print(`  ${ui.paint.tone(top.tone, ui.TONES[top.tone].mark)} ${ui.paint.bold(top.text)}`);
  // The headline already says whether NeoRouter runs.
  if (facts.running) ui.print(`    ${ui.paint.accent(facts.url)}`);
  else ui.print(`    ${ui.paint.faint('run')} ${ui.paint.command('neorouter start')}`);
  const shown = details ? checks : checks.filter((check) => check.id !== 'server' && needsAttention(check));
  if (shown.length > 0) printChecks(shown);
  if (details) {
    ui.section('Details');
    const rows = [
      ['Installed in', facts.appDir],
      ['Version', facts.versionLabel],
      ['Running version', facts.runningVersion || 'not running'],
      ['Release channel', facts.channelSummary],
      ['Settings file', facts.envFile],
      ['Port', String(facts.port)],
    ];
    for (const [label, value] of rows) ui.print(`  ${ui.padEnd(ui.paint.faint(label), 18)}${value}`);
  }
  ui.blank();
}

module.exports = {
  needsAttention,
  printChecks,
  printStatus,
  statusChecks,
};
