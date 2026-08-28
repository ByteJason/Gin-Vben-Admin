#!/usr/bin/env node
import {
  STATES,
  STATE_REASONS,
  formatStatus,
  inspectState,
  inspectWorkspaceState,
  rootFromScript,
  workspaceSelectionSignal,
} from './init-state.mjs';

const allowedCommands = new Set(['build', 'dev', 'preview']);
const argument = process.argv.slice(2);
const commandIndex = argument.indexOf('--command');
const command = commandIndex >= 0 ? argument[commandIndex + 1] : '';
const root = rootFromScript(import.meta.url);

if (!allowedCommands.has(command)) {
  process.stdout.write(`${formatStatus({ state: 'inconsistent', profile: null, next: 'PROVIDE_COMMAND', error: 'COMMAND_INVALID' })}\n`);
  process.exit(2);
}

let workspaceMode = false;
let snapshot;
try {
  workspaceMode = workspaceSelectionSignal(root);
  snapshot = workspaceMode ? inspectWorkspaceState(root) : inspectState(root);
} catch (error) {
  snapshot = {
    state: STATES.INCONSISTENT,
    profile: null,
    reason: error?.message || 'PROFILE_INVALID',
  };
}
if (snapshot.state === STATES.INSTALLED) {
  process.stdout.write(`${formatStatus({ ...snapshot, next: `RUN_${command.toUpperCase().replace(':', '_')}` })}\n`);
  process.exit(0);
}

const layoutConflict = !workspaceMode && snapshot.reason === STATE_REASONS.EXTRA_TEMPLATE_PRESENT;
const runnableWorkspaceState = workspaceMode && (
  snapshot.state === STATES.INSTALLED
  // The documented quick start and CI both install the filtered closure
  // outside init. The receipt is an init retry optimisation, not proof that
  // pnpm's public install command ran, so a valid selection is runnable here.
  || snapshot.state === STATES.UI_PREPARED
);
const workspaceError = workspaceMode && snapshot.state === STATES.INCONSISTENT
  && ['WORKSPACE_LAYOUT_INVALID', 'UI_PACKAGE_MISMATCH', 'WORKSPACE_TRANSACTION_INVALID', 'UI_PROFILE_INVALID', 'UI_PROFILE_MISMATCH'].includes(snapshot.reason)
  ? snapshot.reason
  : '';
const error = workspaceError || (layoutConflict ? 'UNSELECTED_UI_WORKSPACE_PRESENT' : ({
  [STATES.PRISTINE]: 'PROFILE_REQUIRED',
  [STATES.UI_PREPARED]: workspaceMode ? 'NONE' : 'INSTALL_MARKER_REQUIRED',
  [STATES.INSTALLING]: 'INITIALIZATION_IN_PROGRESS',
  [STATES.INCONSISTENT]: 'PROFILE_INVALID',
}[snapshot.state] ?? 'PROFILE_INVALID'));
process.stdout.write(`${formatStatus({
  ...snapshot,
  action: runnableWorkspaceState ? 'RUN_SELECTED_APP' : undefined,
  next: layoutConflict
    ? 'REMOVE_UNSELECTED_UI_WORKSPACE'
    : runnableWorkspaceState
      ? `RUN_${command.toUpperCase()}`
      : 'RUN_INIT',
  error,
})}\n`);
process.exit(runnableWorkspaceState
  ? 0
  : snapshot.state === STATES.PRISTINE || snapshot.state === STATES.UI_PREPARED ? 2 : 3);
