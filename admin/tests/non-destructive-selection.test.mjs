import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import {
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  readlinkSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import test from 'node:test';

import {
  UI_PROFILES,
  buildWorkspaceInstallArgs,
  completeWorkspaceDependencyPreparation,
  initializeWorkspace,
  inspectWorkspaceState,
  resolveWorkspaceProfile,
  resetWorkspaceSelection,
  selectWorkspaceUI,
  statePaths,
  workspaceSelectionSignal,
} from '../scripts/init-state.mjs';

function fixture() {
  const repository = mkdtempSync(join(tmpdir(), 'gin-vben-non-destructive-'));
  const root = join(repository, 'admin');
  mkdirSync(join(root, 'apps'), { recursive: true });
  writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
  writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
  for (const [ui, profile] of Object.entries(UI_PROFILES)) {
    const app = join(root, profile.appDirectory);
    mkdirSync(app, { recursive: true });
    writeFileSync(join(app, 'package.json'), JSON.stringify({ name: profile.packageName }));
    writeFileSync(join(app, 'business.txt'), `${ui}-business\n`);
    writeFileSync(join(app, '.env.development.example'), 'VITE_GLOB_API_URL=/api\n');
    writeFileSync(join(app, '.env.production.example'), 'VITE_GLOB_API_URL=/api\n');
  }
  return root;
}

function filesystemSnapshot(target) {
  let stat;
  try {
    stat = lstatSync(target);
  } catch {
    return { type: 'missing' };
  }
  if (stat.isSymbolicLink()) return { type: 'symlink', target: readlinkSync(target) };
  if (stat.isFile()) return { type: 'file', bytes: readFileSync(target).toString('base64') };
  if (!stat.isDirectory()) return { type: 'other', mode: stat.mode };
  return {
    type: 'directory',
    entries: Object.fromEntries(readdirSync(target).sort().map((name) => [name, filesystemSnapshot(join(target, name))])),
  };
}

function git(cwd, ...args) {
  const result = spawnSync('git', args, { cwd, encoding: 'utf8' });
  assert.equal(result.status, 0, `${args.join(' ')}\n${result.stdout}${result.stderr}`);
  return result.stdout.trim();
}

test('workspace selection writes ignored local state and preserves every tracked UI tree', async () => {
  const root = fixture();
  try {
    const before = Object.fromEntries(Object.entries(UI_PROFILES).map(([ui, profile]) => [
      ui,
      readFileSync(join(root, profile.appDirectory, 'business.txt'), 'utf8'),
    ]));
    const result = await selectWorkspaceUI(root, 'antd');
    assert.equal(result.profile.selectedUi, 'antd');
    assert.equal(result.changed, true);
    assert.equal(existsSync(join(root, '.ui-profile.local.json')), true);
    assert.equal(existsSync(statePaths(root).backupRoot), false);
    for (const [ui, profile] of Object.entries(UI_PROFILES)) {
      assert.equal(existsSync(join(root, profile.appDirectory, 'package.json')), true);
      assert.equal(readFileSync(join(root, profile.appDirectory, 'business.txt'), 'utf8'), before[ui]);
    }
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('switching UI changes only local selector and emits a migration report', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'antd');
    const switched = await selectWorkspaceUI(root, 'naive');
    assert.equal(switched.previousUi, 'antd');
    assert.equal(switched.profile.selectedUi, 'naive');
    assert.equal(switched.report.changedBranch, 'selectedUi');
    assert.equal(switched.report.commonLayer, 'preserved');
    assert.equal(switched.report.sourceAdapter, 'apps/web-antd');
    assert.equal(switched.report.targetAdapter, 'apps/web-naive');
    assert.deepEqual(switched.report.adapterChecks, ['route', 'theme', 'form', 'component']);
    assert.equal(switched.report.manualVerification, 'required');
    assert.equal(switched.report.backendInstallation, 'preserved');
    assert.equal(readFileSync(join(root, '.ui-profile.local.json'), 'utf8').includes('naive'), true);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('a backend apply transaction remains visible while workspace selection exists', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'ele');
    const transaction = statePaths(root).transaction;
    mkdirSync(join(transaction, '..'), { recursive: true });
    writeFileSync(transaction, `${JSON.stringify({
      schema: 1,
      owner: 'server-installer',
      id: 'install-0123456789abcdef0123456789abcdef',
      selectedUi: 'ele',
      mode: 'dev',
      databaseTarget: 'd'.repeat(64),
      phase: 'applying',
      currentStep: 'schema',
      completedSteps: ['plan', 'database', 'redis'],
      updatedAt: '2026-08-24T09:00:00Z',
    })}\n`);
    const repositoryEnvironment = join(root, '..', '.env');
    writeFileSync(repositoryEnvironment, 'APP_UI_ACTIVE=ele\nKEEP=value\n');
    const before = [
      join(root, '.ui-profile.local.json'),
      statePaths(root).workspaceReceipt,
      statePaths(root).workspaceSwitchReport,
      repositoryEnvironment,
      transaction,
    ].map(filesystemSnapshot);

    assert.equal(workspaceSelectionSignal(root), true);
    const state = inspectWorkspaceState(root);
    assert.equal(state.state, 'installing');
    assert.equal(state.reason, 'SERVER_INSTALL_TRANSACTION_PRESENT');
    assert.equal(state.selectedUi, 'ele');
    await assert.rejects(selectWorkspaceUI(root, 'naive'), /INIT_BUSY/);
    assert.deepEqual([
      join(root, '.ui-profile.local.json'),
      statePaths(root).workspaceReceipt,
      statePaths(root).workspaceSwitchReport,
      repositoryEnvironment,
      transaction,
    ].map(filesystemSnapshot), before);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('environment selection overrides local state without rewriting it', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'ele');
    const resolved = resolveWorkspaceProfile(root, { APP_UI: 'naive' });
    assert.equal(resolved.profile.selectedUi, 'naive');
    assert.equal(resolved.source, 'environment');
    assert.equal(readFileSync(join(root, '.ui-profile.local.json'), 'utf8').includes('ele'), true);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('an explicit target must agree with ADMIN_UI or APP_UI', async () => {
  const root = fixture();
  try {
    await assert.rejects(
      selectWorkspaceUI(root, 'ele', { environment: { ADMIN_UI: 'naive' } }),
      /UI_PROFILE_MISMATCH/,
    );
    assert.equal(existsSync(join(root, '.ui-profile.local.json')), false);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('an environment-selected switch compares against persisted state and invalidates its old receipt', async () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    const prepared = await initializeWorkspace(root, 'ele');
    await completeWorkspaceDependencyPreparation(root, prepared.profile);
    assert.equal(existsSync(statePaths(root).workspaceReceipt), true);

    const switched = await selectWorkspaceUI(root, 'naive', {
      environment: { ADMIN_UI: 'naive' },
    });
    assert.equal(switched.previousUi, 'ele');
    assert.equal(switched.changed, true);
    assert.equal(existsSync(statePaths(root).workspaceReceipt), false);
    assert.equal(JSON.parse(readFileSync(join(root, '.ui-profile.local.json'), 'utf8')).selectedUi, 'naive');
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('workspace state remains runnable with all templates present and filters dependency install', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'ele');
    const state = inspectWorkspaceState(root);
    assert.equal(state.profile.selectedUi, 'ele');
    assert.equal(state.allTemplatesPresent, true);
    assert.deepEqual(buildWorkspaceInstallArgs(state.profile), [
      'install', '--filter', '@vben/web-ele...', '--frozen-lockfile',
    ]);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('workspace initializer journals zero moves, completes a receipt, and is idempotent', async () => {
  const root = fixture();
  try {
    const before = Object.fromEntries(Object.entries(UI_PROFILES).map(([ui, profile]) => [
      ui,
      readFileSync(join(root, profile.appDirectory, 'business.txt'), 'utf8'),
    ]));
    const prepared = await initializeWorkspace(root, 'ele');
    assert.equal(prepared.mode, 'workspace');
    const transactionPath = statePaths(root).workspaceTransaction;
    const transaction = JSON.parse(readFileSync(transactionPath, 'utf8'));
    assert.deepEqual(transaction.moves, []);
    assert.equal(existsSync(statePaths(root).backupRoot), false);
    await completeWorkspaceDependencyPreparation(root, prepared.profile);
    assert.equal(existsSync(transactionPath), false);
    assert.equal(existsSync(statePaths(root).workspaceReceipt), true);
    const repeated = await initializeWorkspace(root, 'ele');
    assert.equal(repeated.repeated, true);
    assert.equal(existsSync(transactionPath), false);
    for (const [ui, profile] of Object.entries(UI_PROFILES)) {
      assert.equal(readFileSync(join(root, profile.appDirectory, 'business.txt'), 'utf8'), before[ui]);
    }
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('a lockfile change makes the optional receipt stale without blocking direct-pull usage', async () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    const prepared = await initializeWorkspace(root, 'ele');
    await completeWorkspaceDependencyPreparation(root, prepared.profile);
    assert.equal(inspectWorkspaceState(root).dependenciesReady, true);

    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\nupdated: true\n');
    const stale = inspectWorkspaceState(root);
    assert.equal(stale.state, 'ui_prepared');
    assert.equal(stale.reason, 'NONE');
    assert.equal(stale.dependenciesReady, false);
    const resumed = await initializeWorkspace(root, 'ele');
    assert.equal(resumed.profile.selectedUi, 'ele');
    assert.equal(existsSync(statePaths(root).workspaceTransaction), true);
    assert.equal(existsSync(statePaths(root).backupRoot), false);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('workspace mode is selected by the checked-in manifest before local state exists', () => {
  const root = fixture();
  try {
    assert.equal(workspaceSelectionSignal(root), true);
    assert.equal(inspectWorkspaceState(root).state, 'pristine');
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('selection rejects a missing, malformed, or symbolic-link workspace manifest without writing state', async () => {
  for (const mode of ['missing', 'malformed', 'symlink']) {
    const root = fixture();
    try {
      const manifest = join(root, 'pnpm-workspace.yaml');
      rmSync(manifest);
      if (mode === 'malformed') writeFileSync(manifest, 'packages:\n  - packages/*\n');
      if (mode === 'symlink') {
        const target = join(root, 'workspace-target.yaml');
        writeFileSync(target, 'packages:\n  - apps/*\n');
        symlinkSync(target, manifest);
      }
      if (mode !== 'missing') assert.equal(workspaceSelectionSignal(root), true, mode);
      await assert.rejects(selectWorkspaceUI(root, 'antd'), /WORKSPACE_LAYOUT_INVALID/, mode);
      assert.equal(existsSync(join(root, '.ui-profile.local.json')), false, mode);
      assert.equal(existsSync(join(root, '..', '.runtime')), false, mode);
    } finally {
      rmSync(join(root, '..'), { recursive: true, force: true });
    }
  }
});

test('selection check validates apply preconditions and remains byte-for-byte read-only', async () => {
  for (const mode of ['report-directory', 'pending-transaction', 'state-root-symlink', 'missing-env-template']) {
    const root = fixture();
    try {
      const paths = statePaths(root);
      let expected = /UI_SWITCH_FAILED/;
      if (mode === 'report-directory') {
        mkdirSync(paths.workspaceSwitchReport, { recursive: true });
      }
      if (mode === 'pending-transaction') {
        await selectWorkspaceUI(root, 'antd');
        writeFileSync(paths.workspaceTransaction, `${JSON.stringify({
          schema: 1,
          owner: 'admin-init-workspace',
          id: '12345678-1234-1234-1234-123456789abc',
          selectedUi: 'antd',
          phase: 'dependencies_pending',
          moves: [],
        })}\n`);
        expected = /INITIALIZATION_IN_PROGRESS/;
      }
      if (mode === 'state-root-symlink') {
        const target = join(root, '..', 'external-state');
        mkdirSync(join(root, '..', '.runtime'), { recursive: true });
        mkdirSync(target);
        symlinkSync(target, paths.stateRoot);
      }
      if (mode === 'missing-env-template') {
        rmSync(join(root, 'apps', 'web-antd', '.env.production.example'));
        expected = /RUNTIME_ENV_TEMPLATE_INVALID/;
      }
      const before = filesystemSnapshot(join(root, '..'));
      await assert.rejects(selectWorkspaceUI(root, 'antd', { check: true }), expected, mode);
      assert.deepEqual(filesystemSnapshot(join(root, '..')), before, mode);
    } finally {
      rmSync(join(root, '..'), { recursive: true, force: true });
    }
  }
});

test('dependency completion validates the canonical profile, layout, and regular lockfile before mutation', async () => {
  for (const mode of ['forged-profile', 'missing-lockfile', 'symlink-lockfile', 'missing-layout', 'malformed-transaction']) {
    const root = fixture();
    try {
      const prepared = await initializeWorkspace(root, 'ele');
      const paths = statePaths(root);
      let completionProfile = prepared.profile;
      if (mode === 'forged-profile') completionProfile = { ...prepared.profile, packageName: '@vben/web-naive' };
      if (mode === 'missing-lockfile') rmSync(join(root, 'pnpm-lock.yaml'));
      if (mode === 'symlink-lockfile') {
        const target = join(root, 'lockfile-target.yaml');
        writeFileSync(target, 'lockfileVersion: 9\n');
        rmSync(join(root, 'pnpm-lock.yaml'));
        symlinkSync(target, join(root, 'pnpm-lock.yaml'));
      }
      if (mode === 'missing-layout') rmSync(join(root, 'apps', 'web-naive'), { recursive: true });
      if (mode === 'malformed-transaction') writeFileSync(paths.workspaceTransaction, '{"schema":0}\n');
      const transactionBytes = readFileSync(paths.workspaceTransaction);

      await assert.rejects(
        completeWorkspaceDependencyPreparation(root, completionProfile),
        /UI_PROFILE_INVALID|WORKSPACE_LAYOUT_INVALID|WORKSPACE_TRANSACTION_INVALID/,
        mode,
      );
      assert.deepEqual(readFileSync(paths.workspaceTransaction), transactionBytes, mode);
      assert.equal(existsSync(paths.workspaceReceipt), false, mode);
    } finally {
      rmSync(join(root, '..'), { recursive: true, force: true });
    }
  }
});

test('a pristine workspace reports a missing tracked UI template instead of hiding it', () => {
  const root = fixture();
  try {
    rmSync(join(root, 'apps', 'web-naive'), { recursive: true, force: true });
    const state = inspectWorkspaceState(root);
    assert.equal(state.state, 'inconsistent');
    assert.equal(state.reason, 'WORKSPACE_LAYOUT_INVALID');
    assert.equal(state.allTemplatesPresent, false);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('workspace status rejects orphaned installed and dependency metadata without a profile', () => {
  for (const [name, stateFile, expectedReason] of [
    ['marker', 'marker', 'MARKER_WITHOUT_PROFILE'],
    ['receipt', 'workspaceReceipt', 'RECEIPT_WITHOUT_PROFILE'],
  ]) {
    const root = fixture();
    try {
      const path = statePaths(root)[stateFile];
      mkdirSync(join(path, '..'), { recursive: true });
      writeFileSync(path, name === 'marker' ? '{}' : '{"schema":1}\n');
      const state = inspectWorkspaceState(root);
      assert.equal(state.state, 'inconsistent', name);
      assert.equal(state.reason, expectedReason, name);
    } finally {
      rmSync(join(root, '..'), { recursive: true, force: true });
    }
  }
});

test('legacy journal evidence keeps compatibility migration on the legacy path', () => {
  const root = fixture();
  try {
    writeFileSync(statePaths(root).receipt, '{"legacy":true}\n');
    assert.equal(workspaceSelectionSignal(root), false);
    assert.equal(workspaceSelectionSignal(root, { ADMIN_UI: 'naive' }), false);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('a durable local selector outranks passive legacy receipts and retained backups', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'ele');
    writeFileSync(statePaths(root).receipt, '{"legacy":true}\n');
    const retained = join(statePaths(root).legacyBackupRoot, 'retained', 'apps', 'web-naive');
    mkdirSync(retained, { recursive: true });
    writeFileSync(join(retained, 'historical.txt'), 'retained\n');
    assert.equal(workspaceSelectionSignal(root), true);
    assert.equal(inspectWorkspaceState(root).selectedUi, 'ele');
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('switching an installed workspace preserves and archives the backend marker', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'antd');
    const marker = statePaths(root).marker;
    mkdirSync(join(root, '..', '.runtime', 'install'), { recursive: true });
    const markerContents = JSON.stringify({
      schema_version: 1,
      installer_version: 'test',
      installed_at: '2026-08-28T00:00:00Z',
      selected_ui: 'antd',
      mode: 'dev',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    });
    writeFileSync(marker, markerContents);
    const repositoryEnvironment = join(root, '..', '.env');
    writeFileSync(repositoryEnvironment, 'DATABASE_DSN="keep-secret"\r\n  export APP_UI_ACTIVE = "antd"\r\n');
    const switched = await selectWorkspaceUI(root, 'naive');
    assert.equal(switched.markerArchived.startsWith('.runtime/install/ui-switch-history/'), true);
    assert.equal(existsSync(marker), true);
    assert.equal(readFileSync(marker, 'utf8'), markerContents);
    assert.equal(
      readFileSync(join(root, '..', switched.markerArchived), 'utf8'),
      markerContents,
    );
    assert.equal(
      readFileSync(repositoryEnvironment, 'utf8'),
      'DATABASE_DSN="keep-secret"\r\nAPP_UI_ACTIVE="naive"\r\n',
    );
    assert.equal(existsSync(statePaths(root).backupRoot), false);
    assert.equal(inspectWorkspaceState(root).state, 'installed');
    assert.equal(inspectWorkspaceState(root).selectedUi, 'naive');
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('a failed installed-workspace switch restores profile, receipt, report, marker, and environment bytes', async () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    const prepared = await initializeWorkspace(root, 'antd');
    await completeWorkspaceDependencyPreparation(root, prepared.profile);
    const paths = statePaths(root);
    const markerContents = JSON.stringify({
      schema_version: 1,
      installer_version: 'test',
      installed_at: '2026-08-28T00:00:00Z',
      selected_ui: 'antd',
      mode: 'dev',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    });
    writeFileSync(paths.marker, markerContents);
    writeFileSync(paths.workspaceSwitchReport, '{"previous":"report"}\n');
    const repositoryEnvironment = join(root, '..', '.env');
    writeFileSync(repositoryEnvironment, 'DATABASE_DSN="keep-secret"\nAPP_UI_ACTIVE="antd"\n');
    const protectedFiles = [
      paths.localProfile,
      paths.workspaceReceipt,
      paths.workspaceSwitchReport,
      paths.marker,
      repositoryEnvironment,
    ];
    const before = new Map(protectedFiles.map((file) => [file, readFileSync(file)]));

    await assert.rejects(
      selectWorkspaceUI(root, 'naive', {
        afterEnvironmentWrite: () => { throw new Error('fixture failure'); },
      }),
      /UI_SWITCH_FAILED/,
    );
    for (const file of protectedFiles) {
      assert.deepEqual(readFileSync(file), before.get(file), file);
    }
    const state = inspectWorkspaceState(root);
    assert.equal(state.state, 'installed');
    assert.equal(state.selectedUi, 'antd');
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('a failed switch rollback keeps its blocking journal when a derived file cannot be restored', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'antd');
    const paths = statePaths(root);

    await assert.rejects(
      selectWorkspaceUI(root, 'naive', {
        afterProfileWrite: () => {
          rmSync(paths.workspaceSwitchReport, { force: true });
          mkdirSync(paths.workspaceSwitchReport);
          throw new Error('fixture rollback failure');
        },
      }),
      /UI_SWITCH_FAILED/,
    );

    const transaction = JSON.parse(readFileSync(paths.workspaceTransaction, 'utf8'));
    assert.equal(transaction.phase, 'switching_ui');
    assert.equal(transaction.selectedUi, 'naive');
    const blocked = inspectWorkspaceState(root);
    assert.equal(blocked.state, 'installing');
    assert.equal(blocked.reason, 'UI_SWITCH_PENDING');
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('a killed switch leaves a recovery journal and resumes without a split selector', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'antd');
    const repositoryEnvironment = join(root, '..', '.env');
    writeFileSync(repositoryEnvironment, 'DATABASE_DSN="keep-secret"\nAPP_UI_ACTIVE="antd"\n');
    const runner = join(root, '..', 'crash-switch.mjs');
    const moduleURL = pathToFileURL(join(import.meta.dirname, '..', 'scripts', 'init-state.mjs')).href;
    writeFileSync(runner, [
      `import { selectWorkspaceUI } from ${JSON.stringify(moduleURL)};`,
      `await selectWorkspaceUI(${JSON.stringify(root)}, 'naive', {`,
      '  afterEnvironmentWrite: () => process.exit(86),',
      '});',
      '',
    ].join('\n'));

    const crashed = spawnSync(process.execPath, [runner], { encoding: 'utf8' });
    assert.equal(crashed.status, 86);
    assert.equal(JSON.parse(readFileSync(statePaths(root).localProfile, 'utf8')).selectedUi, 'antd');
    assert.equal(JSON.parse(readFileSync(statePaths(root).workspaceTransaction, 'utf8')).phase, 'switching_ui');
    const pending = inspectWorkspaceState(root);
    assert.equal(pending.state, 'installing');
    assert.equal(pending.reason, 'UI_SWITCH_PENDING');
    assert.equal(pending.selectedUi, 'naive');
    const pendingTransactionBytes = readFileSync(statePaths(root).workspaceTransaction);
    const pendingEnvironmentBytes = readFileSync(repositoryEnvironment);
    await assert.rejects(resetWorkspaceSelection(root), /INITIALIZATION_IN_PROGRESS/);
    assert.deepEqual(readFileSync(statePaths(root).workspaceTransaction), pendingTransactionBytes);
    assert.deepEqual(readFileSync(repositoryEnvironment), pendingEnvironmentBytes);
    await assert.rejects(
      completeWorkspaceDependencyPreparation(root, {
        schema: 1,
        selectedUi: 'naive',
        ...UI_PROFILES.naive,
      }),
      /WORKSPACE_TRANSACTION_INVALID/,
    );
    assert.equal(existsSync(statePaths(root).workspaceTransaction), true);
    assert.equal(existsSync(statePaths(root).workspaceReceipt), false);

    const resumed = await selectWorkspaceUI(root, 'naive');
    assert.equal(resumed.profile.selectedUi, 'naive');
    assert.equal(existsSync(statePaths(root).workspaceTransaction), false);
    assert.equal(JSON.parse(readFileSync(statePaths(root).localProfile, 'utf8')).selectedUi, 'naive');
    assert.equal(
      readFileSync(repositoryEnvironment, 'utf8'),
      'DATABASE_DSN="keep-secret"\nAPP_UI_ACTIVE="antd"\n',
    );
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('a killed installed switch resumes the environment and preserves the backend marker', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'antd');
    const paths = statePaths(root);
    const markerContents = JSON.stringify({
      schema_version: 1,
      installer_version: 'test',
      installed_at: '2026-08-28T00:00:00Z',
      selected_ui: 'antd',
      mode: 'dev',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    });
    writeFileSync(paths.marker, markerContents);
    const repositoryEnvironment = join(root, '..', '.env');
    writeFileSync(repositoryEnvironment, 'DATABASE_DSN="keep-secret"\nAPP_UI_ACTIVE="antd"\n');
    const runner = join(root, '..', 'crash-installed-switch.mjs');
    const moduleURL = pathToFileURL(join(import.meta.dirname, '..', 'scripts', 'init-state.mjs')).href;
    writeFileSync(runner, [
      `import { selectWorkspaceUI } from ${JSON.stringify(moduleURL)};`,
      `await selectWorkspaceUI(${JSON.stringify(root)}, 'naive', {`,
      '  afterEnvironmentWrite: () => process.exit(88),',
      '});',
      '',
    ].join('\n'));

    const crashed = spawnSync(process.execPath, [runner], { encoding: 'utf8' });
    assert.equal(crashed.status, 88);
    assert.equal(JSON.parse(readFileSync(paths.localProfile, 'utf8')).selectedUi, 'antd');
    assert.equal(readFileSync(repositoryEnvironment, 'utf8'), 'DATABASE_DSN="keep-secret"\nAPP_UI_ACTIVE="naive"\n');
    assert.equal(inspectWorkspaceState(root).reason, 'UI_SWITCH_PENDING');

    await selectWorkspaceUI(root, 'naive');
    assert.equal(JSON.parse(readFileSync(paths.localProfile, 'utf8')).selectedUi, 'naive');
    assert.equal(existsSync(paths.workspaceTransaction), false);
    assert.equal(readFileSync(paths.marker, 'utf8'), markerContents);
    assert.equal(readFileSync(repositoryEnvironment, 'utf8'), 'DATABASE_DSN="keep-secret"\nAPP_UI_ACTIVE="naive"\n');
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('dependency completion cannot consume a switch journal after selector commit', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'antd');
    const runner = join(root, '..', 'crash-after-profile.mjs');
    const moduleURL = pathToFileURL(join(import.meta.dirname, '..', 'scripts', 'init-state.mjs')).href;
    writeFileSync(runner, [
      `import { selectWorkspaceUI } from ${JSON.stringify(moduleURL)};`,
      `await selectWorkspaceUI(${JSON.stringify(root)}, 'naive', {`,
      '  afterProfileWrite: () => process.exit(87),',
      '});',
      '',
    ].join('\n'));
    const crashed = spawnSync(process.execPath, [runner], { encoding: 'utf8' });
    assert.equal(crashed.status, 87);
    assert.equal(JSON.parse(readFileSync(statePaths(root).localProfile, 'utf8')).selectedUi, 'naive');
    const transactionBytes = readFileSync(statePaths(root).workspaceTransaction);

    await assert.rejects(
      completeWorkspaceDependencyPreparation(root, {
        schema: 1,
        selectedUi: 'naive',
        ...UI_PROFILES.naive,
      }),
      /WORKSPACE_TRANSACTION_INVALID/,
    );
    assert.deepEqual(readFileSync(statePaths(root).workspaceTransaction), transactionBytes);
    assert.equal(existsSync(statePaths(root).workspaceReceipt), false);
    const resumed = await selectWorkspaceUI(root, 'naive');
    assert.equal(resumed.resumed, true);
    assert.equal(existsSync(statePaths(root).workspaceTransaction), false);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('reset refuses an installed workspace without removing the local selector', async () => {
  const root = fixture();
  try {
    await selectWorkspaceUI(root, 'antd');
    const marker = statePaths(root).marker;
    mkdirSync(join(root, '..', '.runtime', 'install'), { recursive: true });
    writeFileSync(marker, JSON.stringify({
      schema_version: 1,
      installer_version: 'test',
      installed_at: '2026-08-28T00:00:00Z',
      selected_ui: 'antd',
      mode: 'dev',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    }));
    await assert.rejects(resetWorkspaceSelection(root), /RESET_UNAVAILABLE_INSTALLED/);
    assert.equal(existsSync(join(root, '.ui-profile.local.json')), true);
    assert.equal(existsSync(marker), true);
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('reset refuses orphaned marker state without reporting a false success', async () => {
  const root = fixture();
  try {
    const marker = statePaths(root).marker;
    mkdirSync(join(marker, '..'), { recursive: true });
    writeFileSync(marker, '{"orphaned":true}\n');
    await assert.rejects(resetWorkspaceSelection(root), /RESET_UNAVAILABLE_INSTALLED/);
    assert.equal(readFileSync(marker, 'utf8'), '{"orphaned":true}\n');
  } finally {
    rmSync(join(root, '..'), { recursive: true, force: true });
  }
});

test('a selected clone fast-forwards upstream changes to all UI trees without tracked conflicts', async () => {
  const seedAdmin = fixture();
  const seed = join(seedAdmin, '..');
  const remote = mkdtempSync(join(tmpdir(), 'gin-vben-remote-'));
  const cloneParent = mkdtempSync(join(tmpdir(), 'gin-vben-clone-'));
  const clone = join(cloneParent, 'checkout');
  try {
    writeFileSync(join(seed, '.gitignore'), [
      '/.runtime/',
      '/.env',
      '/admin/.ui-profile.local.json',
      '/admin/apps/web-*/.env.development',
      '/admin/apps/web-*/.env.production',
      '',
    ].join('\n'));
    git(seed, 'init');
    git(seed, 'checkout', '-b', 'main');
    git(seed, 'config', 'user.name', 'Fixture');
    git(seed, 'config', 'user.email', 'fixture@example.invalid');
    git(seed, 'add', '.');
    git(seed, 'commit', '-m', 'initial workspace');
    git(remote, 'init', '--bare');
    git(seed, 'remote', 'add', 'origin', remote);
    git(seed, 'push', '-u', 'origin', 'main');
    git(cloneParent, 'clone', '--branch', 'main', remote, clone);

    const cloneAdmin = join(clone, 'admin');
    await selectWorkspaceUI(cloneAdmin, 'ele');
    const selectorBytes = readFileSync(join(cloneAdmin, '.ui-profile.local.json'));
    assert.equal(git(clone, 'status', '--porcelain', '--untracked-files=all'), '');

    for (const ui of ['antd', 'ele', 'naive']) {
      writeFileSync(join(seedAdmin, 'apps', `web-${ui}`, 'business.txt'), `${ui}-upstream-v2\n`);
    }
    writeFileSync(join(seedAdmin, 'pnpm-lock.yaml'), 'lockfileVersion: 9\nupstream: v2\n');
    git(seed, 'add', 'admin');
    git(seed, 'commit', '-m', 'update every ui');
    git(seed, 'push', 'origin', 'main');

    git(clone, 'pull', '--ff-only');
    assert.deepEqual(readFileSync(join(cloneAdmin, '.ui-profile.local.json')), selectorBytes);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(readFileSync(join(cloneAdmin, 'apps', `web-${ui}`, 'business.txt'), 'utf8'), `${ui}-upstream-v2\n`);
    }
    assert.equal(readFileSync(join(cloneAdmin, 'pnpm-lock.yaml'), 'utf8'), 'lockfileVersion: 9\nupstream: v2\n');
    assert.equal(git(clone, 'status', '--porcelain', '--untracked-files=all'), '');
  } finally {
    rmSync(seed, { recursive: true, force: true });
    rmSync(remote, { recursive: true, force: true });
    rmSync(cloneParent, { recursive: true, force: true });
  }
});
