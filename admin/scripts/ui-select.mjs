#!/usr/bin/env node

import {
  acquireAdminInitLease,
  acquireDependencyInstallLease,
  ensureInstallerApplyIdle,
  rootFromScript,
  UI_PROFILES,
  inspectWorkspaceState,
  resolveWorkspaceProfile,
  resetWorkspaceSelection,
  selectWorkspaceUI,
} from './init-state.mjs';

const root = rootFromScript(import.meta.url);

async function acquireWorkspaceWriterLeases() {
  const releaseAdmin = await acquireAdminInitLease(root);
  try {
    const dependency = await acquireDependencyInstallLease(root, {
      adminLeaseId: releaseAdmin.lease.id,
    });
    return {
      release: async () => {
        await dependency.release();
        await releaseAdmin();
      },
    };
  } catch (error) {
    await releaseAdmin();
    throw error;
  }
}

function usage() {
  return 'Usage: pnpm run ui:select -- antd|ele|naive [--check|--dry-run] [--json]\n       pnpm run ui:select -- --clear';
}

function parseArgs(argv) {
  const options = { selectedUi: '', check: false, dryRun: false, json: false, clear: false };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--') continue;
    if (argument === '--ui') options.selectedUi = argv[++index] ?? '';
    else if (argument === '--check') options.check = true;
    else if (argument === '--dry-run') options.dryRun = true;
    else if (argument === '--json') options.json = true;
    else if (argument === '--clear') options.clear = true;
    else if (!options.selectedUi && !argument.startsWith('-')) options.selectedUi = argument;
    else if (argument === '--help' || argument === '-h') return { help: true };
    else throw new Error('ARGUMENT_INVALID');
  }
  if (options.check && options.dryRun) throw new Error('ARGUMENT_INVALID');
  if (options.clear && (options.selectedUi || options.check || options.dryRun)) throw new Error('ARGUMENT_INVALID');
  if (!options.clear && options.selectedUi && !UI_PROFILES[options.selectedUi]) throw new Error('UI_INVALID');
  return options;
}

function print(value, json) {
  if (json) {
    process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
    return;
  }
  process.stdout.write(`UI_SELECTED=${value.profile?.selectedUi ?? value.selectedUi ?? 'none'}\n`);
  if (value.previousUi !== undefined) process.stdout.write(`UI_PREVIOUS=${value.previousUi || 'none'}\n`);
  if (value.changed !== undefined) process.stdout.write(`UI_CHANGED=${value.changed}\n`);
  if (value.dryRun) process.stdout.write('UI_DRY_RUN=true\n');
  if (value.report) {
    process.stdout.write(`UI_COMMON_LAYER=${value.report.commonLayer}\n`);
    process.stdout.write(`UI_DEPENDENCIES=${value.report.dependencies}\n`);
  }
  if (value.plan) {
    process.stdout.write(`UI_PRESERVE=${value.plan.preserve.join(',')}\n`);
    process.stdout.write(`UI_INSTALL_ARGS=${value.plan.dependencyArgs.join(' ')}\n`);
  }
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    process.stdout.write(`${usage()}\n`);
    return 0;
  }
  if (options.clear) {
    const leases = await acquireWorkspaceWriterLeases();
    try {
      ensureInstallerApplyIdle(root);
      const state = await resetWorkspaceSelection(root);
      print({ ...state, selectedUi: null, changed: true }, options.json);
      return 0;
    } finally {
      await leases.release();
    }
  }
  if (!options.selectedUi && (options.check || options.dryRun)) {
    const resolved = resolveWorkspaceProfile(root);
    if (resolved.profile) {
      const result = await selectWorkspaceUI(root, resolved.profile.selectedUi, {
        check: options.check,
        dryRun: options.dryRun,
      });
      print(result, options.json);
      return 0;
    }
    const snapshot = inspectWorkspaceState(root);
    if (snapshot.state === 'inconsistent') throw new Error(snapshot.reason || 'WORKSPACE_LAYOUT_INVALID');
    print({ ...resolved, ...snapshot, dryRun: true }, options.json);
    return 0;
  }
  if (!options.selectedUi) throw new Error('UI_INVALID');
  if (options.check || options.dryRun) {
    const result = await selectWorkspaceUI(root, options.selectedUi, {
      check: options.check,
      dryRun: options.dryRun,
    });
    print(result, options.json);
    return 0;
  }
  const leases = await acquireWorkspaceWriterLeases();
  try {
    ensureInstallerApplyIdle(root);
    const result = await selectWorkspaceUI(root, options.selectedUi, {
      leaseOwned: true,
      dependencyLeaseOwned: true,
    });
    print(result, options.json);
    return 0;
  } finally {
    await leases.release();
  }
}

try {
  process.exitCode = await main();
} catch (error) {
  const code = error instanceof Error ? error.message : 'UI_SWITCH_FAILED';
  process.stderr.write(`${code}\n${usage()}\n`);
  process.exitCode = code === 'UI_INVALID' || code === 'ARGUMENT_INVALID' ? 2 : 1;
}
