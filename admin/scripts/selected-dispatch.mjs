#!/usr/bin/env node
import { spawnSync } from 'node:child_process';

import { STATES, inspectState, rootFromScript } from './init-state.mjs';
import { buildPnpmCommand } from './pnpm-command.mjs';

const commandIndex = process.argv.indexOf('--command');
const command = commandIndex >= 0 ? process.argv[commandIndex + 1] : '';
const root = rootFromScript(import.meta.url);
const snapshot = inspectState(root);

if (!['build', 'dev', 'preview'].includes(command) || snapshot.state !== STATES.INSTALLED || !snapshot.profile) {
  process.exit(3);
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
