#!/usr/bin/env node
import { join } from 'node:path';
import { Worker } from 'node:worker_threads';

import {
  STATE_REASONS,
  STATES,
  acquireDependencyInstallLease,
  completeDependencyPreparation,
  completeWorkspaceDependencyPreparation,
  buildWorkspaceInstallArgs,
  dependenciesPrepared,
  inspectWorkspaceState,
  inspectState,
  requiredWorkspaceLockfileHash,
  rootFromScript,
  statePaths,
  workspaceDependenciesPrepared,
  workspaceSelectionSignal,
} from './init-state.mjs';
import { buildPnpmCommand } from './pnpm-command.mjs';
import { waitForWindowsJobGate } from './dependency-launch.mjs';

const root = rootFromScript(import.meta.url);

async function waitForWindowsJob() {
  if (process.platform !== 'win32') return { guardianPid: process.pid };
  const gate = process.env.INIT_WINDOWS_JOB_GATE;
  const token = process.env.INIT_WINDOWS_JOB_TOKEN;
  if (!/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(token ?? '')) {
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  if (gate !== join(statePaths(root).stateRoot, `dependency-job-gate-${token}.json`)) {
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  return waitForWindowsJobGate(gate, token);
}

function moduleInvocation(profile, workspaceMode) {
  const installArgs = workspaceMode ? buildWorkspaceInstallArgs(profile) : ['install', '--frozen-lockfile'];
  const invocation = buildPnpmCommand(installArgs);
  if (invocation.command !== process.execPath || invocation.args.length < 1) {
    throw new Error('DEPENDENCY_INSTALL_FAILED');
  }
  const [modulePath, ...workerArgs] = invocation.args;
  return { modulePath, args: workerArgs };
}

function runPnpmWorker(invocation) {
  return new Promise((resolveExit) => {
    let settled = false;
    const worker = new Worker(new URL('./dependency-runner.mjs', import.meta.url), {
      workerData: invocation,
    });
    const finish = (status) => {
      if (settled) return;
      settled = true;
      resolveExit(status);
    };
    worker.once('error', () => finish(1));
    worker.once('exit', (status) => finish(status));
  });
}

let dependencyLease;
let status = 1;
try {
  const job = await waitForWindowsJob();
  const configuredMode = String(process.env.GIN_VBEN_UI_SELECTION_MODE || '').trim().toLowerCase();
  const forceDependencyInstall = process.env.GIN_VBEN_FORCE_DEPENDENCY_INSTALL === '1';
  const workspaceMode = configuredMode === 'workspace'
    || (configuredMode !== 'legacy' && workspaceSelectionSignal(root));
  let current = workspaceMode ? inspectWorkspaceState(root) : inspectState(root);
  const profile = current.profile;
  const prepared = !forceDependencyInstall && (workspaceMode
    ? profile && workspaceDependenciesPrepared(root, profile)
    : profile && dependenciesPrepared(root, profile));
  if (prepared) {
    status = 0;
  } else {
    dependencyLease = await acquireDependencyInstallLease(root, {
      adminLeaseId: process.env.GIN_VBEN_ADMIN_LEASE_ID || '',
      childPid: process.pid,
      supervisorPid: job.guardianPid,
    });
    current = workspaceMode ? inspectWorkspaceState(root) : inspectState(root);
    const alreadyPrepared = !forceDependencyInstall && (workspaceMode
      ? current.profile && workspaceDependenciesPrepared(root, current.profile)
      : current.profile && dependenciesPrepared(root, current.profile));
    if (alreadyPrepared) {
      status = 0;
    } else {
      const installedProfile = current.profile;
      const expectedLockfileHash = workspaceMode
        ? requiredWorkspaceLockfileHash(root)
        : '';
      const invocation = moduleInvocation(installedProfile, workspaceMode);
      const workerStatus = await runPnpmWorker(invocation);
      if (workerStatus !== 0) throw new Error('DEPENDENCY_INSTALL_FAILED');

      current = workspaceMode ? inspectWorkspaceState(root) : inspectState(root);
      if (workspaceMode) {
        await completeWorkspaceDependencyPreparation(root, installedProfile, {
          expectedLockfileHash,
        });
      } else if (current.state === STATES.INSTALLING && current.reason === STATE_REASONS.DEPENDENCIES_PENDING) {
        await completeDependencyPreparation(root);
      }
      status = 0;
    }
  }
} catch (error) {
  const reason = error instanceof Error ? error.message : 'DEPENDENCY_INSTALL_FAILED';
  status = ['DEPENDENCY_INSTALL_BUSY', 'INIT_BUSY'].includes(reason) ? 3 : 1;
  process.stderr.write(`DEPENDENCY_SUPERVISOR_ERROR=${status === 3 ? 'INIT_BUSY' : 'DEPENDENCY_INSTALL_FAILED'}\n`);
} finally {
  if (dependencyLease) {
    if (status === 0) await dependencyLease.release();
    else await dependencyLease.abandon();
  }
}

process.exitCode = status;
