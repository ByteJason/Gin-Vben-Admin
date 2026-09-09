#!/usr/bin/env node
import { spawnSync } from 'node:child_process';

import {
  STATES,
  STATE_REASONS,
  formatStatus,
  ensureSelectedUIRuntimeEnv,
  inspectState,
  inspectWorkspaceState,
  rootFromScript,
  workspaceSelectionSignal,
} from './init-state.mjs';

import { buildPnpmCommand } from './pnpm-command.mjs';

const allowedCommands = new Set(['build', 'dev', 'preview']);
const argument = process.argv.slice(2);
const commandIndex = argument.indexOf('--command');
const command = commandIndex >= 0 ? argument[commandIndex + 1] : '';
const root = rootFromScript(import.meta.url);

if (!allowedCommands.has(command)) {
  process.stderr.write(`${formatStatus({ state: 'inconsistent', profile: null, next: 'PROVIDE_COMMAND', error: 'COMMAND_INVALID' })}\n`);
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
const runnable = runnableWorkspaceState || snapshot.state === STATES.INSTALLED;
if (!runnable || !snapshot.profile) {
  process.stderr.write(`${formatStatus({
    ...snapshot,
    next: layoutConflict ? 'REMOVE_UNSELECTED_UI_WORKSPACE' : 'RUN_INIT',
    error,
  })}\n`);
  process.exit(snapshot.state === STATES.PRISTINE || snapshot.state === STATES.UI_PREPARED ? 2 : 3);
}

// Inspection is shared with real dispatch. Successful startup has no init
// status dump; `pnpm run init -- --check` remains the diagnostic entrypoint.
if (argument.includes('--check')) process.exit(0);

try {
  await ensureSelectedUIRuntimeEnv(root, snapshot.profile);
} catch (error) {
  const knownErrors = new Set([
    'RUNTIME_ENV_PROFILE_INVALID',
    'RUNTIME_ENV_APP_INVALID',
    'RUNTIME_ENV_TEMPLATE_INVALID',
    'RUNTIME_ENV_TARGET_INVALID',
  ]);
  const errorCode = error instanceof Error && knownErrors.has(error.message)
    ? error.message
    : 'RUNTIME_ENV_UNKNOWN';
  process.stderr.write(`RUNTIME_ENV_PREPARATION_FAILED\nRUNTIME_ENV_ERROR=${errorCode}\n`);
  process.exit(1);
}

let invocation;
try {
  invocation = buildPnpmCommand(['-F', snapshot.profile.packageName, 'run', command]);
} catch {
  process.exit(1);
}
const result = spawnSync(invocation.command, invocation.args, {
  cwd: root,
  env: process.env,
  stdio: 'inherit',
  shell: false,
});
if (result.error) process.exit(1);
process.exit(result.status ?? 1);
