#!/usr/bin/env node
import { spawnSync } from 'node:child_process';

import { STATES, inspectState, rootFromScript } from './init-state.mjs';

const commandIndex = process.argv.indexOf('--command');
const command = commandIndex >= 0 ? process.argv[commandIndex + 1] : '';
const root = rootFromScript(import.meta.url);
const snapshot = inspectState(root);

if (!['build', 'build:analyze', 'dev', 'preview'].includes(command) || snapshot.state !== STATES.INSTALLED || !snapshot.profile) {
  process.exit(3);
}

const pnpm = process.env.INIT_PNPM_COMMAND || (process.platform === 'win32' ? 'pnpm.cmd' : 'pnpm');
const result = spawnSync(pnpm, ['-F', snapshot.profile.packageName, 'run', command], {
  cwd: root,
  env: process.env,
  stdio: 'inherit',
  shell: false,
});
if (result.error) process.exit(1);
process.exit(result.status ?? 1);
