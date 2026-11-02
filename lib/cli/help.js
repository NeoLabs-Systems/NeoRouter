'use strict';

const ui = require('./ui');

const CLI = 'neorouter';

// Everyday commands, grouped by what someone is trying to do.
const GROUPS = Object.freeze([
  ['Getting started', [
    ['install', 'Get NeoRouter running on this computer'],
    ['channel', 'Choose stable or beta updates'],
  ]],
  ['Everyday', [
    ['status', 'Is everything working?'],
    ['update', 'Get the latest version — your data stays'],
    ['restart', 'Turn it off and on again'],
  ]],
  ["When something's wrong", [
    ['doctor', 'Find problems and explain them'],
    ['repair', 'Fix common problems for you'],
    ['logs', 'See what NeoRouter is doing right now'],
  ]],
  ['Your data', [
    ['backup', 'Save a copy of your settings and data'],
    ['start · stop', 'Turn NeoRouter on or off'],
    ['uninstall', 'Stop NeoRouter starting with your computer'],
  ]],
]);

// Every command and flag, for developers and admins.
const REFERENCE = Object.freeze([
  ['Setup', [
    ['install', 'Install dependencies, the web client and the background service'],
    ['setup [--channel stable|beta]', 'Prepare the database, settings and web client only'],
  ]],
  ['Running', [
    ['start · stop · restart', 'Control the background service'],
    ['serve', 'Run the server in the foreground'],
    ['status --details', 'Health plus install folder, versions and port'],
    ['doctor', 'Read-only diagnostics'],
    ['logs', 'Follow the server logs'],
    ['backup', 'Snapshot settings and data'],
    ['repair', 'Reinstall dependencies and the web client, and restart'],
    ['uninstall', 'Remove the background service (keeps data)'],
  ]],
  ['Updates', [
    ['update [stable|beta]', 'Update, optionally switching channel'],
    ['update --notes', "Show what's new without updating"],
    ['channel [stable|beta]', 'Show or switch the release channel'],
    ['version', 'Print the installed version'],
    ['rebuild-web', 'Rebuild the bundled Flutter web client'],
  ]],
  ['Accounts & providers', [
    ['reset-password <user> <password>', 'Reset an account password'],
    ['reset-2fa <user>', 'Remove two-factor authentication from an account'],
    ['providers import <file> --user <user>', 'Add provider keys from a JSON file'],
  ]],
  ['Configuration', [
    ['env list|get|set|unset', 'Read or change settings (secrets masked)'],
  ]],
]);

function printGroupColumns(groups) {
  const columns = groups.map(([name, commands]) => [
    ui.paint.accent(ui.paint.bold(name)),
    ...commands.flatMap(([command, description]) => [
      ui.paint.bold(`${CLI} ${command}`),
      ui.paint.muted(description),
    ]),
  ]);
  const columnWidth = Math.max(...columns.flat().map(ui.visibleLength)) + 4;
  const perRow = ui.width() > 2 + columnWidth * 2 ? 2 : 1;
  for (let index = 0; index < columns.length; index += perRow) {
    const [left, right = []] = columns.slice(index, index + perRow);
    ui.blank();
    for (let row = 0; row < Math.max(left.length, right.length); row += 1) {
      ui.print(`  ${ui.padEnd(left[row] || '', columnWidth)}${right[row] || ''}`.trimEnd());
    }
  }
}

function printHelp({ all = false } = {}) {
  if (all) {
    ui.title('Every neorouter command');
    for (const [name, commands] of REFERENCE) {
      ui.section(ui.paint.accent(name));
      for (const [command, description] of commands) {
        ui.print(`  ${ui.padEnd(`${CLI} ${command}`, 48)}  ${ui.paint.muted(description)}`);
      }
    }
    ui.blank();
    return;
  }
  ui.title("Here's what you can do", 'Not sure? Just type neorouter and pick from a menu.');
  printGroupColumns(GROUPS);
  ui.blank();
  ui.print(`  ${ui.paint.muted('More for developers and admins:')} ${ui.paint.command('neorouter help --all')}`);
  ui.blank();
}

module.exports = {
  printHelp,
};
