#!/usr/bin/env node
import { spawnSync } from 'node:child_process';

import {
  STATES,
  ensureSelectedUIRuntimeEnv,
  inspectState,
  rootFromScript,
} from './init-state.mjs';
import { buildPnpmCommand } from './pnpm-command.mjs';

const commandIndex = process.argv.indexOf('--command');
const command = commandIndex >= 0 ? process.argv[commandIndex + 1] : '';
const root = rootFromScript(import.meta.url);
const snapshot = inspectState(root);

if (!['build', 'dev', 'preview'].includes(command) || snapshot.state !== STATES.INSTALLED || !snapshot.profile) {
  process.exit(3);
}

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
