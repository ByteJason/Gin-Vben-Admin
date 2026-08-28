#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { closeSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import {
  STATES,
  acquireAdminInitLease,
  ensureInstallerApplyIdle,
  inspectWorkspaceState,
  resolveWorkspaceProfile,
  rootFromScript,
  statePaths,
} from './init-state.mjs';
import { buildDependencySupervisorCommand } from './dependency-launch.mjs';
import { openDependencyLog } from './dependency-log.mjs';

const root = rootFromScript(import.meta.url);

async function installSelectedDependencies(adminLeaseId) {
  const location = statePaths(root);
  const logDescriptor = openDependencyLog(location.dependencyLog);
  const invocation = buildDependencySupervisorCommand({
    execPath: process.execPath,
    platform: process.platform,
    scriptsDirectory: fileURLToPath(new URL('.', import.meta.url)),
    stateRoot: location.stateRoot,
  });
  let supervisor;
  try {
    supervisor = spawn(invocation.command, invocation.args, {
      cwd: root,
      detached: true,
      env: {
        ...process.env,
        GIN_VBEN_ADMIN_LEASE_ID: adminLeaseId,
        GIN_VBEN_FORCE_DEPENDENCY_INSTALL: '1',
        GIN_VBEN_UI_SELECTION_MODE: 'workspace',
      },
      shell: false,
      stdio: ['ignore', logDescriptor, logDescriptor],
      windowsHide: true,
    });
  } finally {
    closeSync(logDescriptor);
  }
  process.stdout.write('UI_INSTALL_LOG=.runtime/install/dependency-install.log\n');
  const status = await new Promise((resolveExit, rejectExit) => {
    supervisor.once('error', rejectExit);
    supervisor.once('exit', (code) => resolveExit(code));
  }).catch(() => null);
  if (status === 3) throw new Error('INIT_BUSY');
  if (status !== 0) throw new Error('DEPENDENCY_INSTALL_FAILED');
}

async function main() {
  const releaseAdmin = await acquireAdminInitLease(root);
  try {
    ensureInstallerApplyIdle(root);
    const resolved = resolveWorkspaceProfile(root);
    if (!resolved.profile) throw new Error('UI_PROFILE_REQUIRED');
    const snapshot = inspectWorkspaceState(root);
    if (snapshot.state === STATES.INCONSISTENT) {
      throw new Error(snapshot.reason || 'WORKSPACE_LAYOUT_INVALID');
    }
    if (![STATES.UI_PREPARED, STATES.INSTALLED].includes(snapshot.state)) {
      throw new Error('INITIALIZATION_IN_PROGRESS');
    }
    await installSelectedDependencies(releaseAdmin.lease.id);
    const completedProfile = resolveWorkspaceProfile(root).profile;
    if (
      !completedProfile
      || completedProfile.selectedUi !== resolved.profile.selectedUi
      || completedProfile.packageName !== resolved.profile.packageName
      || completedProfile.appDirectory !== resolved.profile.appDirectory
    ) throw new Error('UI_PROFILE_MISMATCH');
    process.stdout.write(`UI_INSTALL_SELECTED=${resolved.profile.selectedUi}\nUI_INSTALL_COMPLETE=true\n`);
    return 0;
  } finally {
    await releaseAdmin?.();
  }
}

try {
  process.exitCode = await main();
} catch (error) {
  process.stderr.write(`UI_INSTALL_ERROR=${error instanceof Error ? error.message : 'DEPENDENCY_INSTALL_FAILED'}\n`);
  process.exitCode = 1;
}
