#!/usr/bin/env node
import { STATES, formatStatus, inspectState, rootFromScript } from './init-state.mjs';

const allowedCommands = new Set(['build', 'build:analyze', 'dev', 'preview']);
const argument = process.argv.slice(2);
const commandIndex = argument.indexOf('--command');
const command = commandIndex >= 0 ? argument[commandIndex + 1] : '';
const root = rootFromScript(import.meta.url);

if (!allowedCommands.has(command)) {
  process.stdout.write(`${formatStatus({ state: 'inconsistent', profile: null, next: 'PROVIDE_COMMAND', error: 'COMMAND_INVALID' })}\n`);
  process.exit(2);
}

const snapshot = inspectState(root);
if (snapshot.state === STATES.INSTALLED) {
  process.stdout.write(`${formatStatus({ ...snapshot, next: `RUN_${command.toUpperCase().replace(':', '_')}` })}\n`);
  process.exit(0);
}

const error = {
  [STATES.PRISTINE]: 'PROFILE_REQUIRED',
  [STATES.UI_PREPARED]: 'INSTALL_MARKER_REQUIRED',
  [STATES.INSTALLING]: 'INITIALIZATION_IN_PROGRESS',
  [STATES.INCONSISTENT]: 'PROFILE_INVALID',
}[snapshot.state] ?? 'PROFILE_INVALID';
process.stdout.write(`${formatStatus({ ...snapshot, next: 'RUN_INIT', error })}\n`);
process.exit(snapshot.state === STATES.PRISTINE || snapshot.state === STATES.UI_PREPARED ? 2 : 3);
