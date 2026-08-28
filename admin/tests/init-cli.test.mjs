import assert from 'node:assert/strict';
import { accessSync, constants as fsConstants, cpSync, existsSync, linkSync, lstatSync, mkdtempSync, mkdirSync, readFileSync, readdirSync, readlinkSync, renameSync, rmSync, statSync, symlinkSync, utimesSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, parse } from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import { link as linkAsync, open as openAsync, rename as renameAsync, rm as rmAsync } from 'node:fs/promises';
import test from 'node:test';

import {
  acquireAdminInitLease,
  acquireDependencyInstallLease,
  ensureSelectedUIRuntimeEnv,
  ensureInstallerApplyIdle,
  migrateLegacyPreparedState,
  preflightInitialization,
  preflightInitializationResume,
  preflightLegacyPreparedMigration,
  preflightReset,
  preflightSafeLocalRecovery,
  recoverSafeLocalState,
  reset as resetInitialization,
  stableInitializationErrorCode,
  syncDirectory,
} from '../scripts/init-state.mjs';

const sourceRoot = join(import.meta.dirname, '..');
const fixtureRepositories = new Map();

test('directory fsync tolerates Windows directory-open limitations without hiding other failures', async () => {
  const windowsError = Object.assign(new Error('directory handles are unavailable'), { code: 'EPERM' });
  const openDirectory = async () => { throw windowsError; };

  await assert.doesNotReject(syncDirectory('C:\\fixture', { platform: 'win32', openDirectory }));
  await assert.rejects(syncDirectory('/fixture', { platform: 'linux', openDirectory }), windowsError);
});

function fixture() {
  const repository = mkdtempSync(join(tmpdir(), 'gin-vben-init-'));
  const root = join(repository, 'admin');
  mkdirSync(join(root, 'scripts'), { recursive: true });
  for (const name of [
    'dependency-runner.mjs',
    'dependency-launch.mjs',
    'dependency-log.mjs',
    'dependency-supervisor.mjs',
    'dependency-supervisor-windows.ps1',
    'init-heartbeat.mjs',
    'init.mjs',
    'init-state.mjs',
    'pnpm-command.mjs',
    'process-identity.mjs',
    'profile-gate.mjs',
    'selected-dispatch.mjs',
    'ui-install.mjs',
    'ui-select.mjs',
  ]) {
    cpSync(join(sourceRoot, 'scripts', name), join(root, 'scripts', name));
  }
  for (const ui of ['antd', 'ele', 'naive']) {
    const directory = join(root, 'apps', `web-${ui}`);
    mkdirSync(directory, { recursive: true });
    writeFileSync(join(directory, 'package.json'), JSON.stringify({ name: `@vben/web-${ui}` }));
    writeFileSync(join(directory, '.env.development.example'), [
      'VITE_APP_TITLE=Gin Vben Admin',
      'VITE_GLOB_API_URL=/api',
      '',
    ].join('\n'));
    writeFileSync(join(directory, '.env.production.example'), [
      'VITE_APP_TITLE=Gin Vben Admin',
      'VITE_GLOB_API_URL=/api',
      '',
    ].join('\n'));
  }
  mkdirSync(join(root, 'apps', 'install'), { recursive: true });
  fixtureRepositories.set(root, repository);
  return root;
}

function legacyPreparedFixture({ selectedUi = 'ele', transactionId = '12345678-1234-1234-1234-123456789abc' } = {}) {
  const root = fixture();
  const moves = ['antd', 'ele', 'naive']
    .filter((ui) => ui !== selectedUi)
    .map((ui) => ({ source: `apps/web-${ui}`, backup: `apps/web-${ui}` }));
  const profile = {
    schema: 1,
    selectedUi,
    packageName: `@vben/web-${selectedUi}`,
    appDirectory: `apps/web-${selectedUi}`,
  };
  const receipt = { schema: 1, transactionId, selectedUi, moves };
  const legacyBackup = join(root, '..', '.runtime', 'init-backup', transactionId);
  mkdirSync(join(legacyBackup, 'apps'), { recursive: true });
  for (const move of moves) renameSync(join(root, move.source), join(legacyBackup, move.backup));
  const profileBytes = `${JSON.stringify(profile, null, 2)}\n`;
  const receiptBytes = `${JSON.stringify(receipt, null, 2)}\n`;
  writeFileSync(join(root, '.ui-profile.json'), profileBytes);
  writeFileSync(join(root, '.ui-init-receipt.json'), receiptBytes);
  const historicalRecovery = join(root, '..', '.runtime', 'init-recovery', 'historical', 'checkpoint.json');
  mkdirSync(join(historicalRecovery, '..'), { recursive: true });
  writeFileSync(historicalRecovery, '{"legacy":true}\n');
  assert.equal(existsSync(join(root, 'node_modules')), false);
  return { historicalRecovery, legacyBackup, moves, profile, profileBytes, receipt, receiptBytes, root, transactionId };
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

function dispose(root) {
  rmSync(fixtureRepositories.get(root) ?? root, { force: true, recursive: true });
  fixtureRepositories.delete(root);
}

function run(root, script, args = [], env = {}) {
  return spawnSync(process.execPath, [join(root, 'scripts', script), ...args], {
    cwd: root,
    encoding: 'utf8',
    env: {
      ...process.env,
      INIT_API_TEST_MODE: 'ready',
      INIT_DEPENDENCY_INSTALL_TEST_MODE: 'success',
      ...env,
    },
  });
}

function runAsync(root, script, args = [], env = {}) {
  const child = spawn(process.execPath, [join(root, 'scripts', script), ...args], {
    cwd: root,
    env: {
      ...process.env,
      INIT_API_TEST_MODE: 'ready',
      INIT_DEPENDENCY_INSTALL_TEST_MODE: 'success',
      ...env,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let stdout = '';
  let stderr = '';
  child.stdout.setEncoding('utf8');
  child.stderr.setEncoding('utf8');
  child.stdout.on('data', (chunk) => { stdout += chunk; });
  child.stderr.on('data', (chunk) => { stderr += chunk; });
  return new Promise((resolveRun) => {
    child.once('error', (error) => resolveRun({ status: null, stdout, stderr: `${stderr}${error.message}` }));
    child.once('close', (status, signal) => resolveRun({ status, signal, stdout, stderr }));
  });
}

function output(result) {
  return `${result.stdout}${result.stderr}`;
}

function adminLeasePath(root) {
  return join(root, '..', '.runtime', 'install', 'admin-init.lock');
}

function adminHeartbeatRoot(root) {
  return join(root, '..', '.runtime', 'install', 'admin-init-heartbeat');
}

function writeGoCompatibleApplyLease(root) {
  const stateRoot = join(root, '..', '.runtime', 'install');
  const path = join(stateRoot, 'apply.lock');
  const contents = `${JSON.stringify({
    schema: 2,
    pid: process.pid,
    createdAt: new Date().toISOString(),
  })}\n`;
  mkdirSync(stateRoot, { recursive: true });
  writeFileSync(path, contents);
  return { contents, path };
}

function writeAdminLease(root, {
  pid = process.pid,
  pidStartToken = 'a'.repeat(64),
  createdAt = new Date().toISOString(),
  id = '12345678-1234-1234-1234-123456789abc',
  schema = 2,
} = {}) {
  const path = adminLeasePath(root);
  mkdirSync(join(root, '..', '.runtime', 'install'), { recursive: true });
  const value = schema === 1
    ? { schema, owner: 'admin-init', id, pid, createdAt }
    : { schema, owner: 'admin-init', id, pid, pidStartToken, createdAt };
  const contents = `${JSON.stringify(value)}\n`;
  writeFileSync(path, contents);
  return { path, contents, ...value };
}

function writeAdminHeartbeat(root, lease, { updatedAt = new Date().toISOString() } = {}) {
  const directory = adminHeartbeatRoot(root);
  mkdirSync(directory, { recursive: true });
  const path = join(directory, `${lease.id}.json`);
  const value = lease.schema === 1
    ? { schema: 1, owner: 'admin-init', id: lease.id, pid: lease.pid, updatedAt }
    : {
        schema: 2,
        owner: 'admin-init',
        id: lease.id,
        pid: lease.pid,
        pidStartToken: lease.pidStartToken,
        updatedAt,
      };
  const contents = `${JSON.stringify(value)}\n`;
  writeFileSync(path, contents);
  return { path, contents };
}

function writeDependencyLease(root, {
  childPid = process.pid,
  childStartToken = 'a'.repeat(64),
  createdAt = new Date().toISOString(),
  id = '12345678-1234-1234-1234-123456789abc',
  supervisorPid = process.pid,
  supervisorStartToken = 'a'.repeat(64),
  updatedAt = createdAt,
} = {}) {
  const stateRoot = join(root, '..', '.runtime', 'install');
  mkdirSync(stateRoot, { recursive: true });
  const lease = {
    schema: 2,
    owner: 'admin-dependency-install',
    id,
    supervisorPid,
    supervisorStartToken,
    childPid,
    childStartToken,
    createdAt,
  };
  const path = join(stateRoot, 'dependency-install.lock');
  writeFileSync(path, `${JSON.stringify(lease)}\n`);
  const heartbeatRoot = join(stateRoot, 'dependency-install-heartbeat');
  mkdirSync(heartbeatRoot, { recursive: true });
  const heartbeatPath = join(heartbeatRoot, `${id}.json`);
  writeFileSync(heartbeatPath, `${JSON.stringify({
    schema: 2,
    owner: lease.owner,
    id,
    supervisorPid,
    supervisorStartToken,
    childPid,
    childStartToken,
    updatedAt,
  })}\n`);
  return { lease, path, heartbeatPath };
}

function definitelyDeadPID() {
  for (const pid of [2_147_483_646, 999_999_999, 99_999_999]) {
    try {
      process.kill(pid, 0);
    } catch (error) {
      if (error?.code === 'ESRCH') return pid;
    }
  }
  throw new Error('DEAD_PID_FIXTURE_UNAVAILABLE');
}

test('pristine init without a UI directs the user to the web installer without changing workspace state', () => {
  const root = fixture();
  try {
    const result = run(root, 'init.mjs', ['--no-open']);
    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_STATE=pristine/);
    assert.match(output(result), /INIT_ACTION=OPEN_INSTALLER/);
    assert.match(output(result), /INIT_URL=http:\/\/127\.0\.0\.1:8080\/install/);
    assert.match(output(result), /INIT_NEXT=OPEN_INSTALLER/);
    assert.match(output(result), /INIT_ERROR=NONE/);
    assert.doesNotMatch(output(result), /UI_REQUIRED|Select UI/);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'transaction.json')), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
    }
  } finally {
    dispose(root);
  }
});

test('init accepts the pnpm argument separator used by documented commands', () => {
  const root = fixture();
  try {
    const result = run(root, 'init.mjs', ['--', '--check']);
    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_STATE=pristine/);
    assert.match(output(result), /INIT_NEXT=CHECK_COMPLETE/);
    assert.match(output(result), /INIT_ERROR=NONE/);
  } finally {
    dispose(root);
  }
});

test('workspace dry-run remains read-only even when an inherited legacy mode is present', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    const before = filesystemSnapshot(root);
    const result = run(root, 'init.mjs', [
      '--dry-run', '--ui', 'ele', '--confirm-cleanup', '--no-open',
    ], {
      GIN_VBEN_UI_SELECTION_MODE: 'legacy',
    });
    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_NEXT=PREFLIGHT_COMPLETE/);
    assert.match(output(result), /INIT_PLAN_PRESERVE=apps\/web-antd,apps\/web-ele,apps\/web-naive/);
    assert.deepEqual(filesystemSnapshot(root), before);
  } finally {
    dispose(root);
  }
});

test('init verifies the ordinary API before moving any UI template', () => {
  const root = fixture();
  try {
    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], {
      INIT_API_TEST_MODE: '',
      INIT_API_BASE_URL: 'http://127.0.0.1:1',
    });
    assert.equal(result.status, 1, output(result));
    assert.match(output(result), /INIT_ERROR=API_UNAVAILABLE/);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'transaction.json')), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
    }
  } finally {
    dispose(root);
  }
});

test('init probes health, install status, and the install page on the ordinary API', () => {
  const root = fixture();
  try {
    const probe = join(root, 'scripts', 'record-api-probe.mjs');
    const requestLog = join(root, 'api-requests.log');
    writeFileSync(probe, [
      'import { appendFileSync } from "node:fs";',
      'appendFileSync(process.env.INIT_API_REQUEST_LOG, `${process.argv.slice(2).join(" ")}\\n`);',
    ].join('\n'));
    assert.equal(existsSync(join(root, 'node_modules')), false);
    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], {
      INIT_API_TEST_MODE: '',
      INIT_API_BASE_URL: 'http://127.0.0.1:8080',
      INIT_API_PROBE_COMMAND: process.execPath,
      INIT_API_PROBE_PREFIX_ARGS: JSON.stringify([probe]),
      INIT_API_REQUEST_LOG: requestLog,
    });
    assert.equal(result.status, 0, output(result));
    assert.deepEqual(readFileSync(requestLog, 'utf8').trim().split('\n'), [
      'GET http://127.0.0.1:8080/health/live',
      'GET http://127.0.0.1:8080/api/system/install/v1/status',
      'GET http://127.0.0.1:8080/install',
    ]);
  } finally {
    dispose(root);
  }
});

test('init error boundary normalizes post-preflight filesystem races without exposing paths', () => {
  const raw = new Error("EACCES: permission denied, rename '/private/admin/.ui-profile.json.tmp-1' -> '/private/admin/.ui-profile.json'");
  assert.equal(stableInitializationErrorCode(raw), 'INITIALIZATION_OPERATION_FAILED');
  assert.equal(stableInitializationErrorCode(new Error('PREFLIGHT_FAILED')), 'PREFLIGHT_FAILED');
  assert.equal(stableInitializationErrorCode({ message: '/private/TOKEN=secret' }), 'INITIALIZATION_OPERATION_FAILED');
});

test('init stages non-selected templates, writes the fixed profile schema, and is idempotent', () => {
  const root = fixture();
  try {
    const first = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']);
    assert.equal(first.status, 0, output(first));
    assert.match(output(first), /INIT_STATE=ui_prepared/);
    assert.match(output(first), /INIT_SELECTED_UI=ele/);
    assert.match(output(first), /INIT_PREFLIGHT=ok/);
    assert.match(output(first), /INIT_PLAN_RETAIN=apps\/web-ele/);
    assert.match(output(first), /INIT_PLAN_STAGE=apps\/web-antd,apps\/web-naive/);
    assert.match(output(first), /INIT_PLAN_BACKUP=\.runtime\/install\/ui-backup\/<transaction>/);
    assert.deepEqual(
      [...output(first).matchAll(/^INIT_STAGE=(.+)$/gm)].map((match) => match[1]),
      ['prepare:preflight', 'prepare:workspace', 'prepare:dependencies', 'prepare:complete'],
    );
    assert.match(output(first), /INIT_URL=http:\/\/127\.0\.0\.1:8080\/install/);
    assert.match(output(first), /INIT_NEXT=OPEN_INSTALLER/);
    assert.match(output(first), /INIT_ERROR=NONE/);

    assert.deepEqual(JSON.parse(readFileSync(join(root, '.ui-profile.json'), 'utf8')), {
      schema: 1,
      selectedUi: 'ele',
      packageName: '@vben/web-ele',
      appDirectory: 'apps/web-ele',
    });
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
    const backups = join(root, '..', '.runtime', 'install', 'ui-backup');
    assert.equal(existsSync(backups), true);

    const repeated = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(repeated.status, 0, output(repeated));
    assert.match(output(repeated), /INIT_STATE=ui_prepared/);
    assert.match(output(repeated), /INIT_SELECTED_UI=ele/);
    assert.match(output(repeated), /INIT_NEXT=OPEN_INSTALLER/);
    assert.match(output(repeated), /INIT_ERROR=NONE/);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), true);
  } finally {
    dispose(root);
  }
});

test('init keeps all preparation state in a controlled absolute state directory supplied by Go', () => {
  const root = fixture();
  try {
    const customStateRoot = join(root, '..', '.go-runtime', 'install');
    const runner = join(root, 'scripts', 'custom-state-pnpm.mjs');
    const log = join(root, 'custom-state-pnpm.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_PNPM_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));
    const result = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open'], {
      GIN_VBEN_INSTALL_STATE_DIR: customStateRoot,
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });

    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_SELECTED_UI=ele/);
    assert.equal(readFileSync(log, 'utf8'), 'install --frozen-lockfile');
    assert.equal(existsSync(join(customStateRoot, 'dependency-install.log')), true);
    assert.equal(existsSync(join(customStateRoot, 'transaction.json')), false);
    const backupTransactions = readdirSync(join(customStateRoot, 'ui-backup'));
    assert.equal(backupTransactions.length, 1);
    assert.equal(
      existsSync(join(customStateRoot, 'ui-backup', backupTransactions[0], 'receipt.json')),
      true,
    );
    assert.equal(existsSync(join(root, '..', '.runtime', 'install')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
  } finally {
    dispose(root);
  }
});

test('init accepts an absolute installation state directory outside the workspace', () => {
  const root = fixture();
  const external = mkdtempSync(join(tmpdir(), 'gin-vben-external-state-'));
  try {
    const externalStateRoot = join(external, 'install');
    const result = run(root, 'init.mjs', ['--ui', 'naive', '--confirm-cleanup', '--no-open'], {
      GIN_VBEN_INSTALL_STATE_DIR: externalStateRoot,
    });

    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_SELECTED_UI=naive/);
    const backupTransactions = readdirSync(join(externalStateRoot, 'ui-backup'));
    assert.equal(backupTransactions.length, 1);
    assert.equal(
      existsSync(join(externalStateRoot, 'ui-backup', backupTransactions[0], 'receipt.json')),
      true,
    );
    assert.equal(existsSync(join(root, '..', '.runtime', 'install')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
  } finally {
    dispose(root);
    rmSync(external, { force: true, recursive: true });
  }
});

test('init canonicalizes an existing state-directory symlink to an external trusted directory', () => {
  const root = fixture();
  const external = mkdtempSync(join(tmpdir(), 'gin-vben-external-state-target-'));
  try {
    const externalStateRoot = join(external, 'install');
    mkdirSync(externalStateRoot);
    const configuredAlias = join(root, '..', '.go-state-link');
    symlinkSync(externalStateRoot, configuredAlias, process.platform === 'win32' ? 'junction' : 'dir');
    const result = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open'], {
      GIN_VBEN_INSTALL_STATE_DIR: configuredAlias,
    });

    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_SELECTED_UI=ele/);
    const backupTransactions = readdirSync(join(externalStateRoot, 'ui-backup'));
    assert.equal(backupTransactions.length, 1);
    assert.equal(
      existsSync(join(externalStateRoot, 'ui-backup', backupTransactions[0], 'receipt.json')),
      true,
    );
    assert.equal(existsSync(join(root, '..', '.runtime', 'install')), false);
  } finally {
    dispose(root);
    rmSync(external, { force: true, recursive: true });
  }
});

test('init rejects relative, root, workspace-root, and admin-source state directories before changing the workspace', () => {
  const cases = [
    { name: 'relative', value: () => '.go-runtime/install' },
    { name: 'filesystem root', value: (root) => parse(root).root },
    { name: 'workspace root', value: (root) => join(root, '..') },
    { name: 'admin source tree', value: (root) => join(root, 'runtime-state') },
    { name: 'UI source tree', value: (root) => join(root, 'apps', 'web-antd', '.runtime-state') },
    {
      name: 'symlinked admin source tree',
      value: (root) => {
        const alias = join(root, '..', '.admin-state-link');
        symlinkSync(root, alias, process.platform === 'win32' ? 'junction' : 'dir');
        return join(alias, '.runtime-state');
      },
    },
  ];
  for (const testCase of cases) {
    const root = fixture();
    try {
      const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], {
        GIN_VBEN_INSTALL_STATE_DIR: testCase.value(root),
      });

      assert.equal(result.status, 2, `${testCase.name}: ${output(result)}`);
      assert.match(output(result), /INIT_STATE=inconsistent/, testCase.name);
      assert.match(output(result), /INIT_REASON=INSTALL_STATE_DIR_INVALID/, testCase.name);
      assert.match(output(result), /INIT_NEXT=PROVIDE_INPUT/, testCase.name);
      assert.match(output(result), /INIT_ERROR=INSTALL_STATE_DIR_INVALID/, testCase.name);
      assert.equal(existsSync(join(root, '.ui-profile.json')), false, testCase.name);
      assert.equal(existsSync(join(root, '..', '.runtime', 'install')), false, testCase.name);
      for (const ui of ['antd', 'ele', 'naive']) {
        assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true, `${testCase.name}: ${ui}`);
      }
    } finally {
      dispose(root);
    }
  }
});

test('legacy Windows build failure is migrated and installs only the selected UI workspace', () => {
  const legacy = legacyPreparedFixture();
  const { root, transactionId } = legacy;
  try {
    const runner = join(root, 'scripts', 'legacy-pnpm.mjs');
    const log = join(root, 'legacy-pnpm.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_PNPM_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));

    const result = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });

    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_LEGACY_MIGRATION=completed/);
    assert.match(output(result), /INIT_SELECTED_UI=ele/);
    assert.equal(readFileSync(log, 'utf8'), 'install --frozen-lockfile');
    const stateRoot = join(root, '..', '.runtime', 'install');
    const migratedBackup = join(stateRoot, 'ui-backup', transactionId);
    assert.equal(existsSync(join(migratedBackup, 'apps', 'web-antd')), true);
    assert.equal(existsSync(join(migratedBackup, 'apps', 'web-naive')), true);
    assert.equal(existsSync(legacy.legacyBackup), false);
    assert.equal(existsSync(join(root, '.ui-init-receipt.json')), false);
    assert.equal(
      readFileSync(join(stateRoot, 'legacy-recovery', transactionId, '.ui-init-receipt.json'), 'utf8'),
      legacy.receiptBytes,
    );
    assert.equal(existsSync(join(stateRoot, 'legacy-prepared-migration.json')), false);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.deepEqual(JSON.parse(readFileSync(join(migratedBackup, 'receipt.json'), 'utf8')), {
      schema: 1,
      owner: 'admin-init',
      transactionId,
      selectedUi: 'ele',
      dependenciesReady: true,
      moves: legacy.moves,
    });
    assert.equal(readFileSync(legacy.historicalRecovery, 'utf8'), '{"legacy":true}\n');
    assert.equal(existsSync(join(root, 'node_modules')), false);
  } finally {
    dispose(root);
  }
});

test('legacy migration and direct reset respect a live Go apply lease before any transaction exists', async (t) => {
  const cases = [
    { name: 'migration', args: ['--no-open'] },
    { name: 'direct reset', args: ['--reset', '--confirm-reset', '--no-open'] },
  ];
  for (const testCase of cases) {
    await t.test(testCase.name, () => {
      const legacy = legacyPreparedFixture();
      const { root } = legacy;
      try {
        const applyLease = writeGoCompatibleApplyLease(root);
        const stateRoot = join(root, '..', '.runtime', 'install');
        const protectedPaths = [
          join(root, '.ui-profile.json'),
          join(root, '.ui-init-receipt.json'),
          join(root, 'apps'),
          join(root, '..', '.runtime', 'init-backup'),
          join(root, '..', '.runtime', 'init-recovery'),
          join(stateRoot, 'legacy-prepared-migration.json'),
          join(stateRoot, 'legacy-recovery'),
          join(stateRoot, 'ui-backup'),
          join(stateRoot, 'transaction.json'),
          applyLease.path,
        ];
        const before = protectedPaths.map(filesystemSnapshot);
        const pnpmLog = join(root, `go-apply-${testCase.name.replaceAll(' ', '-')}-pnpm.log`);
        const runner = join(root, 'scripts', 'go-apply-lock-pnpm.mjs');
        writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

        const result = run(root, 'init.mjs', testCase.args, {
          INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
          INIT_PNPM_COMMAND: process.execPath,
          INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
          INIT_PNPM_LOG: pnpmLog,
        });

        assert.equal(result.status, 3, output(result));
        assert.match(output(result), /INIT_ERROR=INIT_BUSY/);
        assert.equal(readFileSync(applyLease.path, 'utf8'), applyLease.contents);
        assert.equal(existsSync(pnpmLog), false);
        assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
      } finally {
        dispose(root);
      }
    });
  }
});

test('legacy migration preflight proves the real backup transfer before publishing a lease or journal', async () => {
  const fixtureState = legacyPreparedFixture({ selectedUi: 'ele' });
  const { legacyBackup, root } = fixtureState;
  try {
    const repository = fixtureRepositories.get(root);
    const before = filesystemSnapshot(repository);
    await assert.rejects(
      () => preflightLegacyPreparedMigration(root, {
        deviceOf: (target) => statSync(target).dev + (target === legacyBackup ? 1 : 0),
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'ui_backup'
        && error?.operation === 'cross_directory_rename',
    );
    assert.deepEqual(filesystemSnapshot(repository), before);
    assert.equal(existsSync(adminLeasePath(root)), false);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'legacy-prepared-migration.json')), false);
  } finally {
    dispose(root);
  }
});

test('legacy migration preflights the selected UI continuation before moving legacy state', () => {
  const fixtureState = legacyPreparedFixture({ selectedUi: 'ele' });
  const { legacyBackup, receiptBytes, root } = fixtureState;
  try {
    rmSync(join(root, 'apps', 'web-ele', '.env.production.example'));
    const result = run(root, 'init.mjs', ['--no-open']);

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=PREFLIGHT_FAILED/);
    assert.equal(readFileSync(join(root, '.ui-init-receipt.json'), 'utf8'), receiptBytes);
    assert.equal(existsSync(legacyBackup), true);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'legacy-prepared-migration.json')), false);
    assert.equal(existsSync(adminLeasePath(root)), false);
  } finally {
    dispose(root);
  }
});

test('orphan recovery preflight fails before moving local state or creating the admin lease', async () => {
  const root = fixture();
  try {
    writeFileSync(join(root, '.ui-init-receipt.json'), '{"legacy":true}\n');
    const repository = fixtureRepositories.get(root);
    const before = filesystemSnapshot(repository);
    await assert.rejects(
      () => preflightSafeLocalRecovery(root, {
        open: async (target, ...args) => {
          if (parse(String(target)).dir === root && String(target).includes('.gin-vben-preflight-')) {
            throw Object.assign(new Error('denied'), { code: 'EACCES' });
          }
          return openAsync(target, ...args);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'admin_root'
        && error?.operation === 'create',
    );
    assert.deepEqual(filesystemSnapshot(repository), before);
    assert.equal(existsSync(adminLeasePath(root)), false);
  } finally {
    dispose(root);
  }
});

test('orphan recovery with a UI choice preflights the later UI preparation before moving local state', () => {
  const root = fixture();
  try {
    const receipt = join(root, '.ui-init-receipt.json');
    const receiptBytes = '{"legacy":true}\n';
    writeFileSync(receipt, receiptBytes);
    rmSync(join(root, 'apps', 'web-antd', '.env.development.example'));

    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=PREFLIGHT_FAILED/);
    assert.equal(readFileSync(receipt, 'utf8'), receiptBytes);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'recovery')), false);
    assert.equal(existsSync(adminLeasePath(root)), false);
  } finally {
    dispose(root);
  }
});

test('apply lease inspection errors fail closed without changing state or running pnpm', () => {
  for (const code of ['EACCES', 'EPERM', 'EIO']) {
    const root = fixture();
    try {
      const repository = join(root, '..');
      const before = filesystemSnapshot(repository);
      const pnpmLog = join(root, `apply-lstat-${code}.log`);
      assert.throws(
        () => ensureInstallerApplyIdle(root, {
          lstat: () => { throw Object.assign(new Error(code), { code }); },
        }),
        /INIT_BUSY/,
      );
      assert.deepEqual(filesystemSnapshot(repository), before);
      assert.equal(existsSync(pnpmLog), false);
      assert.equal(existsSync(join(root, 'node_modules')), false);
    } finally {
      dispose(root);
    }
  }
});

test('legacy dependency execution remains covered by the admin-init handshake lease', () => {
  const legacy = legacyPreparedFixture();
  const { root } = legacy;
  try {
    const runner = join(root, 'scripts', 'observe-node-apply-lock.mjs');
    const log = join(root, 'observe-node-apply-lock.json');
    writeFileSync(runner, [
      'import { readFileSync, writeFileSync } from "node:fs";',
      'const lease = JSON.parse(readFileSync(new URL("../../.runtime/install/admin-init.lock", import.meta.url), "utf8"));',
      'writeFileSync(process.env.INIT_PNPM_LOG, JSON.stringify({ args: process.argv.slice(2), lease }));',
    ].join('\n'));

    const result = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });

    assert.equal(result.status, 0, output(result));
    const observed = JSON.parse(readFileSync(log, 'utf8'));
    assert.deepEqual(observed.args, ['install', '--frozen-lockfile']);
    assert.equal(observed.lease.schema, 2);
    assert.equal(observed.lease.owner, 'admin-init');
    assert.ok(Number.isInteger(observed.lease.pid) && observed.lease.pid > 0);
    assert.equal(new Date(observed.lease.createdAt).toISOString(), observed.lease.createdAt);
    assert.match(observed.lease.pidStartToken, /^[0-9a-f]{64}$/);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'admin-init.lock')), false);
  } finally {
    dispose(root);
  }
});

test('legacy migration resumes after interruption immediately after the backup rename', async () => {
  const legacy = legacyPreparedFixture();
  const { root, transactionId } = legacy;
  try {
    await assert.rejects(
      migrateLegacyPreparedState(root, {
        afterBackupMove: async () => { throw new Error('SIMULATED_MIGRATION_INTERRUPT'); },
      }),
      /SIMULATED_MIGRATION_INTERRUPT/,
    );

    const stateRoot = join(root, '..', '.runtime', 'install');
    const migrationPath = join(stateRoot, 'legacy-prepared-migration.json');
    assert.deepEqual(JSON.parse(readFileSync(migrationPath, 'utf8')), {
      schema: 1,
      owner: 'admin-init-legacy-migration',
      transactionId,
      selectedUi: 'ele',
      moves: legacy.moves,
    });
    assert.equal(existsSync(legacy.legacyBackup), false);
    assert.equal(existsSync(join(stateRoot, 'ui-backup', transactionId, 'apps', 'web-antd')), true);
    assert.equal(readFileSync(join(root, '.ui-init-receipt.json'), 'utf8'), legacy.receiptBytes);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.equal(readFileSync(join(root, '.ui-profile.json'), 'utf8'), legacy.profileBytes);

    const resumed = run(root, 'init.mjs', ['--no-open']);

    assert.equal(resumed.status, 0, output(resumed));
    assert.match(output(resumed), /INIT_LEGACY_MIGRATION=resumed/);
    assert.equal(existsSync(migrationPath), false);
    assert.equal(existsSync(join(root, '.ui-init-receipt.json')), false);
    assert.equal(
      readFileSync(join(stateRoot, 'legacy-recovery', transactionId, '.ui-init-receipt.json'), 'utf8'),
      legacy.receiptBytes,
    );
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.equal(existsSync(join(stateRoot, 'ui-backup', transactionId, 'receipt.json')), true);
  } finally {
    dispose(root);
  }
});

test('check reports an interrupted legacy migration without changing its checkpoint', async () => {
  const legacy = legacyPreparedFixture();
  const { root, transactionId } = legacy;
  try {
    await assert.rejects(
      migrateLegacyPreparedState(root, {
        afterBackupMove: async () => { throw new Error('SIMULATED_MIGRATION_INTERRUPT'); },
      }),
      /SIMULATED_MIGRATION_INTERRUPT/,
    );
    const stateRoot = join(root, '..', '.runtime', 'install');
    const migrationPath = join(stateRoot, 'legacy-prepared-migration.json');
    const backupPackage = join(stateRoot, 'ui-backup', transactionId, 'apps', 'web-antd', 'package.json');
    const before = {
      migration: readFileSync(migrationPath, 'utf8'),
      profile: readFileSync(join(root, '.ui-profile.json'), 'utf8'),
      receipt: readFileSync(join(root, '.ui-init-receipt.json'), 'utf8'),
      backupPackage: readFileSync(backupPackage, 'utf8'),
    };

    const checked = run(root, 'init.mjs', ['--check']);

    assert.equal(checked.status, 0, output(checked));
    assert.match(output(checked), /INIT_STATE=installing/);
    assert.match(output(checked), /INIT_REASON=LEGACY_PREPARED_MIGRATION_PENDING/);
    assert.equal(readFileSync(migrationPath, 'utf8'), before.migration);
    assert.equal(readFileSync(join(root, '.ui-profile.json'), 'utf8'), before.profile);
    assert.equal(readFileSync(join(root, '.ui-init-receipt.json'), 'utf8'), before.receipt);
    assert.equal(readFileSync(backupPackage, 'utf8'), before.backupPackage);
  } finally {
    dispose(root);
  }
});

test('invalid legacy prepared candidates remain byte-for-byte unchanged and never run pnpm', async (t) => {
  const cases = [
    {
      name: 'old installer marker',
      mutate({ root }) {
        writeFileSync(join(root, 'apps', 'install', '.installed'), 'legacy-marker\n');
      },
    },
    {
      name: 'old source move transaction',
      mutate({ root }) {
        writeFileSync(join(root, '.ui-init-transaction.json'), '{"schema":1,"legacy":true}\n');
      },
    },
    {
      name: 'old runtime receipt',
      mutate({ root }) {
        writeFileSync(join(root, '.ui-init-runtime.json'), '{"schema":1,"pid":1234}\n');
      },
    },
    {
      name: 'dangling symlink old runtime receipt',
      mutate({ root }) {
        symlinkSync('missing-runtime.json', join(root, '.ui-init-runtime.json'));
      },
    },
    {
      name: 'receipt and backup transaction mismatch',
      mutate({ root }) {
        const receiptPath = join(root, '.ui-init-receipt.json');
        const receipt = JSON.parse(readFileSync(receiptPath, 'utf8'));
        receipt.transactionId = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee';
        writeFileSync(receiptPath, `${JSON.stringify(receipt, null, 2)}\n`);
      },
    },
    {
      name: 'symlinked legacy template backup',
      mutate({ root, legacyBackup }) {
        const backup = join(legacyBackup, 'apps', 'web-antd');
        rmSync(backup, { recursive: true });
        symlinkSync(join(root, 'apps', 'web-ele'), backup, process.platform === 'win32' ? 'junction' : 'dir');
      },
    },
    {
      name: 'unexpected legacy backup entry',
      mutate({ legacyBackup }) {
        writeFileSync(join(legacyBackup, 'unexpected.txt'), 'preserve me\n');
      },
    },
    {
      name: 'current transaction conflict',
      mutate({ root }) {
        const stateRoot = join(root, '..', '.runtime', 'install');
        mkdirSync(stateRoot, { recursive: true });
        writeFileSync(join(stateRoot, 'transaction.json'), '{"schema":1,"owner":"server-installer"}\n');
      },
    },
    {
      name: 'dangling symlink current transaction conflict',
      mutate({ root }) {
        const stateRoot = join(root, '..', '.runtime', 'install');
        mkdirSync(stateRoot, { recursive: true });
        symlinkSync('missing-transaction.json', join(stateRoot, 'transaction.json'));
      },
    },
    {
      name: 'current marker conflict',
      mutate({ root }) {
        const stateRoot = join(root, '..', '.runtime', 'install');
        mkdirSync(stateRoot, { recursive: true });
        writeFileSync(join(stateRoot, '.installed'), `${JSON.stringify({
          schema_version: 1,
          installer_version: '0.4.0-dev',
          installed_at: '2026-08-24T00:00:00Z',
          selected_ui: 'ele',
          mode: 'dev',
          artifact_hash: 'a'.repeat(64),
          manifest_hash: 'b'.repeat(64),
        })}\n`);
      },
    },
    {
      name: 'unrelated current backup conflict',
      mutate({ root }) {
        const conflicting = join(root, '..', '.runtime', 'install', 'ui-backup', 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee');
        mkdirSync(conflicting, { recursive: true });
        writeFileSync(join(conflicting, 'preserve.txt'), 'current backup conflict\n');
      },
    },
    {
      name: 'legacy recovery parent is a regular file',
      mutate({ root }) {
        const stateRoot = join(root, '..', '.runtime', 'install');
        mkdirSync(stateRoot, { recursive: true });
        writeFileSync(join(stateRoot, 'legacy-recovery'), 'preserve recovery parent\n');
      },
    },
    {
      name: 'legacy recovery parent is a symlink',
      mutate({ root }) {
        const stateRoot = join(root, '..', '.runtime', 'install');
        const outside = join(root, 'legacy-recovery-outside');
        mkdirSync(stateRoot, { recursive: true });
        mkdirSync(outside, { recursive: true });
        writeFileSync(join(outside, 'preserve.txt'), 'outside recovery target\n');
        symlinkSync(outside, join(stateRoot, 'legacy-recovery'), process.platform === 'win32' ? 'junction' : 'dir');
      },
    },
  ];

  for (const testCase of cases) {
    await t.test(testCase.name, () => {
      const legacy = legacyPreparedFixture();
      const { root } = legacy;
      try {
        testCase.mutate(legacy);
        const stateRoot = join(root, '..', '.runtime', 'install');
        const protectedPaths = [
          join(root, '.ui-profile.json'),
          join(root, '.ui-init-receipt.json'),
          join(root, '.ui-init-transaction.json'),
          join(root, '.ui-init-runtime.json'),
          join(root, 'apps', 'install', '.installed'),
          join(root, 'legacy-recovery-outside'),
          join(root, '..', '.runtime', 'init-backup'),
          join(root, '..', '.runtime', 'init-recovery'),
          join(stateRoot, 'legacy-prepared-migration.json'),
          join(stateRoot, 'legacy-recovery'),
          join(stateRoot, 'ui-backup'),
          join(stateRoot, 'transaction.json'),
          join(stateRoot, '.installed'),
        ];
        const before = protectedPaths.map(filesystemSnapshot);
        const pnpmLog = join(root, 'invalid-legacy-pnpm.log');
        const runner = join(root, 'scripts', 'invalid-legacy-pnpm.mjs');
        writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

        const checked = run(root, 'init.mjs', ['--check']);
        assert.equal(checked.status, 3, output(checked));
        assert.match(output(checked), /INIT_STATE=inconsistent/);
        assert.match(output(checked), /INIT_REASON=LEGACY_PREPARED_STATE_INVALID/);
        assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);

        const result = run(root, 'init.mjs', ['--no-open'], {
          INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
          INIT_PNPM_COMMAND: process.execPath,
          INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
          INIT_PNPM_LOG: pnpmLog,
        });

        assert.equal(result.status, 3, output(result));
        assert.match(output(result), /INIT_STATE=inconsistent/);
        assert.match(output(result), /INIT_REASON=LEGACY_PREPARED_STATE_INVALID/);
        assert.equal(existsSync(pnpmLog), false);
        assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
      } finally {
        dispose(root);
      }
    });
  }
});

test('legacy migration compares receipt fields semantically instead of by JSON key order', () => {
  const legacy = legacyPreparedFixture();
  const { root } = legacy;
  try {
    const reordered = {
      moves: legacy.receipt.moves.map((move) => ({ backup: move.backup, source: move.source })),
      selectedUi: legacy.receipt.selectedUi,
      transactionId: legacy.receipt.transactionId,
      schema: legacy.receipt.schema,
    };
    const reorderedBytes = `${JSON.stringify(reordered, null, 2)}\n`;
    writeFileSync(join(root, '.ui-init-receipt.json'), reorderedBytes);

    const result = run(root, 'init.mjs', ['--no-open']);

    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_LEGACY_MIGRATION=completed/);
    assert.equal(
      readFileSync(join(root, '..', '.runtime', 'install', 'legacy-recovery', legacy.transactionId, '.ui-init-receipt.json'), 'utf8'),
      reorderedBytes,
    );
  } finally {
    dispose(root);
  }
});

test('legacy migration resume validates selected and unselected source layout before advancing', async (t) => {
  const cases = [
    {
      name: 'selected app disappeared',
      mutate(root) {
        rmSync(join(root, 'apps', 'web-ele'), { recursive: true });
      },
    },
    {
      name: 'unselected app reappeared',
      mutate(root) {
        mkdirSync(join(root, 'apps', 'web-antd'), { recursive: true });
        writeFileSync(join(root, 'apps', 'web-antd', 'conflict.txt'), 'preserve conflict\n');
      },
    },
    {
      name: 'old runtime appears',
      mutate(root) {
        writeFileSync(join(root, '.ui-init-runtime.json'), '{"schema":1,"pid":1234}\n');
      },
    },
    {
      name: 'dangling old runtime symlink appears',
      mutate(root) {
        symlinkSync('missing-runtime.json', join(root, '.ui-init-runtime.json'));
      },
    },
  ];

  for (const testCase of cases) {
    await t.test(testCase.name, async () => {
      const legacy = legacyPreparedFixture();
      const { root, transactionId } = legacy;
      try {
        await assert.rejects(
          migrateLegacyPreparedState(root, {
            afterBackupMove: async () => { throw new Error('SIMULATED_MIGRATION_INTERRUPT'); },
          }),
          /SIMULATED_MIGRATION_INTERRUPT/,
        );
        testCase.mutate(root);
        const stateRoot = join(root, '..', '.runtime', 'install');
        const protectedPaths = [
          join(root, '.ui-profile.json'),
          join(root, '.ui-init-receipt.json'),
          join(root, '.ui-init-runtime.json'),
          join(root, 'apps'),
          join(stateRoot, 'legacy-prepared-migration.json'),
          join(stateRoot, 'ui-backup', transactionId),
          join(stateRoot, 'transaction.json'),
          join(stateRoot, 'legacy-recovery'),
        ];
        const before = protectedPaths.map(filesystemSnapshot);
        const pnpmLog = join(root, 'invalid-resume-pnpm.log');
        const runner = join(root, 'scripts', 'invalid-resume-pnpm.mjs');
        writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

        const checked = run(root, 'init.mjs', ['--check']);
        assert.equal(checked.status, 3, output(checked));
        assert.match(output(checked), /INIT_STATE=inconsistent/);
        assert.match(output(checked), /INIT_REASON=LEGACY_PREPARED_STATE_INVALID/);
        assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);

        const result = run(root, 'init.mjs', ['--no-open'], {
          INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
          INIT_PNPM_COMMAND: process.execPath,
          INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
          INIT_PNPM_LOG: pnpmLog,
        });

        assert.equal(result.status, 3, output(result));
        assert.match(output(result), /INIT_ERROR=LEGACY_MIGRATION_INVALID/);
        assert.equal(existsSync(pnpmLog), false);
        assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
      } finally {
        dispose(root);
      }
    });
  }
});

test('legacy migration survives dependency failure and rerun only retries the selected workspace install', () => {
  const legacy = legacyPreparedFixture();
  const { root, transactionId } = legacy;
  try {
    const failingRunner = join(root, 'scripts', 'legacy-fail-pnpm.mjs');
    writeFileSync(failingRunner, 'process.exit(29);\n');

    const failed = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([failingRunner]),
    });

    assert.equal(failed.status, 1, output(failed));
    assert.match(output(failed), /INIT_LEGACY_MIGRATION=completed/);
    assert.match(output(failed), /INIT_ERROR=DEPENDENCY_INSTALL_FAILED/);
    const stateRoot = join(root, '..', '.runtime', 'install');
    const transactionPath = join(stateRoot, 'transaction.json');
    assert.deepEqual(JSON.parse(readFileSync(transactionPath, 'utf8')), {
      schema: 1,
      owner: 'admin-init',
      id: transactionId,
      selectedUi: 'ele',
      phase: 'dependencies_pending',
      moves: legacy.moves,
    });
    assert.equal(existsSync(join(stateRoot, 'legacy-prepared-migration.json')), false);
    assert.equal(existsSync(join(stateRoot, 'ui-backup', transactionId, 'receipt.json')), false);
    assert.equal(
      readFileSync(join(stateRoot, 'legacy-recovery', transactionId, '.ui-init-receipt.json'), 'utf8'),
      legacy.receiptBytes,
    );

    const successfulRunner = join(root, 'scripts', 'legacy-retry-pnpm.mjs');
    const retryLog = join(root, 'legacy-retry-pnpm.log');
    writeFileSync(successfulRunner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_PNPM_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));
    const retried = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([successfulRunner]),
      INIT_PNPM_LOG: retryLog,
    });

    assert.equal(retried.status, 0, output(retried));
    assert.doesNotMatch(output(retried), /INIT_LEGACY_MIGRATION=/);
    assert.equal(readFileSync(retryLog, 'utf8'), 'install --frozen-lockfile');
    assert.equal(existsSync(transactionPath), false);
    assert.equal(existsSync(join(stateRoot, 'ui-backup', transactionId, 'receipt.json')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
  } finally {
    dispose(root);
  }
});

test('reset after legacy migration restores all three UI templates', () => {
  const legacy = legacyPreparedFixture();
  const { root, transactionId } = legacy;
  try {
    const migrated = run(root, 'init.mjs', ['--no-open']);
    assert.equal(migrated.status, 0, output(migrated));
    const stateRoot = join(root, '..', '.runtime', 'install');
    const isolatedReceipt = join(stateRoot, 'legacy-recovery', transactionId, '.ui-init-receipt.json');
    assert.equal(readFileSync(isolatedReceipt, 'utf8'), legacy.receiptBytes);

    const reset = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);

    assert.equal(reset.status, 0, output(reset));
    assert.match(output(reset), /INIT_STATE=pristine/);
    assert.match(output(reset), /INIT_NEXT=RESET_COMPLETE/);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
    }
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    assert.equal(existsSync(join(root, '.ui-init-receipt.json')), false);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.equal(existsSync(join(stateRoot, 'ui-backup', transactionId)), false);
    assert.equal(readFileSync(isolatedReceipt, 'utf8'), legacy.receiptBytes);
    assert.equal(readFileSync(legacy.historicalRecovery, 'utf8'), '{"legacy":true}\n');
  } finally {
    dispose(root);
  }
});

test('reset directly restores an exact legacy partial without installing dependencies', () => {
  const legacy = legacyPreparedFixture();
  const { root, transactionId } = legacy;
  try {
    const pnpmLog = join(root, 'legacy-direct-reset-pnpm.log');
    const runner = join(root, 'scripts', 'legacy-direct-reset-pnpm.mjs');
    writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

    const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: pnpmLog,
    });

    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_LEGACY_MIGRATION=completed/);
    assert.match(output(result), /INIT_NEXT=RESET_COMPLETE/);
    assert.equal(existsSync(pnpmLog), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
    }
    const stateRoot = join(root, '..', '.runtime', 'install');
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    assert.equal(existsSync(join(root, '.ui-init-receipt.json')), false);
    assert.equal(existsSync(join(stateRoot, 'legacy-prepared-migration.json')), false);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.equal(existsSync(join(stateRoot, 'ui-backup', transactionId)), false);
    assert.equal(
      readFileSync(join(stateRoot, 'legacy-recovery', transactionId, '.ui-init-receipt.json'), 'utf8'),
      legacy.receiptBytes,
    );
  } finally {
    dispose(root);
  }
});

test('legacy reset preflight does not require forward-only selected UI inputs', async () => {
  const legacy = legacyPreparedFixture();
  try {
    const selectedDirectory = join(legacy.root, 'apps', 'web-ele');
    rmSync(join(selectedDirectory, '.env.development.example'));
    rmSync(join(selectedDirectory, '.env.production.example'));
    const selectedProbePrefix = join(selectedDirectory, '.gin-vben-preflight-');
    await assert.doesNotReject(() => preflightLegacyPreparedMigration(legacy.root, {
      resetAfterMigration: true,
      platform: 'linux',
      link: async (source, target) => {
        if (String(source).startsWith(selectedProbePrefix)) {
          throw new Error('selected UI link should not be probed for reset');
        }
        return linkAsync(source, target);
      },
    }));
  } finally {
    dispose(legacy.root);
  }
});

test('legacy partial reset requires confirmation before writing any migration state', () => {
  const legacy = legacyPreparedFixture();
  const { root } = legacy;
  try {
    const stateRoot = join(root, '..', '.runtime', 'install');
    const protectedPaths = [
      join(root, '.ui-profile.json'),
      join(root, '.ui-init-receipt.json'),
      join(root, 'apps'),
      join(root, '..', '.runtime', 'init-backup'),
      join(root, '..', '.runtime', 'init-recovery'),
      join(stateRoot, 'legacy-prepared-migration.json'),
      join(stateRoot, 'legacy-recovery'),
      join(stateRoot, 'ui-backup'),
      join(stateRoot, 'transaction.json'),
    ];
    const before = protectedPaths.map(filesystemSnapshot);
    const pnpmLog = join(root, 'legacy-unconfirmed-reset-pnpm.log');
    const runner = join(root, 'scripts', 'legacy-unconfirmed-reset-pnpm.mjs');
    writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

    const result = run(root, 'init.mjs', ['--reset', '--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: pnpmLog,
    });

    assert.equal(result.status, 2, output(result));
    assert.match(output(result), /INIT_ERROR=RESET_CONFIRMATION_REQUIRED/);
    assert.equal(existsSync(pnpmLog), false);
    assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
  } finally {
    dispose(root);
  }
});

test('legacy partial reset resumes after receipt isolation and transaction publication crash windows', async (t) => {
  const cases = [
    {
      name: 'receipt isolation',
      hook: 'afterReceiptIsolation',
      assertCheckpoint(root, transactionId) {
        const stateRoot = join(root, '..', '.runtime', 'install');
        assert.equal(existsSync(join(root, '.ui-init-receipt.json')), false);
        assert.equal(existsSync(join(stateRoot, 'legacy-recovery', transactionId, '.ui-init-receipt.json')), true);
        assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
      },
    },
    {
      name: 'transaction publication',
      hook: 'afterTransactionPublish',
      assertCheckpoint(root, transactionId) {
        const transaction = JSON.parse(readFileSync(join(root, '..', '.runtime', 'install', 'transaction.json'), 'utf8'));
        assert.equal(transaction.id, transactionId);
        assert.equal(transaction.phase, 'dependencies_pending');
      },
    },
  ];

  for (const testCase of cases) {
    await t.test(testCase.name, async () => {
      const legacy = legacyPreparedFixture();
      const { root, transactionId } = legacy;
      try {
        await assert.rejects(
          migrateLegacyPreparedState(root, {
            [testCase.hook]: async () => { throw new Error('SIMULATED_MIGRATION_INTERRUPT'); },
          }),
          /SIMULATED_MIGRATION_INTERRUPT/,
        );
        testCase.assertCheckpoint(root, transactionId);
        assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'legacy-prepared-migration.json')), true);
        const stateRoot = join(root, '..', '.runtime', 'install');
        const checkpointPaths = [
          join(root, '.ui-profile.json'),
          join(root, '.ui-init-receipt.json'),
          join(root, 'apps'),
          join(stateRoot, 'legacy-prepared-migration.json'),
          join(stateRoot, 'legacy-recovery'),
          join(stateRoot, 'ui-backup'),
          join(stateRoot, 'transaction.json'),
        ];
        const checkpoint = checkpointPaths.map(filesystemSnapshot);
        const checked = run(root, 'init.mjs', ['--check']);
        assert.equal(checked.status, 0, output(checked));
        assert.match(output(checked), /INIT_REASON=LEGACY_PREPARED_MIGRATION_PENDING/);
        assert.deepEqual(checkpointPaths.map(filesystemSnapshot), checkpoint);

        const pnpmLog = join(root, `${testCase.name.replaceAll(' ', '-')}-pnpm.log`);
        const runner = join(root, 'scripts', 'legacy-reset-window-pnpm.mjs');
        writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');
        const resumed = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open'], {
          INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
          INIT_PNPM_COMMAND: process.execPath,
          INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
          INIT_PNPM_LOG: pnpmLog,
        });

        assert.equal(resumed.status, 0, output(resumed));
        assert.match(output(resumed), /INIT_LEGACY_MIGRATION=resumed/);
        assert.match(output(resumed), /INIT_NEXT=RESET_COMPLETE/);
        assert.equal(existsSync(pnpmLog), false);
        for (const ui of ['antd', 'ele', 'naive']) {
          assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
        }
        assert.equal(existsSync(join(stateRoot, 'legacy-prepared-migration.json')), false);
        assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
        assert.equal(existsSync(join(stateRoot, 'ui-backup', transactionId)), false);
      } finally {
        dispose(root);
      }
    });
  }
});

test('legacy migration crash hooks are not replayed after their mutation is durable', async (t) => {
  for (const hook of ['afterReceiptIsolation', 'afterTransactionPublish']) {
    await t.test(hook, async () => {
      const legacy = legacyPreparedFixture();
      try {
        let calls = 0;
        const interruptOnce = async () => {
          calls += 1;
          if (calls === 1) throw new Error('SIMULATED_MIGRATION_INTERRUPT');
          throw new Error('MIGRATION_HOOK_REPLAYED');
        };
        await assert.rejects(
          migrateLegacyPreparedState(legacy.root, { [hook]: interruptOnce }),
          /SIMULATED_MIGRATION_INTERRUPT/,
        );
        await assert.doesNotReject(migrateLegacyPreparedState(legacy.root, { [hook]: interruptOnce }));
        assert.equal(calls, 1);
        assert.equal(existsSync(join(legacy.root, '..', '.runtime', 'install', 'legacy-prepared-migration.json')), false);
      } finally {
        dispose(legacy.root);
      }
    });
  }
});

test('legacy dependencies-pending checkpoint resets after the migration journal is already removed', async () => {
  const legacy = legacyPreparedFixture();
  try {
    await migrateLegacyPreparedState(legacy.root);
    const stateRoot = join(legacy.root, '..', '.runtime', 'install');
    assert.equal(existsSync(join(stateRoot, 'legacy-prepared-migration.json')), false);
    assert.equal(JSON.parse(readFileSync(join(stateRoot, 'transaction.json'), 'utf8')).phase, 'dependencies_pending');

    const result = run(legacy.root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(result.status, 0, output(result));
    assert.doesNotMatch(output(result), /INIT_LEGACY_MIGRATION=/);
    assert.match(output(result), /INIT_NEXT=RESET_COMPLETE/);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(legacy.root, 'apps', `web-${ui}`)), true);
    }
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.equal(existsSync(join(stateRoot, 'ui-backup', legacy.transactionId)), false);
  } finally {
    dispose(legacy.root);
  }
});

test('dependencies-pending direct reset removes a valid atomic receipt temp', async () => {
  const legacy = legacyPreparedFixture();
  try {
    await migrateLegacyPreparedState(legacy.root);
    const stateRoot = join(legacy.root, '..', '.runtime', 'install');
    const transactionDirectory = join(stateRoot, 'ui-backup', legacy.transactionId);
    const temporary = join(transactionDirectory, 'receipt.json.tmp-4242-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee');
    writeFileSync(temporary, '{"partial":true}\n');

    const result = run(legacy.root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_NEXT=RESET_COMPLETE/);
    assert.equal(existsSync(temporary), false);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.equal(existsSync(transactionDirectory), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(legacy.root, 'apps', `web-${ui}`)), true);
    }
  } finally {
    dispose(legacy.root);
  }
});

test('dependencies-pending direct reset rejects malformed and symlinked receipt temps unchanged', async (t) => {
  const cases = [
    {
      name: 'malformed name',
      create(root, transactionDirectory) {
        writeFileSync(join(transactionDirectory, 'receipt.json.tmp-not-a-transaction'), 'preserve malformed temp\n');
      },
    },
    {
      name: 'symlink',
      create(root, transactionDirectory) {
        symlinkSync(
          join(root, '.ui-profile.json'),
          join(transactionDirectory, 'receipt.json.tmp-4242-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'),
        );
      },
    },
  ];

  for (const testCase of cases) {
    await t.test(testCase.name, async () => {
      const legacy = legacyPreparedFixture();
      try {
        await migrateLegacyPreparedState(legacy.root);
        const stateRoot = join(legacy.root, '..', '.runtime', 'install');
        const transactionDirectory = join(stateRoot, 'ui-backup', legacy.transactionId);
        testCase.create(legacy.root, transactionDirectory);
        const protectedPaths = [
          join(legacy.root, '.ui-profile.json'),
          join(legacy.root, 'apps'),
          join(stateRoot, 'transaction.json'),
          transactionDirectory,
        ];
        const before = protectedPaths.map(filesystemSnapshot);

        const result = run(legacy.root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
        assert.equal(result.status, 3, output(result));
        assert.match(output(result), /INIT_ERROR=INITIALIZATION_RESUME_INVALID/);
        assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
      } finally {
        dispose(legacy.root);
      }
    });
  }
});

test('legacy reset resumes after one template was restored without a dependency receipt', async () => {
  const legacy = legacyPreparedFixture();
  try {
    await migrateLegacyPreparedState(legacy.root);
    const stateRoot = join(legacy.root, '..', '.runtime', 'install');
    const transactionPath = join(stateRoot, 'transaction.json');
    const transaction = JSON.parse(readFileSync(transactionPath, 'utf8'));
    transaction.phase = 'resetting_ui';
    writeFileSync(transactionPath, `${JSON.stringify(transaction, null, 2)}\n`);
    renameSync(
      join(stateRoot, 'ui-backup', legacy.transactionId, 'apps', 'web-antd'),
      join(legacy.root, 'apps', 'web-antd'),
    );
    assert.equal(existsSync(join(stateRoot, 'ui-backup', legacy.transactionId, 'receipt.json')), false);

    const resumed = run(legacy.root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(resumed.status, 0, output(resumed));
    assert.match(output(resumed), /INIT_NEXT=RESET_COMPLETE/);
    assert.equal(existsSync(transactionPath), false);
    assert.equal(existsSync(join(stateRoot, 'ui-backup', legacy.transactionId)), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(legacy.root, 'apps', `web-${ui}`)), true);
    }
  } finally {
    dispose(legacy.root);
  }
});

test('reset directly reverses moving and dependencies-pending UI transactions', async (t) => {
  for (const phase of ['moving_ui', 'dependencies_pending']) {
    await t.test(phase, () => {
      const root = fixture();
      try {
        const transaction = {
          schema: 1,
          owner: 'admin-init',
          id: '12345678-1234-1234-1234-123456789abc',
          selectedUi: 'antd',
          phase,
          moves: [
            { source: 'apps/web-ele', backup: 'apps/web-ele' },
            { source: 'apps/web-naive', backup: 'apps/web-naive' },
          ],
        };
        const stateRoot = join(root, '..', '.runtime', 'install');
        const backup = join(stateRoot, 'ui-backup', transaction.id, 'apps');
        mkdirSync(backup, { recursive: true });
        renameSync(join(root, 'apps', 'web-ele'), join(backup, 'web-ele'));
        if (phase === 'dependencies_pending') {
          renameSync(join(root, 'apps', 'web-naive'), join(backup, 'web-naive'));
          writeFileSync(join(root, '.ui-profile.json'), `${JSON.stringify({
            schema: 1,
            selectedUi: 'antd',
            packageName: '@vben/web-antd',
            appDirectory: 'apps/web-antd',
          }, null, 2)}\n`);
        }
        writeFileSync(join(stateRoot, 'transaction.json'), `${JSON.stringify(transaction, null, 2)}\n`);
        const pnpmLog = join(root, `${phase}-reset-pnpm.log`);
        const runner = join(root, 'scripts', `${phase}-reset-pnpm.mjs`);
        writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

        const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open'], {
          INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
          INIT_PNPM_COMMAND: process.execPath,
          INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
          INIT_PNPM_LOG: pnpmLog,
        });

        assert.equal(result.status, 0, output(result));
        assert.match(output(result), /INIT_NEXT=RESET_COMPLETE/);
        assert.equal(existsSync(pnpmLog), false);
        assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
        assert.equal(existsSync(join(stateRoot, 'ui-backup', transaction.id)), false);
        assert.equal(existsSync(join(root, '.ui-profile.json')), false);
        for (const ui of ['antd', 'ele', 'naive']) {
          assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
        }
      } finally {
        dispose(root);
      }
    });
  }
});

test('prepared reset rejects a symlinked admin apps parent without moving backup templates outside the workspace', () => {
  const root = fixture();
  const externalParent = mkdtempSync(join(tmpdir(), 'gin-vben-external-apps-reset-'));
  try {
    const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(prepared.status, 0, output(prepared));
    const stateRoot = join(root, '..', '.runtime', 'install');
    const externalApps = join(externalParent, 'apps');
    renameSync(join(root, 'apps'), externalApps);
    symlinkSync(externalApps, join(root, 'apps'));
    const protectedPaths = [
      join(root, 'apps'),
      externalApps,
      join(stateRoot, 'ui-backup'),
      join(stateRoot, 'transaction.json'),
      join(root, '.ui-profile.json'),
    ];
    const before = protectedPaths.map(filesystemSnapshot);

    const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=RESET_LAYOUT_INVALID/);
    assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
  } finally {
    rmSync(externalParent, { force: true, recursive: true });
    dispose(root);
  }
});

test('reset phase transition is durable when interrupted before restoring a moving UI', async () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backup = join(stateRoot, 'ui-backup', transaction.id, 'apps');
    mkdirSync(backup, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backup, 'web-ele'));
    const transactionPath = join(stateRoot, 'transaction.json');
    writeFileSync(transactionPath, `${JSON.stringify(transaction, null, 2)}\n`);

    await assert.rejects(
      resetInitialization(root, {
        afterTransition: async () => { throw new Error('SIMULATED_RESET_INTERRUPT'); },
      }),
      /SIMULATED_RESET_INTERRUPT/,
    );
    assert.equal(JSON.parse(readFileSync(transactionPath, 'utf8')).phase, 'resetting_ui');
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), true);

    const resumed = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(resumed.status, 0, output(resumed));
    assert.match(output(resumed), /INIT_NEXT=RESET_COMPLETE/);
    assert.equal(existsSync(transactionPath), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
    }
  } finally {
    dispose(root);
  }
});

test('reset phase transition preserves a server transaction that wins before publication', async () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backup = join(stateRoot, 'ui-backup', transaction.id, 'apps');
    mkdirSync(backup, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backup, 'web-ele'));
    const transactionPath = join(stateRoot, 'transaction.json');
    writeFileSync(transactionPath, `${JSON.stringify(transaction, null, 2)}\n`);
    const serverBytes = `${JSON.stringify({
      schema: 1,
      owner: 'server-installer',
      id: transaction.id,
      selectedUi: 'antd',
      mode: 'dev',
      phase: 'applying',
      currentStep: 'database',
    })}\n`;

    await assert.rejects(
      resetInitialization(root, {
        beforeTransition: async () => writeFileSync(transactionPath, serverBytes),
      }),
      /INIT_BUSY/,
    );

    assert.equal(readFileSync(transactionPath, 'utf8'), serverBytes);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), true);
  } finally {
    dispose(root);
  }
});

test('init installs the reduced workspace with a frozen lockfile after UI selection', () => {
  const root = fixture();
  try {
    const runner = join(root, 'scripts', 'fake-pnpm.mjs');
    const log = join(root, 'pnpm.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_PNPM_LOG, process.argv.slice(2).join(" "));',
      'console.log("PINNED_DEPENDENCY_RUNNER");',
    ].join('\n'));
    const result = run(root, 'init.mjs', ['--ui', 'naive', '--confirm-cleanup', '--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });
    assert.equal(result.status, 0, output(result));
    assert.equal(readFileSync(log, 'utf8'), 'install --frozen-lockfile');
    assert.match(output(result), /INIT_DEPENDENCY_LOG=\.runtime\/install\/dependency-install\.log/);
    assert.match(
      readFileSync(join(root, '..', '.runtime', 'install', 'dependency-install.log'), 'utf8'),
      /PINNED_DEPENDENCY_RUNNER/,
    );
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
  } finally {
    dispose(root);
  }
});

test('dependency log is announced only after the log file opens successfully', () => {
  const root = fixture();
  try {
    const logPath = join(root, '..', '.runtime', 'install', 'dependency-install.log');
    mkdirSync(logPath, { recursive: true });
    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
    });
    assert.equal(result.status, 1, output(result));
    assert.match(output(result), /INIT_ERROR=DEPENDENCY_INSTALL_FAILED/);
    assert.doesNotMatch(output(result), /INIT_DEPENDENCY_LOG=/);
  } finally {
    dispose(root);
  }
});

test('an interrupted dependency install keeps the selected UI and rerun only retries dependencies', () => {
  const root = fixture();
  try {
    const failingRunner = join(root, 'scripts', 'fail-pnpm.mjs');
    writeFileSync(failingRunner, 'process.exit(23);\n');
    const first = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([failingRunner]),
    });
    assert.equal(first.status, 1, output(first));
    assert.match(output(first), /INIT_ERROR=DEPENDENCY_INSTALL_FAILED/);
    assert.match(
      readFileSync(join(root, '..', '.runtime', 'install', 'dependency-install.log'), 'utf8'),
      /DEPENDENCY_SUPERVISOR_ERROR=DEPENDENCY_INSTALL_FAILED/,
    );
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);

    const transactionPath = join(root, '..', '.runtime', 'install', 'transaction.json');
    const dependencyLeasePath = join(root, '..', '.runtime', 'install', 'dependency-install.lock');
    assert.equal(existsSync(dependencyLeasePath), true);
    const failedDependencyLease = JSON.parse(readFileSync(dependencyLeasePath, 'utf8'));
    assert.equal(
      existsSync(join(root, '..', '.runtime', 'install', 'dependency-install-heartbeat', `${failedDependencyLease.id}.json`)),
      false,
    );
    assert.deepEqual(JSON.parse(readFileSync(transactionPath, 'utf8')), {
      schema: 1,
      owner: 'admin-init',
      id: JSON.parse(readFileSync(transactionPath, 'utf8')).id,
      selectedUi: 'ele',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-antd', backup: 'apps/web-antd' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    });

    const successfulRunner = join(root, 'scripts', 'successful-pnpm.mjs');
    const log = join(root, 'pnpm-retry.log');
    writeFileSync(successfulRunner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_PNPM_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));
    const retried = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([successfulRunner]),
      INIT_PNPM_LOG: log,
    });
    assert.equal(retried.status, 0, output(retried));
    assert.equal(readFileSync(log, 'utf8'), 'install --frozen-lockfile');
    assert.equal(existsSync(transactionPath), false);
    assert.match(output(retried), /INIT_SELECTED_UI=ele/);
    assert.match(output(retried), /INIT_NEXT=OPEN_INSTALLER/);
  } finally {
    dispose(root);
  }
});

test('dependency retry removes only a transaction-scoped receipt temp and does not reinstall again', () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const transactionDirectory = join(stateRoot, 'ui-backup', transaction.id);
    mkdirSync(join(transactionDirectory, 'apps'), { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(transactionDirectory, 'apps', 'web-ele'));
    renameSync(join(root, 'apps', 'web-naive'), join(transactionDirectory, 'apps', 'web-naive'));
    writeFileSync(join(root, '.ui-profile.json'), JSON.stringify({
      schema: 1, selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd',
    }));
    writeFileSync(join(stateRoot, 'transaction.json'), JSON.stringify(transaction));
    const staleTemp = join(transactionDirectory, 'receipt.json.tmp-4242-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee');
    writeFileSync(staleTemp, '{"partial":');
    const runner = join(root, 'scripts', 'receipt-temp-pnpm.mjs');
    const log = join(root, 'receipt-temp-pnpm.log');
    writeFileSync(runner, [
      'import { appendFileSync } from "node:fs";',
      'appendFileSync(process.env.INIT_PNPM_LOG, "install\\n");',
    ].join('\n'));
    const env = {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    };

    const recovered = run(root, 'init.mjs', ['--no-open'], env);
    assert.equal(recovered.status, 0, output(recovered));
    assert.equal(existsSync(staleTemp), false);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);

    const repeated = run(root, 'init.mjs', ['--no-open'], env);
    assert.equal(repeated.status, 0, output(repeated));
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), ['install']);
  } finally {
    dispose(root);
  }
});

test('resume after the dependency receipt commit skips pnpm and only completes the transaction', () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const transactionDirectory = join(stateRoot, 'ui-backup', transaction.id);
    mkdirSync(join(transactionDirectory, 'apps'), { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(transactionDirectory, 'apps', 'web-ele'));
    renameSync(join(root, 'apps', 'web-naive'), join(transactionDirectory, 'apps', 'web-naive'));
    writeFileSync(join(root, '.ui-profile.json'), JSON.stringify({
      schema: 1, selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd',
    }));
    writeFileSync(join(stateRoot, 'transaction.json'), JSON.stringify(transaction));
    writeFileSync(join(transactionDirectory, 'receipt.json'), JSON.stringify({
      schema: 1,
      owner: 'admin-init',
      transactionId: transaction.id,
      selectedUi: transaction.selectedUi,
      dependenciesReady: true,
      moves: transaction.moves,
    }));
    const pnpmLog = join(root, 'committed-receipt-pnpm.log');
    const runner = join(root, 'scripts', 'committed-receipt-pnpm.mjs');
    writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');
    const env = {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: pnpmLog,
    };

    const supervisor = run(root, 'dependency-supervisor.mjs', [], env);
    assert.equal(supervisor.status, 0, output(supervisor));
    assert.equal(existsSync(pnpmLog), false);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), true);

    const result = run(root, 'init.mjs', ['--no-open'], env);

    assert.equal(result.status, 0, output(result));
    assert.equal(existsSync(pnpmLog), false);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.match(output(result), /INIT_STATE=ui_prepared/);
    assert.match(output(result), /INIT_NEXT=OPEN_INSTALLER/);
  } finally {
    dispose(root);
  }
});

test('concurrent init processes execute dependency installation only once', async () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backup = join(stateRoot, 'ui-backup', transaction.id, 'apps');
    mkdirSync(backup, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backup, 'web-ele'));
    renameSync(join(root, 'apps', 'web-naive'), join(backup, 'web-naive'));
    writeFileSync(join(root, '.ui-profile.json'), JSON.stringify({
      schema: 1, selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd',
    }));
    writeFileSync(join(stateRoot, 'transaction.json'), JSON.stringify(transaction));

    const runner = join(root, 'scripts', 'slow-pnpm.mjs');
    const active = join(root, 'pnpm-active.lock');
    const log = join(root, 'pnpm-concurrency.log');
    writeFileSync(runner, [
      'import { appendFileSync, openSync, closeSync, rmSync } from "node:fs";',
      'let handle;',
      'try { handle = openSync(process.env.INIT_PNPM_ACTIVE, "wx"); } catch {',
      '  appendFileSync(process.env.INIT_PNPM_LOG, "OVERLAP\\n");',
      '  process.exit(23);',
      '}',
      'appendFileSync(process.env.INIT_PNPM_LOG, "START\\n");',
      'await new Promise((resolveDelay) => setTimeout(resolveDelay, 400));',
      'closeSync(handle);',
      'rmSync(process.env.INIT_PNPM_ACTIVE);',
      'appendFileSync(process.env.INIT_PNPM_LOG, "END\\n");',
    ].join('\n'));
    const env = {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_ACTIVE: active,
      INIT_PNPM_LOG: log,
    };
    const results = await Promise.all([
      runAsync(root, 'init.mjs', ['--no-open'], env),
      runAsync(root, 'init.mjs', ['--no-open'], env),
    ]);
    assert.deepEqual(results.map((result) => result.status).sort(), [0, 3], results.map(output).join('\n---\n'));
    const busy = results.find((result) => result.status === 3);
    assert.match(output(busy), /INIT_STATE=installing/);
    assert.match(output(busy), /INIT_ERROR=INIT_BUSY/);
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), ['START', 'END']);
  } finally {
    dispose(root);
  }
});

test('a killed init parent cannot orphan an installer that overlaps the next init', async () => {
  const root = fixture();
  let first;
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backup = join(stateRoot, 'ui-backup', transaction.id, 'apps');
    mkdirSync(backup, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backup, 'web-ele'));
    renameSync(join(root, 'apps', 'web-naive'), join(backup, 'web-naive'));
    writeFileSync(join(root, '.ui-profile.json'), JSON.stringify({
      schema: 1, selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd',
    }));
    writeFileSync(join(stateRoot, 'transaction.json'), JSON.stringify(transaction));

    const runner = join(root, 'scripts', 'orphan-sensitive-pnpm.mjs');
    const active = join(root, 'orphan-pnpm-active.lock');
    const ready = join(root, 'orphan-pnpm-ready');
    const log = join(root, 'orphan-pnpm.log');
    writeFileSync(runner, [
      'import { appendFileSync, closeSync, openSync, rmSync, writeFileSync } from "node:fs";',
      'let handle;',
      'try { handle = openSync(process.env.INIT_PNPM_ACTIVE, "wx"); } catch {',
      '  appendFileSync(process.env.INIT_PNPM_LOG, "OVERLAP\\n");',
      '  process.exit(23);',
      '}',
      'appendFileSync(process.env.INIT_PNPM_LOG, "START\\n");',
      'writeFileSync(process.env.INIT_PNPM_READY, String(process.pid));',
      'await new Promise((resolveDelay) => setTimeout(resolveDelay, 800));',
      'closeSync(handle);',
      'rmSync(process.env.INIT_PNPM_ACTIVE);',
      'appendFileSync(process.env.INIT_PNPM_LOG, "END\\n");',
    ].join('\n'));
    const env = {
      ...process.env,
      INIT_API_TEST_MODE: 'ready',
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_ACTIVE: active,
      INIT_PNPM_READY: ready,
      INIT_PNPM_LOG: log,
    };
    first = spawn(process.execPath, [join(root, 'scripts', 'init.mjs'), '--no-open'], {
      cwd: root,
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const firstExit = new Promise((resolveExit) => first.once('exit', (status, signal) => resolveExit({ status, signal })));
    const readyDeadline = Date.now() + 2_000;
    while (!existsSync(ready) && Date.now() < readyDeadline) {
      await new Promise((resolveDelay) => setTimeout(resolveDelay, 10));
    }
    assert.equal(existsSync(ready), true);

    first.kill('SIGKILL');
    const killed = await firstExit;
    assert.notEqual(killed.status, 0);

    const dependencyLease = JSON.parse(readFileSync(join(stateRoot, 'dependency-install.lock'), 'utf8'));
    assert.equal(dependencyLease.childPid, dependencyLease.supervisorPid);
    const dependencyHeartbeat = join(stateRoot, 'dependency-install-heartbeat', `${dependencyLease.id}.json`);
    const stale = new Date(Date.now() - 86_400_000);
    const replacementHeartbeat = `${JSON.stringify({
      schema: 1,
      owner: 'admin-dependency-install',
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      supervisorPid: dependencyLease.supervisorPid,
      childPid: dependencyLease.childPid,
      updatedAt: stale.toISOString(),
    })}\n`;
    rmSync(dependencyHeartbeat);
    writeFileSync(dependencyHeartbeat, replacementHeartbeat);
    utimesSync(dependencyHeartbeat, stale, stale);

    const second = run(root, 'init.mjs', ['--no-open'], env);
    assert.equal(second.status, 3, output(second));
    assert.match(output(second), /INIT_ERROR=INIT_BUSY/);
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), ['START']);

    const completionDeadline = Date.now() + 3_000;
    while ((!existsSync(log) || !readFileSync(log, 'utf8').includes('END\n')) && Date.now() < completionDeadline) {
      await new Promise((resolveDelay) => setTimeout(resolveDelay, 20));
    }
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), ['START', 'END']);
    assert.equal(readFileSync(dependencyHeartbeat, 'utf8'), replacementHeartbeat);

    const third = run(root, 'init.mjs', ['--no-open'], env);
    assert.equal(third.status, 0, output(third));
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), ['START', 'END']);
  } finally {
    if (first && first.exitCode === null && first.signalCode === null) first.kill('SIGKILL');
    dispose(root);
  }
});

test('failed pnpm Worker keeps its lease while a POSIX lifecycle descendant is alive', async (context) => {
  if (process.platform === 'win32') {
    context.skip('Windows descendants are terminated by the kill-on-close Job Object wrapper');
    return;
  }
  const root = fixture();
  try {
    const sleeper = join(root, 'scripts', 'orphan-lifecycle-child.mjs');
    const failingRunner = join(root, 'scripts', 'spawn-orphan-pnpm.mjs');
    const ready = join(root, 'orphan-lifecycle-ready');
    const finished = join(root, 'orphan-lifecycle-finished');
    const secondStarted = join(root, 'second-install-started');
    writeFileSync(sleeper, [
      'import { writeFileSync } from "node:fs";',
      'await new Promise((resolveDelay) => setTimeout(resolveDelay, 700));',
      'writeFileSync(process.env.INIT_ORPHAN_FINISHED, "done");',
    ].join('\n'));
    writeFileSync(failingRunner, [
      'import { spawn } from "node:child_process";',
      'import { writeFileSync } from "node:fs";',
      'const child = spawn(process.execPath, [process.env.INIT_ORPHAN_SLEEPER], {',
      '  env: process.env, stdio: "ignore", shell: false,',
      '});',
      'await new Promise((resolveSpawn, rejectSpawn) => {',
      '  child.once("spawn", resolveSpawn); child.once("error", rejectSpawn);',
      '});',
      'child.unref();',
      'writeFileSync(process.env.INIT_ORPHAN_READY, String(child.pid));',
      'process.exit(23);',
    ].join('\n'));
    const failingEnv = {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_ORPHAN_FINISHED: finished,
      INIT_ORPHAN_READY: ready,
      INIT_ORPHAN_SLEEPER: sleeper,
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([failingRunner]),
    };
    const first = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], failingEnv);
    assert.equal(first.status, 1, output(first));
    assert.equal(existsSync(ready), true);

    const successfulRunner = join(root, 'scripts', 'second-install-pnpm.mjs');
    writeFileSync(successfulRunner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_SECOND_STARTED, "yes");\n');
    const retryEnv = {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([successfulRunner]),
      INIT_SECOND_STARTED: secondStarted,
    };
    const busy = run(root, 'init.mjs', ['--no-open'], retryEnv);
    assert.equal(busy.status, 3, output(busy));
    assert.match(output(busy), /INIT_ERROR=INIT_BUSY/);
    assert.equal(existsSync(secondStarted), false);

    const deadline = Date.now() + 3_000;
    while (!existsSync(finished) && Date.now() < deadline) {
      await new Promise((resolveDelay) => setTimeout(resolveDelay, 20));
    }
    assert.equal(existsSync(finished), true);
    const recovered = run(root, 'init.mjs', ['--no-open'], retryEnv);
    assert.equal(recovered.status, 0, output(recovered));
    assert.equal(readFileSync(secondStarted, 'utf8'), 'yes');
  } finally {
    dispose(root);
  }
});

test('two concurrent stale-lease reclaimers allow at most one dependency installer', async () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backup = join(stateRoot, 'ui-backup', transaction.id, 'apps');
    mkdirSync(backup, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backup, 'web-ele'));
    renameSync(join(root, 'apps', 'web-naive'), join(backup, 'web-naive'));
    writeFileSync(join(root, '.ui-profile.json'), JSON.stringify({
      schema: 1, selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd',
    }));
    writeFileSync(join(stateRoot, 'transaction.json'), JSON.stringify(transaction));
    writeAdminLease(root, { pid: definitelyDeadPID(), createdAt: new Date(Date.now() - 86_400_000).toISOString() });

    const runner = join(root, 'scripts', 'slow-stale-reclaim-pnpm.mjs');
    const active = join(root, 'stale-reclaim-active.lock');
    const log = join(root, 'stale-reclaim-concurrency.log');
    writeFileSync(runner, [
      'import { appendFileSync, openSync, closeSync, rmSync } from "node:fs";',
      'let handle;',
      'try { handle = openSync(process.env.INIT_PNPM_ACTIVE, "wx"); } catch {',
      '  appendFileSync(process.env.INIT_PNPM_LOG, "OVERLAP\\n");',
      '  process.exit(23);',
      '}',
      'appendFileSync(process.env.INIT_PNPM_LOG, "START\\n");',
      'await new Promise((resolveDelay) => setTimeout(resolveDelay, 400));',
      'closeSync(handle);',
      'rmSync(process.env.INIT_PNPM_ACTIVE);',
      'appendFileSync(process.env.INIT_PNPM_LOG, "END\\n");',
    ].join('\n'));
    const env = {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_ACTIVE: active,
      INIT_PNPM_LOG: log,
    };

    const results = await Promise.all([
      runAsync(root, 'init.mjs', ['--no-open'], env),
      runAsync(root, 'init.mjs', ['--no-open'], env),
    ]);

    assert.deepEqual(results.map((result) => result.status).sort(), [0, 3], results.map(output).join('\n---\n'));
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), ['START', 'END']);
  } finally {
    dispose(root);
  }
});

test('stale lease reclamation uses one fixed no-clobber tombstone before unlinking the canonical lease', () => {
  const source = readFileSync(join(sourceRoot, 'scripts', 'init-state.mjs'), 'utf8');
  assert.match(source, /admin-init\.lock\.reclaim/);
  assert.match(source, /await link\(location\.adminLease, location\.adminLeaseReclaim\)/);
  assert.match(source, /readLeaseSnapshot\(location\.adminLeaseReclaim(?:, options)?\)/);
});

test('a run after a crashed stale reclaimer restores the canonical gap and acquires one exact lease', async () => {
  const root = fixture();
  try {
    const lease = writeAdminLease(root, {
      pid: definitelyDeadPID(),
      createdAt: new Date(Date.now() - 86_400_000).toISOString(),
    });
    const reclaim = `${lease.path}.reclaim`;
    linkSync(lease.path, reclaim);
    rmSync(lease.path);

    const release = await acquireAdminInitLease(root, {
      now: () => Date.now() + 61_000,
      reclaimGraceMs: 60_000,
    });

    assert.equal(existsSync(lease.path), true);
    assert.equal(existsSync(reclaim), false);
    assert.equal(await release(), true);
    assert.equal(existsSync(lease.path), false);
  } finally {
    dispose(root);
  }
});

test('an expired reclaim tombstone never evicts a canonical lease whose PID is live', async () => {
  const root = fixture();
  try {
    const lease = writeAdminLease(root, {
      pid: process.pid,
      createdAt: new Date(Date.now() - 86_400_000).toISOString(),
    });
    const simulatedNow = Date.now() + 61_000;
    writeAdminHeartbeat(root, lease, { updatedAt: new Date(simulatedNow).toISOString() });
    const reclaim = `${lease.path}.reclaim`;
    linkSync(lease.path, reclaim);
    const canonicalBefore = readFileSync(lease.path, 'utf8');
    const reclaimBefore = readFileSync(reclaim, 'utf8');

    await assert.rejects(
      acquireAdminInitLease(root, { now: () => simulatedNow, reclaimGraceMs: 60_000 }),
      /INIT_BUSY/,
    );

    assert.equal(readFileSync(lease.path, 'utf8'), canonicalBefore);
    assert.equal(readFileSync(reclaim, 'utf8'), reclaimBefore);
  } finally {
    dispose(root);
  }
});

test('an old lease with a reused live PID and stale matching heartbeat is reclaimed', () => {
  const root = fixture();
  try {
    const old = new Date(Date.now() - 86_400_000);
    const lease = writeAdminLease(root, { pid: process.pid, createdAt: old.toISOString() });
    utimesSync(lease.path, old, old);
    const heartbeat = writeAdminHeartbeat(root, lease, { updatedAt: old.toISOString() });
    utimesSync(heartbeat.path, old, old);

    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);

    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_STATE=ui_prepared/);
    assert.equal(existsSync(lease.path), false);
    assert.equal(existsSync(heartbeat.path), false);
  } finally {
    dispose(root);
  }
});

test('a SIGSTOPed admin owner with exact process identity remains busy beyond heartbeat TTL', async (context) => {
  if (process.platform === 'win32') {
    context.skip('SIGSTOP is POSIX-only');
    return;
  }
  const root = fixture();
  let owner;
  let stopped = false;
  try {
    const helper = join(root, 'scripts', 'stopped-admin-owner.mjs');
    const ready = join(root, 'stopped-admin-ready');
    const finish = join(root, 'stopped-admin-finish');
    writeFileSync(helper, [
      'import { existsSync, writeFileSync } from "node:fs";',
      'import { acquireAdminInitLease } from "./init-state.mjs";',
      'const release = await acquireAdminInitLease(process.argv[2], { heartbeatIntervalMs: 20, heartbeatStaleMs: 100 });',
      'writeFileSync(process.argv[3], "ready");',
      'while (!existsSync(process.argv[4])) await new Promise((resolveDelay) => setTimeout(resolveDelay, 10));',
      'await release();',
    ].join('\n'));
    owner = spawn(process.execPath, [helper, root, ready, finish], {
      cwd: root,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let ownerOutput = '';
    owner.stdout.on('data', (chunk) => { ownerOutput += chunk; });
    owner.stderr.on('data', (chunk) => { ownerOutput += chunk; });
    const ownerClosed = new Promise((resolveClosed) => owner.once('close', (status, signal) => resolveClosed({ status, signal })));
    const readyDeadline = Date.now() + 2_000;
    while (!existsSync(ready) && Date.now() < readyDeadline) {
      await new Promise((resolveDelay) => setTimeout(resolveDelay, 10));
    }
    assert.equal(existsSync(ready), true, ownerOutput);
    const lease = JSON.parse(readFileSync(adminLeasePath(root), 'utf8'));
    assert.equal(lease.schema, 2);
    assert.match(lease.pidStartToken, /^[0-9a-f]{64}$/);
    const heartbeat = JSON.parse(readFileSync(join(adminHeartbeatRoot(root), `${lease.id}.json`), 'utf8'));
    assert.equal(heartbeat.schema, 2);
    assert.equal(heartbeat.pidStartToken, lease.pidStartToken);

    owner.kill('SIGSTOP');
    stopped = true;
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 180));
    const contender = await acquireAdminInitLease(root, {
      heartbeatIntervalMs: 20,
      heartbeatStaleMs: 100,
      processStartToken: () => lease.pidStartToken,
    }).then(
      (release) => ({ release }),
      (error) => ({ error }),
    );
    if (contender.release) await contender.release();
    assert.match(contender.error?.message ?? '', /INIT_BUSY/);

    owner.kill('SIGCONT');
    stopped = false;
    writeFileSync(finish, 'finish');
    const closed = await ownerClosed;
    assert.equal(closed.status, 0, ownerOutput);
    assert.equal(closed.signal, null, ownerOutput);
  } finally {
    if (owner && stopped) owner.kill('SIGCONT');
    if (owner && owner.exitCode === null && owner.signalCode === null) owner.kill('SIGTERM');
    dispose(root);
  }
});

test('admin schema2 lease reclaims a reused live PID only when its start token mismatches', async () => {
  const root = fixture();
  try {
    const old = new Date(Date.now() - 7_200_000);
    const lease = writeAdminLease(root, { createdAt: old.toISOString() });
    const heartbeat = writeAdminHeartbeat(root, lease, { updatedAt: old.toISOString() });
    utimesSync(lease.path, old, old);
    utimesSync(heartbeat.path, old, old);

    const release = await acquireAdminInitLease(root, {
      processStartToken: () => 'b'.repeat(64),
    });
    assert.equal(existsSync(heartbeat.path), false);
    assert.equal(await release(), true);
  } finally {
    dispose(root);
  }
});

test('legacy schema1 admin leases use bounded fail-safe recovery', async () => {
  const recentRoot = fixture();
  try {
    const recent = new Date(Date.now() - 7_200_000);
    const lease = writeAdminLease(recentRoot, { createdAt: recent.toISOString(), schema: 1 });
    const heartbeat = writeAdminHeartbeat(recentRoot, lease, { updatedAt: recent.toISOString() });
    utimesSync(lease.path, recent, recent);
    utimesSync(heartbeat.path, recent, recent);
    await assert.rejects(
      acquireAdminInitLease(recentRoot, { processStartToken: () => 'b'.repeat(64) }),
      /INIT_BUSY/,
    );
  } finally {
    dispose(recentRoot);
  }

  const expiredRoot = fixture();
  try {
    const expired = new Date(Date.now() - 172_800_000);
    const lease = writeAdminLease(expiredRoot, { createdAt: expired.toISOString(), schema: 1 });
    const heartbeat = writeAdminHeartbeat(expiredRoot, lease, { updatedAt: expired.toISOString() });
    utimesSync(lease.path, expired, expired);
    utimesSync(heartbeat.path, expired, expired);
    const release = await acquireAdminInitLease(expiredRoot, { processStartToken: () => 'b'.repeat(64) });
    assert.equal(existsSync(lease.path), true);
    assert.equal(await release(), true);
  } finally {
    dispose(expiredRoot);
  }
});

test('unavailable schema2 admin identity is fail-closed but not permanent', async () => {
  const live = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 5000)'], { stdio: 'ignore' });
  const resolver = (pid) => (pid === process.pid ? 'b'.repeat(64) : null);
  try {
    const recentRoot = fixture();
    try {
      const recent = new Date(Date.now() - 7_200_000);
      const lease = writeAdminLease(recentRoot, { pid: live.pid, createdAt: recent.toISOString() });
      const heartbeat = writeAdminHeartbeat(recentRoot, lease, { updatedAt: recent.toISOString() });
      utimesSync(lease.path, recent, recent);
      utimesSync(heartbeat.path, recent, recent);
      await assert.rejects(
        acquireAdminInitLease(recentRoot, { processStartToken: resolver }),
        /INIT_BUSY/,
      );
    } finally {
      dispose(recentRoot);
    }

    const expiredRoot = fixture();
    try {
      const expired = new Date(Date.now() - 172_800_000);
      const lease = writeAdminLease(expiredRoot, { pid: live.pid, createdAt: expired.toISOString() });
      const heartbeat = writeAdminHeartbeat(expiredRoot, lease, { updatedAt: expired.toISOString() });
      utimesSync(lease.path, expired, expired);
      utimesSync(heartbeat.path, expired, expired);
      const release = await acquireAdminInitLease(expiredRoot, { processStartToken: resolver });
      assert.equal(await release(), true);
    } finally {
      dispose(expiredRoot);
    }
  } finally {
    if (live.exitCode === null && live.signalCode === null) live.kill('SIGTERM');
  }
});

test('owner heartbeat stays fresh while the main thread is blocked by a long synchronous command', async () => {
  const root = fixture();
  try {
    const release = await acquireAdminInitLease(root, {
      heartbeatIntervalMs: 20,
      heartbeatStaleMs: 100,
    });
    const heartbeatRoot = adminHeartbeatRoot(root);
    const [heartbeatName] = readdirSync(heartbeatRoot);
    const heartbeatPath = join(heartbeatRoot, heartbeatName);
    const before = JSON.parse(readFileSync(heartbeatPath, 'utf8'));

    const blocked = spawnSync(process.execPath, ['-e', 'setTimeout(() => {}, 240)']);
    assert.equal(blocked.status, 0);
    const after = JSON.parse(readFileSync(heartbeatPath, 'utf8'));
    assert.ok(Date.parse(after.updatedAt) > Date.parse(before.updatedAt));
    await assert.rejects(
      acquireAdminInitLease(root, { heartbeatIntervalMs: 20, heartbeatStaleMs: 100 }),
      /INIT_BUSY/,
    );

    assert.equal(await release(), true);
    assert.equal(existsSync(heartbeatPath), false);
  } finally {
    dispose(root);
  }
});

test('intentional heartbeat stop preserves a same-UUID replacement inode', () => {
  const root = fixture();
  try {
    const helper = join(root, 'scripts', 'replace-heartbeat-then-release.mjs');
    const replacementCopy = join(root, 'replacement-heartbeat.txt');
    writeFileSync(helper, [
      'import { readFileSync, rmSync, writeFileSync } from "node:fs";',
      'import { join } from "node:path";',
      'import { acquireAdminInitLease } from "./init-state.mjs";',
      'const release = await acquireAdminInitLease(process.argv[2], { heartbeatIntervalMs: 1_000, heartbeatStaleMs: 3_000 });',
      'const lease = JSON.parse(readFileSync(join(process.argv[2], "..", ".runtime", "install", "admin-init.lock"), "utf8"));',
      'const heartbeat = join(process.argv[2], "..", ".runtime", "install", "admin-init-heartbeat", `${lease.id}.json`);',
      'const original = JSON.parse(readFileSync(heartbeat, "utf8"));',
      'const replacement = `${JSON.stringify({ ...original, updatedAt: new Date(Date.now() + 1_000).toISOString() })}\\n`;',
      'rmSync(heartbeat);',
      'writeFileSync(heartbeat, replacement);',
      'writeFileSync(process.argv[3], replacement);',
      'if (!await release()) process.exitCode = 2;',
    ].join('\n'));
    const result = spawnSync(process.execPath, [helper, root, replacementCopy], {
      cwd: root,
      encoding: 'utf8',
      env: process.env,
    });

    assert.equal(result.status, 0, output(result));
    const [heartbeatName] = readdirSync(adminHeartbeatRoot(root));
    const heartbeatPath = join(adminHeartbeatRoot(root), heartbeatName);
    assert.equal(readFileSync(heartbeatPath, 'utf8'), readFileSync(replacementCopy, 'utf8'));
  } finally {
    dispose(root);
  }
});

test('one heartbeat worker failure keeps a blocked owner exclusive through its UUID owner channel', async () => {
  const root = fixture();
  let owner;
  try {
    const helper = join(root, 'scripts', 'blocked-heartbeat-owner.mjs');
    const ready = join(root, 'heartbeat-owner-ready');
    writeFileSync(helper, [
      'import { spawnSync } from "node:child_process";',
      'import { writeFileSync } from "node:fs";',
      'import { acquireAdminInitLease } from "./init-state.mjs";',
      'const release = await acquireAdminInitLease(process.argv[2], { heartbeatIntervalMs: 20, heartbeatStaleMs: 100 });',
      'writeFileSync(process.argv[3], "ready");',
      'spawnSync(process.execPath, ["-e", "setTimeout(() => {}, 500)"]);',
      'await release();',
    ].join('\n'));
    owner = spawn(process.execPath, [helper, root, ready], { cwd: root, stdio: ['ignore', 'pipe', 'pipe'] });
    const ownerClosed = new Promise((resolveClosed) => owner.once('close', (status, signal) => resolveClosed({ status, signal })));
    const deadline = Date.now() + 2_000;
    while (!existsSync(ready) && Date.now() < deadline) {
      await new Promise((resolveDelay) => setTimeout(resolveDelay, 10));
    }
    assert.equal(existsSync(ready), true);

    const lease = JSON.parse(readFileSync(adminLeasePath(root), 'utf8'));
    assert.equal(existsSync(join(adminHeartbeatRoot(root), `${lease.id}.owner.json`)), true);
    const heartbeatPath = join(adminHeartbeatRoot(root), `${lease.id}.json`);
    const replacement = `${JSON.stringify({
      schema: 1,
      owner: 'admin-init',
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      pid: process.pid,
      updatedAt: new Date().toISOString(),
    })}\n`;
    rmSync(heartbeatPath);
    writeFileSync(heartbeatPath, replacement);
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 160));

    await assert.rejects(async () => {
      const release = await acquireAdminInitLease(root, { heartbeatIntervalMs: 20, heartbeatStaleMs: 100 });
      await release();
    }, /INIT_BUSY/);

    const closed = await ownerClosed;
    assert.equal(closed.status, 0);
    assert.equal(closed.signal, null);
    assert.equal(readFileSync(heartbeatPath, 'utf8'), replacement);
  } finally {
    if (owner && owner.exitCode === null && owner.signalCode === null) owner.kill('SIGTERM');
    dispose(root);
  }
});

test('heartbeat worker stops with a crashed owner and the next init reclaims its lease', async () => {
  const root = fixture();
  try {
    const helper = join(root, 'scripts', 'crash-after-lease.mjs');
    writeFileSync(helper, [
      'import { acquireAdminInitLease } from "./init-state.mjs";',
      'await acquireAdminInitLease(process.argv[2], { heartbeatIntervalMs: 20, heartbeatStaleMs: 100 });',
      'process.stdout.write("ready\\n");',
      'process.exit(0);',
    ].join('\n'));
    const crashed = spawnSync(process.execPath, [helper, root], { cwd: root, encoding: 'utf8' });
    assert.equal(crashed.status, 0, output(crashed));
    assert.match(crashed.stdout, /ready/);
    const lease = JSON.parse(readFileSync(adminLeasePath(root), 'utf8'));
    const heartbeatPath = join(adminHeartbeatRoot(root), `${lease.id}.json`);
    const before = statSync(heartbeatPath);
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 80));
    const after = statSync(heartbeatPath);
    assert.equal(after.mtimeMs, before.mtimeMs);

    const recovered = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']);
    assert.equal(recovered.status, 0, output(recovered));
    assert.equal(existsSync(heartbeatPath), false);
    assert.equal(existsSync(adminLeasePath(root)), false);
  } finally {
    dispose(root);
  }
});

test('an old lease with a fresh matching heartbeat remains busy beyond the lease age', () => {
  const root = fixture();
  try {
    const old = new Date(Date.now() - 86_400_000);
    const lease = writeAdminLease(root, { createdAt: old.toISOString() });
    utimesSync(lease.path, old, old);
    const heartbeat = writeAdminHeartbeat(root, lease);
    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=INIT_BUSY/);
    assert.equal(readFileSync(lease.path, 'utf8'), lease.contents);
    assert.equal(readFileSync(heartbeat.path, 'utf8'), heartbeat.contents);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'transaction.json')), false);
  } finally {
    dispose(root);
  }
});

test('admin lease and heartbeat inspection faults are always fail-closed', async (t) => {
  const cases = [
    {
      name: 'canonical read EIO',
      target(root, lease) { return lease.path; },
    },
    {
      name: 'reclaim tombstone read EIO',
      prepare(lease) { linkSync(lease.path, `${lease.path}.reclaim`); },
      target(root, lease) { return `${lease.path}.reclaim`; },
      options: { reclaimGraceMs: -1 },
    },
    {
      name: 'heartbeat read EIO',
      pid: definitelyDeadPID(),
      prepare(lease, root, old) {
        const heartbeat = writeAdminHeartbeat(root, lease, { updatedAt: old.toISOString() });
        utimesSync(heartbeat.path, old, old);
        return heartbeat.path;
      },
      target(root, lease, prepared) { return prepared; },
    },
  ];

  for (const testCase of cases) {
    await t.test(testCase.name, async () => {
      const root = fixture();
      try {
        const old = new Date(Date.now() - 172_800_000);
        const lease = writeAdminLease(root, { createdAt: old.toISOString(), pid: testCase.pid ?? process.pid });
        utimesSync(lease.path, old, old);
        const prepared = testCase.prepare?.(lease, root, old);
        const faultPath = testCase.target(root, lease, prepared);
        const stateRoot = join(root, '..', '.runtime', 'install');
        const before = filesystemSnapshot(stateRoot);

        await assert.rejects(
          acquireAdminInitLease(root, {
            ...testCase.options,
            processStartToken: () => 'b'.repeat(64),
            readFile(target, encoding) {
              if (target === faultPath) throw Object.assign(new Error('I/O'), { code: 'EIO' });
              return readFileSync(target, encoding);
            },
          }),
          /INIT_BUSY/,
        );

        assert.deepEqual(filesystemSnapshot(stateRoot), before);
      } finally {
        dispose(root);
      }
    });
  }
});

test('admin heartbeat root symlinks never expose external files to stale cleanup or new workers', async (t) => {
  for (const dangling of [false, true]) {
    await t.test(dangling ? 'dangling symlink' : 'external directory symlink', async () => {
      const root = fixture();
      try {
        const stateRoot = join(root, '..', '.runtime', 'install');
        const external = join(root, '..', dangling ? 'missing-heartbeats' : 'external-heartbeats');
        if (!dangling) mkdirSync(external, { recursive: true });
        mkdirSync(stateRoot, { recursive: true });
        symlinkSync(external, adminHeartbeatRoot(root));
        const old = new Date(Date.now() - 172_800_000);
        const lease = writeAdminLease(root, {
          createdAt: old.toISOString(),
          pid: dangling ? definitelyDeadPID() : process.pid,
        });
        utimesSync(lease.path, old, old);
        if (!dangling) {
          const heartbeat = writeAdminHeartbeat(root, lease, { updatedAt: old.toISOString() });
          utimesSync(heartbeat.path, old, old);
        }
        const externalBefore = filesystemSnapshot(external);
        const leaseBefore = readFileSync(lease.path);

        await assert.rejects(
          acquireAdminInitLease(root, { processStartToken: () => 'b'.repeat(64) }),
          /INIT_BUSY|INIT_LEASE_FAILED/,
        );

        assert.deepEqual(filesystemSnapshot(external), externalBefore);
        assert.deepEqual(readFileSync(lease.path), leaseBefore);
        assert.equal(lstatSync(adminHeartbeatRoot(root)).isSymbolicLink(), true);
      } finally {
        dispose(root);
      }
    });
  }
});

test('heartbeat schema and updatedAt are strict before they can keep a live PID lease active', () => {
  for (const mutate of [
    (heartbeat) => ({ ...heartbeat, unexpected: true }),
    (heartbeat) => ({ ...heartbeat, updatedAt: 'not-an-iso-timestamp' }),
  ]) {
    const root = fixture();
    try {
      const old = new Date(Date.now() - 86_400_000);
      const lease = writeAdminLease(root, { pid: process.pid, createdAt: old.toISOString() });
      utimesSync(lease.path, old, old);
      const directory = adminHeartbeatRoot(root);
      mkdirSync(directory, { recursive: true });
      const heartbeatPath = join(directory, `${lease.id}.json`);
      const invalidHeartbeat = `${JSON.stringify(mutate({
        schema: 1,
        owner: 'admin-init',
        id: lease.id,
        pid: lease.pid,
        updatedAt: new Date().toISOString(),
      }))}\n`;
      writeFileSync(heartbeatPath, invalidHeartbeat);
      utimesSync(heartbeatPath, old, old);

      const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
      assert.equal(result.status, 0, output(result));
      assert.equal(readFileSync(heartbeatPath, 'utf8'), invalidHeartbeat);
    } finally {
      dispose(root);
    }
  }
});

test('a recent truncated lease is busy while an expired truncated lease is reclaimed', () => {
  const recentRoot = fixture();
  try {
    const lease = adminLeasePath(recentRoot);
    mkdirSync(join(recentRoot, '..', '.runtime', 'install'), { recursive: true });
    writeFileSync(lease, '{"schema":');
    const busy = run(recentRoot, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(busy.status, 3, output(busy));
    assert.match(output(busy), /INIT_ERROR=INIT_BUSY/);
    assert.equal(readFileSync(lease, 'utf8'), '{"schema":');
  } finally {
    dispose(recentRoot);
  }

  const expiredRoot = fixture();
  try {
    const lease = adminLeasePath(expiredRoot);
    mkdirSync(join(expiredRoot, '..', '.runtime', 'install'), { recursive: true });
    writeFileSync(lease, '{"schema":');
    const old = new Date(Date.now() - 120_000);
    utimesSync(lease, old, old);
    const recovered = run(expiredRoot, 'init.mjs', ['--ui', 'naive', '--confirm-cleanup', '--no-open']);
    assert.equal(recovered.status, 0, output(recovered));
    assert.equal(existsSync(lease), false);
    assert.equal(existsSync(join(expiredRoot, 'apps', 'web-naive')), true);
    assert.equal(existsSync(join(expiredRoot, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(expiredRoot, 'apps', 'web-ele')), false);
  } finally {
    dispose(expiredRoot);
  }
});

test('after Go startup clears a dead apply lease, init reclaims the dead admin lease and continues', () => {
  const root = fixture();
  try {
    const stateRoot = join(root, '..', '.runtime', 'install');
    mkdirSync(stateRoot, { recursive: true });
    const processGuard = join(stateRoot, 'process.guard');
    writeFileSync(processGuard, '');
    assert.equal(existsSync(join(stateRoot, 'apply.lock')), false);
    const lease = writeAdminLease(root, { pid: definitelyDeadPID() });
    const result = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 0, output(result));
    assert.equal(existsSync(lease.path), false);
    assert.equal(readFileSync(processGuard, 'utf8'), '');
    assert.equal(existsSync(join(stateRoot, 'apply.lock')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), true);
  } finally {
    dispose(root);
  }
});

test('--check never creates or reclaims the admin init lease', () => {
  const root = fixture();
  try {
    const lease = adminLeasePath(root);
    mkdirSync(join(root, '..', '.runtime', 'install'), { recursive: true });
    writeFileSync(lease, '{"schema":');
    const heartbeatDirectory = adminHeartbeatRoot(root);
    mkdirSync(heartbeatDirectory, { recursive: true });
    const heartbeat = join(heartbeatDirectory, '12345678-1234-1234-1234-123456789abc.json');
    writeFileSync(heartbeat, '{"schema":');
    const dependencyLease = join(root, '..', '.runtime', 'install', 'dependency-install.lock');
    writeFileSync(dependencyLease, '{"schema":');
    const dependencyHeartbeatDirectory = join(root, '..', '.runtime', 'install', 'dependency-install-heartbeat');
    mkdirSync(dependencyHeartbeatDirectory, { recursive: true });
    const dependencyHeartbeat = join(dependencyHeartbeatDirectory, '12345678-1234-1234-1234-123456789abc.json');
    writeFileSync(dependencyHeartbeat, '{"schema":');
    const old = new Date(Date.now() - 120_000);
    utimesSync(lease, old, old);
    utimesSync(heartbeat, old, old);
    utimesSync(dependencyLease, old, old);
    utimesSync(dependencyHeartbeat, old, old);
    const before = statSync(lease);
    const heartbeatBefore = statSync(heartbeat);
    const dependencyLeaseBefore = statSync(dependencyLease);
    const dependencyHeartbeatBefore = statSync(dependencyHeartbeat);
    const result = run(root, 'init.mjs', ['--check', '--no-open']);
    assert.equal(result.status, 0, output(result));
    assert.equal(readFileSync(lease, 'utf8'), '{"schema":');
    const after = statSync(lease);
    assert.equal(after.ino, before.ino);
    assert.equal(after.mtimeMs, before.mtimeMs);
    assert.equal(readFileSync(heartbeat, 'utf8'), '{"schema":');
    const heartbeatAfter = statSync(heartbeat);
    assert.equal(heartbeatAfter.ino, heartbeatBefore.ino);
    assert.equal(heartbeatAfter.mtimeMs, heartbeatBefore.mtimeMs);
    assert.equal(readFileSync(dependencyLease, 'utf8'), '{"schema":');
    assert.equal(statSync(dependencyLease).ino, dependencyLeaseBefore.ino);
    assert.equal(statSync(dependencyLease).mtimeMs, dependencyLeaseBefore.mtimeMs);
    assert.equal(readFileSync(dependencyHeartbeat, 'utf8'), '{"schema":');
    assert.equal(statSync(dependencyHeartbeat).ino, dependencyHeartbeatBefore.ino);
    assert.equal(statSync(dependencyHeartbeat).mtimeMs, dependencyHeartbeatBefore.mtimeMs);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'dependency-install.log')), false);
  } finally {
    dispose(root);
  }
});

test('lease release preserves a replacement that is not the acquired exact lock', () => {
  const root = fixture();
  try {
    const runner = join(root, 'scripts', 'replace-admin-lease.mjs');
    const replacement = `${JSON.stringify({
      schema: 1,
      owner: 'admin-init',
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      pid: process.pid,
      createdAt: new Date().toISOString(),
    })}\n`;
    writeFileSync(runner, [
      'import { rmSync, writeFileSync } from "node:fs";',
      'rmSync(process.env.INIT_ADMIN_LEASE_PATH);',
      'writeFileSync(process.env.INIT_ADMIN_LEASE_PATH, process.env.INIT_REPLACEMENT_LEASE);',
    ].join('\n'));
    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_ADMIN_LEASE_PATH: adminLeasePath(root),
      INIT_REPLACEMENT_LEASE: replacement,
    });
    assert.equal(result.status, 0, output(result));
    assert.equal(readFileSync(adminLeasePath(root), 'utf8'), replacement);
  } finally {
    dispose(root);
  }
});

test('dependency lease release preserves replacement lock and heartbeat inodes', async () => {
  const root = fixture();
  try {
    const controller = await acquireDependencyInstallLease(root, { heartbeatIntervalMs: 1_000 });
    const stateRoot = join(root, '..', '.runtime', 'install');
    const leasePath = join(stateRoot, 'dependency-install.lock');
    const originalLease = controller.lease;
    const heartbeatPath = join(stateRoot, 'dependency-install-heartbeat', `${originalLease.id}.json`);
    const originalHeartbeat = JSON.parse(readFileSync(heartbeatPath, 'utf8'));
    const replacementLease = `${JSON.stringify({
      ...originalLease,
      id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
      createdAt: new Date().toISOString(),
    })}\n`;
    const replacementHeartbeat = `${JSON.stringify({
      ...originalHeartbeat,
      updatedAt: new Date().toISOString(),
    })}\n`;

    rmSync(leasePath);
    writeFileSync(leasePath, replacementLease);
    rmSync(heartbeatPath);
    writeFileSync(heartbeatPath, replacementHeartbeat);

    assert.equal(await controller.release(), false);
    assert.equal(readFileSync(leasePath, 'utf8'), replacementLease);
    assert.equal(readFileSync(heartbeatPath, 'utf8'), replacementHeartbeat);
  } finally {
    dispose(root);
  }
});

test('abandoning a failed dependency installer stops heartbeat but preserves its process lease', async () => {
  const root = fixture();
  try {
    const controller = await acquireDependencyInstallLease(root, { heartbeatIntervalMs: 1_000 });
    const stateRoot = join(root, '..', '.runtime', 'install');
    const leasePath = join(stateRoot, 'dependency-install.lock');
    const heartbeatPath = join(stateRoot, 'dependency-install-heartbeat', `${controller.lease.id}.json`);

    assert.equal(await controller.abandon(), true);
    assert.equal(existsSync(leasePath), true);
    assert.equal(existsSync(heartbeatPath), false);
  } finally {
    dispose(root);
  }
});

test('dependency lease binds a separate Job guardian and Node child identity', async () => {
  const root = fixture();
  try {
    const guardianPid = process.ppid;
    const controller = await acquireDependencyInstallLease(root, {
      childPid: process.pid,
      heartbeatIntervalMs: 1_000,
      processStartToken: (pid) => (pid === guardianPid ? 'a'.repeat(64) : 'b'.repeat(64)),
      supervisorPid: guardianPid,
    });

    assert.equal(controller.lease.supervisorPid, guardianPid);
    assert.equal(controller.lease.childPid, process.pid);
    assert.equal(controller.lease.supervisorStartToken, 'a'.repeat(64));
    assert.equal(controller.lease.childStartToken, 'b'.repeat(64));
    assert.deepEqual(
      JSON.parse(readFileSync(join(root, '..', '.runtime', 'install', 'dependency-install.lock'), 'utf8')),
      controller.lease,
    );
    assert.equal(await controller.release(), true);
  } finally {
    dispose(root);
  }
});

test('dependency installation cannot cross an admin mutation lease without its exact handoff', async () => {
  const root = fixture();
  let releaseAdmin;
  let dependency;
  try {
    releaseAdmin = await acquireAdminInitLease(root, { heartbeatIntervalMs: 1_000 });
    let blocked;
    try {
      dependency = await acquireDependencyInstallLease(root, { heartbeatIntervalMs: 1_000 });
    } catch (error) {
      blocked = error;
    }
    if (dependency) {
      await dependency.release();
      dependency = null;
    }
    assert.equal(blocked?.message, 'INIT_BUSY');

    dependency = await acquireDependencyInstallLease(root, {
      adminLeaseId: releaseAdmin.lease.id,
      heartbeatIntervalMs: 1_000,
    });
    assert.equal(dependency.lease.owner, 'admin-dependency-install');
  } finally {
    await dependency?.release();
    await releaseAdmin?.();
    dispose(root);
  }
});

test('dependency admission rechecks an admin writer that wins its publication race', async () => {
  const root = fixture();
  let releaseAdmin;
  try {
    await assert.rejects(
      acquireDependencyInstallLease(root, {
        heartbeatIntervalMs: 1_000,
        afterAdminLeaseAdmission: async () => {
          releaseAdmin = await acquireAdminInitLease(root, { heartbeatIntervalMs: 1_000 });
        },
      }),
      /INIT_BUSY/,
    );
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'dependency-install.lock')), false);
  } finally {
    await releaseAdmin?.();
    dispose(root);
  }
});

test('admin admission removes only itself when dependency publication wins the reverse race', async () => {
  const root = fixture();
  let dependency;
  try {
    await assert.rejects(
      acquireAdminInitLease(root, {
        heartbeatIntervalMs: 1_000,
        afterDependencyAdmission: async () => {
          dependency = await acquireDependencyInstallLease(root, { heartbeatIntervalMs: 1_000 });
        },
      }),
      /INIT_BUSY/,
    );
    assert.equal(existsSync(adminLeasePath(root)), false);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'dependency-install.lock')), true);
  } finally {
    await dependency?.release();
    dispose(root);
  }
});

test('stale dependency lease with reused live PIDs is reclaimed by start-token mismatch', async () => {
  const root = fixture();
  try {
    const old = new Date(Date.now() - 86_400_000);
    const dependency = writeDependencyLease(root, { createdAt: old.toISOString(), updatedAt: old.toISOString() });
    utimesSync(dependency.path, old, old);
    utimesSync(dependency.heartbeatPath, old, old);

    const release = await acquireAdminInitLease(root, {
      processStartToken: () => 'b'.repeat(64),
    });
    assert.equal(existsSync(dependency.path), false);
    assert.equal(await release(), true);
  } finally {
    dispose(root);
  }
});

test('exact dependency process identities remain busy across heartbeat TTL', async () => {
  const root = fixture();
  try {
    const old = new Date(Date.now() - 86_400_000);
    const dependency = writeDependencyLease(root, { createdAt: old.toISOString(), updatedAt: old.toISOString() });
    utimesSync(dependency.path, old, old);
    utimesSync(dependency.heartbeatPath, old, old);

    await assert.rejects(
      acquireAdminInitLease(root, { processStartToken: () => 'a'.repeat(64) }),
      /INIT_BUSY/,
    );
    assert.equal(existsSync(dependency.path), true);
  } finally {
    dispose(root);
  }
});

test('unavailable dependency identity is fail-closed for active work but bounded for recovery', async () => {
  const recentRoot = fixture();
  try {
    const recent = new Date(Date.now() - 7_200_000);
    const dependency = writeDependencyLease(recentRoot, {
      createdAt: recent.toISOString(),
      updatedAt: recent.toISOString(),
    });
    utimesSync(dependency.path, recent, recent);
    utimesSync(dependency.heartbeatPath, recent, recent);
    await assert.rejects(
      acquireAdminInitLease(recentRoot, { processStartToken: () => null }),
      /INIT_BUSY/,
    );
  } finally {
    dispose(recentRoot);
  }

  const expiredRoot = fixture();
  try {
    const expired = new Date(Date.now() - 172_800_000);
    const dependency = writeDependencyLease(expiredRoot, {
      createdAt: expired.toISOString(),
      updatedAt: expired.toISOString(),
    });
    utimesSync(dependency.path, expired, expired);
    utimesSync(dependency.heartbeatPath, expired, expired);
    let identityLookups = 0;
    const release = await acquireAdminInitLease(expiredRoot, {
      processStartToken: () => (++identityLookups <= 2 ? null : 'b'.repeat(64)),
    });
    assert.equal(existsSync(dependency.path), false);
    assert.equal(await release(), true);
  } finally {
    dispose(expiredRoot);
  }
});

test('dependency lease inspection faults and invalid bytes are always fail-closed', async (t) => {
  const cases = [
    {
      name: 'lstat EACCES',
      inject(path) {
        return {
          lstat(target) {
            if (target === path) throw Object.assign(new Error('denied'), { code: 'EACCES' });
            return lstatSync(target);
          },
        };
      },
    },
    {
      name: 'read EIO',
      inject(path) {
        return {
          readFile(target, encoding) {
            if (target === path) throw Object.assign(new Error('I/O'), { code: 'EIO' });
            return readFileSync(target, encoding);
          },
        };
      },
    },
    {
      name: 'short read',
      inject(path) {
        return {
          readFile(target, encoding) {
            if (target === path) return '{"schema":';
            return readFileSync(target, encoding);
          },
        };
      },
    },
    {
      name: 'invalid old receipt',
      invalid: true,
      inject() { return {}; },
    },
    {
      name: 'reclaim tombstone EPERM',
      tombstone: true,
      inject(path, tombstonePath) {
        return {
          lstat(target) {
            if (target === tombstonePath) throw Object.assign(new Error('denied'), { code: 'EPERM' });
            return lstatSync(target);
          },
        };
      },
    },
  ];
  for (const testCase of cases) {
    await t.test(testCase.name, async () => {
      const root = fixture();
      try {
        const stateRoot = join(root, '..', '.runtime', 'install');
        mkdirSync(stateRoot, { recursive: true });
        const leasePath = join(stateRoot, 'dependency-install.lock');
        const tombstonePath = join(stateRoot, 'dependency-install.lock.reclaim');
        if (testCase.invalid) {
          writeFileSync(leasePath, '{"schema":');
          const old = new Date(Date.now() - 86_400_000);
          utimesSync(leasePath, old, old);
        } else {
          writeDependencyLease(root, { supervisorPid: definitelyDeadPID(), childPid: definitelyDeadPID() });
          rmSync(join(stateRoot, 'dependency-install-heartbeat'), { force: true, recursive: true });
        }
        if (testCase.tombstone) linkSync(leasePath, tombstonePath);
        const before = [leasePath, tombstonePath].map(filesystemSnapshot);
        const injected = testCase.inject(leasePath, tombstonePath);

        await assert.rejects(
          acquireDependencyInstallLease(root, {
            ...injected,
            processStartToken: () => null,
            heartbeatIntervalMs: 1_000,
            ...(testCase.tombstone ? { reclaimGraceMs: -1 } : {}),
          }),
          /INIT_BUSY/,
        );

        assert.deepEqual([leasePath, tombstonePath].map(filesystemSnapshot), before);
      } finally {
        dispose(root);
      }
    });
  }
});

test('init completes a reversible capability preflight before confirmation or source moves', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, '..', '.runtime'), 'not-a-directory');

    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_PREFLIGHT=failed/);
    assert.match(output(result), /INIT_ERROR=PREFLIGHT_FAILED/);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'transaction.json')), false);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    for (const ui of ['antd', 'ele', 'naive']) assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
  } finally {
    dispose(root);
  }
});

test('init rejects an invalid selected runtime env template before creating a transaction or moving UI sources', () => {
  const root = fixture();
  try {
    const template = join(root, 'apps', 'web-ele', '.env.development.example');
    rmSync(template);
    mkdirSync(template);

    const result = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_PREFLIGHT=failed/);
    assert.match(output(result), /INIT_ERROR=PREFLIGHT_FAILED/);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'transaction.json')), false);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    for (const ui of ['antd', 'ele', 'naive']) assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
  } finally {
    dispose(root);
  }
});

test('UI permission preflight proves create capability and removes every probe before failing', async () => {
  const root = fixture();
  try {
    const repository = fixtureRepositories.get(root);
    const before = filesystemSnapshot(repository);
    const denied = Object.assign(new Error('private path detail'), { code: 'EACCES' });

    await assert.rejects(
      () => preflightInitialization(root, 'ele', {
        open: async (target, ...args) => {
          if (String(target).includes('.gin-vben-preflight-')) throw denied;
          return openAsync(target, ...args);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'admin_root'
        && error?.operation === 'create',
    );

    assert.deepEqual(filesystemSnapshot(repository), before);
    assert.equal(existsSync(join(repository, '.runtime', 'install')), false);
  } finally {
    dispose(root);
  }
});

test('UI permission preflight detects cross-volume template moves before creating initialization state', async () => {
  const root = fixture();
  try {
    const repository = fixtureRepositories.get(root);
    const before = filesystemSnapshot(repository);
    const crossDevice = Object.assign(new Error('/private/source -> /private/target'), { code: 'EXDEV' });

    await assert.rejects(
      () => preflightInitialization(root, 'antd', {
        rename: async (source, target) => {
          if (String(source).includes('.gin-vben-preflight-transfer-')) throw crossDevice;
          return renameAsync(source, target);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'ui_backup'
        && error?.operation === 'cross_directory_rename',
    );

    assert.deepEqual(filesystemSnapshot(repository), before);
    assert.equal(existsSync(join(repository, '.runtime', 'install')), false);
  } finally {
    dispose(root);
  }
});

test('UI permission preflight verifies hard-link and directory-sync capabilities', async () => {
  for (const [operation, scope, override] of [
    ['link', 'selected_ui', { link: async () => { throw Object.assign(new Error('unsupported'), { code: 'EOPNOTSUPP' }); } }],
    ['sync', 'admin_root', { syncDirectory: async () => { throw Object.assign(new Error('denied'), { code: 'EACCES' }); } }],
  ]) {
    const root = fixture();
    try {
      const repository = fixtureRepositories.get(root);
      const before = filesystemSnapshot(repository);
      await assert.rejects(
        () => preflightInitialization(root, 'ele', override),
        (error) => error?.message === 'PREFLIGHT_FAILED'
          && error?.scope === scope
          && error?.operation === operation,
      );
      assert.deepEqual(filesystemSnapshot(repository), before);
      assert.equal(existsSync(join(repository, '.runtime', 'install')), false);
    } finally {
      dispose(root);
    }
  }
});

test('UI permission preflight treats transfer cleanup as a required capability', async () => {
  const root = fixture();
  try {
    const repository = fixtureRepositories.get(root);
    const before = filesystemSnapshot(repository);
    await assert.rejects(
      () => preflightInitialization(root, 'naive', {
        remove: async (target, options) => {
          if (String(target).includes('.gin-vben-preflight-transfer-')) {
            throw Object.assign(new Error('locked'), { code: 'EPERM' });
          }
          return rmAsync(target, options);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'ui_backup'
        && error?.operation === 'delete',
    );
    assert.deepEqual(filesystemSnapshot(repository), before);
  } finally {
    dispose(root);
  }
});

test('UI permission preflight treats probe cleanup failures as a required capability', async () => {
  const root = fixture();
  try {
    let reportedFailure = false;
    await assert.rejects(
      () => preflightInitialization(root, 'antd', {
        cleanup: async (target, options) => {
          if (!reportedFailure && String(target).includes('.gin-vben-preflight-')) {
            reportedFailure = true;
            await rmAsync(target, options);
            throw Object.assign(new Error('cleanup acknowledgement failed'), { code: 'EIO' });
          }
          return rmAsync(target, options);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'admin_root'
        && error?.operation === 'delete',
    );
    assert.equal(reportedFailure, true);
    assert.deepEqual(
      readdirSync(root).filter((name) => name.startsWith('.gin-vben-preflight-')),
      [],
    );
  } finally {
    dispose(root);
  }
});

test('preflight never removes a matching-name file with an incomplete sentinel', async () => {
  const root = fixture();
  try {
    const probe = join(
      root,
      `.gin-vben-preflight-${process.pid}-12345678-1234-1234-1234-123456789abc`,
    );
    writeFileSync(probe, 'gin-vben-admin-preflight');
    await assert.rejects(
      () => preflightInitialization(root, 'antd'),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'admin_root'
        && error?.operation === 'read',
    );
    assert.equal(readFileSync(probe, 'utf8'), 'gin-vben-admin-preflight');
  } finally {
    dispose(root);
  }
});

test('resume preflight probes the actual transaction backup directory', async () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backupApps = join(stateRoot, 'ui-backup', transaction.id, 'apps');
    mkdirSync(backupApps, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backupApps, 'web-ele'));
    writeFileSync(join(stateRoot, 'transaction.json'), `${JSON.stringify(transaction, null, 2)}\n`);
    const repository = fixtureRepositories.get(root);
    const before = filesystemSnapshot(repository);

    await assert.rejects(
      () => preflightInitializationResume(root, {
        open: async (target, ...args) => {
          if (String(target).startsWith(backupApps) && String(target).includes('.gin-vben-preflight-')) {
            throw Object.assign(new Error('denied'), { code: 'EACCES' });
          }
          return openAsync(target, ...args);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'ui_backup'
        && error?.operation === 'create',
    );
    assert.deepEqual(filesystemSnapshot(repository), before);
  } finally {
    dispose(root);
  }
});

test('dependency resume preflight probes the transaction receipt parent', async () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const transactionDirectory = join(stateRoot, 'ui-backup', transaction.id);
    const backupApps = join(transactionDirectory, 'apps');
    mkdirSync(backupApps, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backupApps, 'web-ele'));
    renameSync(join(root, 'apps', 'web-naive'), join(backupApps, 'web-naive'));
    writeFileSync(join(stateRoot, 'transaction.json'), `${JSON.stringify(transaction, null, 2)}\n`);
    const repository = fixtureRepositories.get(root);
    const before = filesystemSnapshot(repository);

    await assert.rejects(
      () => preflightInitializationResume(root, {
        open: async (target, ...args) => {
          if (
            String(target).includes('.gin-vben-preflight-')
            && parse(String(target)).dir === transactionDirectory
          ) {
            throw Object.assign(new Error('denied'), { code: 'EACCES' });
          }
          return openAsync(target, ...args);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'ui_backup'
        && error?.operation === 'create',
    );
    assert.deepEqual(filesystemSnapshot(repository), before);
  } finally {
    dispose(root);
  }
});

test('dependency resume preflight checks the existing transaction file before replacement or deletion', async () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const transactionDirectory = join(stateRoot, 'ui-backup', transaction.id);
    const backupApps = join(transactionDirectory, 'apps');
    mkdirSync(backupApps, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backupApps, 'web-ele'));
    renameSync(join(root, 'apps', 'web-naive'), join(backupApps, 'web-naive'));
    const transactionFile = join(stateRoot, 'transaction.json');
    writeFileSync(transactionFile, `${JSON.stringify(transaction, null, 2)}\n`);

    await assert.rejects(
      () => preflightInitializationResume(root, {
        access: (target, mode) => {
          if (target === transactionFile) throw Object.assign(new Error('denied'), { code: 'EACCES' });
          return accessSync(target, mode);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'state_root'
        && error?.operation === 'write',
    );
  } finally {
    dispose(root);
  }
});

test('resume preflight removes its own stale probes after delete permission is restored', async () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backupApps = join(stateRoot, 'ui-backup', transaction.id, 'apps');
    mkdirSync(backupApps, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backupApps, 'web-ele'));
    writeFileSync(join(stateRoot, 'transaction.json'), `${JSON.stringify(transaction, null, 2)}\n`);
    const denyProbeDelete = async (target, ...args) => {
      if (parse(String(target)).dir === backupApps && String(target).includes('.gin-vben-preflight-')) {
        throw Object.assign(new Error('denied'), { code: 'EACCES' });
      }
      return rmAsync(target, ...args);
    };

    await assert.rejects(
      () => preflightInitializationResume(root, {
        cleanup: denyProbeDelete,
        remove: denyProbeDelete,
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'ui_backup'
        && error?.operation === 'delete',
    );
    assert.ok(readdirSync(backupApps).some((name) => name.startsWith('.gin-vben-preflight-')));

    await preflightInitializationResume(root);

    assert.equal(readdirSync(backupApps).some((name) => name.startsWith('.gin-vben-preflight-')), false);
  } finally {
    dispose(root);
  }
});

test('reset preflight ignores forward-only runtime env templates', async () => {
  const root = fixture();
  try {
    const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(prepared.status, 0, output(prepared));
    rmSync(join(root, 'apps', 'web-antd', '.env.development.example'));
    await preflightReset(root);

    const resetResult = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(resetResult.status, 0, output(resetResult));
    assert.match(output(resetResult), /INIT_NEXT=RESET_COMPLETE/);
  } finally {
    dispose(root);
  }
});

test('POSIX reset preflight checks the parent for deletion instead of requiring target write bits', async () => {
  const root = fixture();
  try {
    const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(prepared.status, 0, output(prepared));
    const profile = join(root, '.ui-profile.json');
    const targetCalls = [];
    const parentCalls = [];
    await assert.doesNotReject(() => preflightReset(root, {
      platform: 'linux',
      access: (target, mode) => {
        if (target === profile) {
          targetCalls.push(mode);
        }
        if (target === root) {
          parentCalls.push(mode);
        }
        return accessSync(target, mode);
      },
    }));
    assert.equal(targetCalls.length, 0);
    assert.ok(parentCalls.length > 0);
    assert.ok(parentCalls.every((mode) => (mode & fsConstants.W_OK) !== 0));
  } finally {
    dispose(root);
  }
});

test('Windows reset preflight checks a read-only target before deletion', async () => {
  const root = fixture();
  try {
    const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(prepared.status, 0, output(prepared));
    const profile = join(root, '.ui-profile.json');
    await assert.rejects(
      () => preflightReset(root, {
        platform: 'win32',
        access: (target, mode) => {
          if (target === profile && (mode & fsConstants.W_OK)) throw new Error('read-only target');
          return accessSync(target, mode);
        },
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'admin_root'
        && error?.operation === 'delete',
    );
  } finally {
    dispose(root);
  }
});

test('reset preflight removes its own stale transaction probes after permission is restored', async () => {
  const root = fixture();
  try {
    const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(prepared.status, 0, output(prepared));
    const backupRoot = join(root, '..', '.runtime', 'install', 'ui-backup');
    const [transactionId] = readdirSync(backupRoot);
    const transactionDirectory = join(backupRoot, transactionId);
    const denyProbeDelete = async (target, ...args) => {
      if (parse(String(target)).dir === transactionDirectory && String(target).includes('.gin-vben-preflight-')) {
        throw Object.assign(new Error('denied'), { code: 'EACCES' });
      }
      return rmAsync(target, ...args);
    };

    await assert.rejects(
      () => preflightReset(root, {
        cleanup: denyProbeDelete,
        remove: denyProbeDelete,
      }),
      (error) => error?.message === 'PREFLIGHT_FAILED'
        && error?.scope === 'ui_backup'
        && error?.operation === 'delete',
    );
    assert.ok(readdirSync(transactionDirectory).some((name) => name.startsWith('.gin-vben-preflight-')));

    await preflightReset(root);

    assert.equal(readdirSync(transactionDirectory).some((name) => name.startsWith('.gin-vben-preflight-')), false);
  } finally {
    dispose(root);
  }
});

test('init rejects an unsupported pnpm major before creating local state', () => {
  const root = fixture();
  try {
    const repository = fixtureRepositories.get(root);
    const before = filesystemSnapshot(repository);
    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], {
      npm_config_user_agent: 'pnpm/10.14.0 npm/? node/v24.19.0 win32 x64',
    });

    assert.equal(result.status, 1, output(result));
    assert.match(output(result), /INIT_ERROR=PNPM_VERSION_UNSUPPORTED/);
    assert.deepEqual(filesystemSnapshot(repository), before);
  } finally {
    dispose(root);
  }
});

test('build/dev/preview profile gate requires both a valid profile and future installer marker', () => {
  const root = fixture();
  try {
    const pristine = run(root, 'profile-gate.mjs', ['--command', 'build']);
    assert.equal(pristine.status, 2, output(pristine));
    assert.match(output(pristine), /INIT_STATE=pristine/);
    assert.match(output(pristine), /INIT_ERROR=PROFILE_REQUIRED/);

    const initialized = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(initialized.status, 0, output(initialized));

    const blocked = run(root, 'profile-gate.mjs', ['--command', 'build']);
    assert.equal(blocked.status, 2, output(blocked));
    assert.match(output(blocked), /INIT_STATE=ui_prepared/);
    assert.match(output(blocked), /INIT_ERROR=INSTALL_MARKER_REQUIRED/);

    writeFileSync(join(root, '..', '.runtime', 'install', '.installed'), JSON.stringify({
      schema_version: 1,
      installer_version: '0.4.0-dev',
      installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'antd',
      mode: 'embedded',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    }));
    for (const command of ['build', 'dev', 'preview']) {
      const allowed = run(root, 'profile-gate.mjs', ['--command', command]);
      assert.equal(allowed.status, 0, output(allowed));
      assert.match(output(allowed), /INIT_STATE=installed/);
      assert.match(output(allowed), /INIT_SELECTED_UI=antd/);
      assert.match(output(allowed), new RegExp(`INIT_NEXT=RUN_${command.toUpperCase()}`));
      assert.match(output(allowed), /INIT_ERROR=NONE/);
    }
  } finally {
    dispose(root);
  }
});

test('profile gate reports an inconsistent profile and a persistent transaction as stable states', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, '.ui-profile.json'), '{"schema":1,"selectedUi":"antd"}\n');
    const invalid = run(root, 'profile-gate.mjs', ['--command', 'preview']);
    assert.equal(invalid.status, 3, output(invalid));
    assert.match(output(invalid), /INIT_STATE=inconsistent/);
    assert.match(output(invalid), /INIT_ERROR=PROFILE_INVALID/);

    rmSync(join(root, '.ui-profile.json'));
    const transaction = join(root, '..', '.runtime', 'install', 'transaction.json');
    mkdirSync(join(root, '..', '.runtime', 'install'), { recursive: true });
    writeFileSync(transaction, '{"schema":1}\n');
    const active = run(root, 'profile-gate.mjs', ['--command', 'preview']);
    assert.equal(active.status, 3, output(active));
    assert.match(output(active), /INIT_STATE=inconsistent/);
    assert.match(output(active), /INIT_ERROR=PROFILE_INVALID/);
  } finally {
    dispose(root);
  }
});

test('admin initialization journals reject unknown or credential-like fields', () => {
  const root = fixture();
  try {
    const stateRoot = join(root, '..', '.runtime', 'install');
    mkdirSync(stateRoot, { recursive: true });
    const transactionPath = join(stateRoot, 'transaction.json');
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
      password: 'must-not-persist',
    };
    writeFileSync(transactionPath, JSON.stringify(transaction));
    const before = readFileSync(transactionPath, 'utf8');
    const result = run(root, 'init.mjs', ['--check', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_REASON=TRANSACTION_INVALID/);
    assert.equal(readFileSync(transactionPath, 'utf8'), before);
    assert.doesNotMatch(output(result), /must-not-persist/);
  } finally {
    dispose(root);
  }
});

test('removed analyze builds are rejected by the public command gate', () => {
  const root = fixture();
  try {
    const result = run(root, 'profile-gate.mjs', ['--command', 'build:analyze']);
    assert.equal(result.status, 2, output(result));
    assert.match(output(result), /INIT_ERROR=COMMAND_INVALID/);
  } finally {
    dispose(root);
  }
});

test('init safely quarantines an orphaned local receipt and continues first-time setup', () => {
  const root = fixture();
  try {
    const receiptPath = join(root, '.ui-init-receipt.json');
    const receipt = '{"schema":1,"transactionId":"12345678-1234-1234-1234-123456789abc","selectedUi":"antd","moves":[]}\n';
    writeFileSync(receiptPath, receipt);

    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /检测到上次初始化留下的本地状态，已安全备份并自动恢复。/);
    assert.match(output(result), /INIT_RECOVERY=completed/);
    assert.match(output(result), /INIT_RECOVERY_REASON=RECEIPT_WITHOUT_PROFILE/);
    assert.match(output(result), /INIT_RECOVERY_BACKUP=\.runtime\/install\/recovery\/[0-9a-f-]+/);
    assert.match(output(result), /INIT_STATE=ui_prepared/);
    assert.match(output(result), /INIT_SELECTED_UI=antd/);
    assert.equal(existsSync(join(root, '.ui-profile.json')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);

    const recoveryRoot = join(root, '..', '.runtime', 'install', 'recovery');
    const recoveryDirectories = readdirSync(recoveryRoot);
    assert.equal(recoveryDirectories.length, 1);
    assert.equal(
      readFileSync(join(recoveryRoot, recoveryDirectories[0], '.ui-init-receipt.json'), 'utf8'),
      receipt,
    );
  } finally {
    dispose(root);
  }
});

test('init safely quarantines an orphaned runtime record and continues first-time setup', () => {
  const root = fixture();
  try {
    const runtimePath = join(root, '.ui-init-runtime.json');
    const runtime = '{"schema":1,"port":8080,"pid":12345}\n';
    writeFileSync(runtimePath, runtime);

    const result = run(root, 'init.mjs', ['--ui', 'naive', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_RECOVERY=completed/);
    assert.match(output(result), /INIT_RECOVERY_REASON=RUNTIME_WITHOUT_PROFILE/);
    assert.match(output(result), /INIT_STATE=ui_prepared/);
    assert.match(output(result), /INIT_SELECTED_UI=naive/);

    const recoveryRoot = join(root, '..', '.runtime', 'install', 'recovery');
    const recoveryDirectories = readdirSync(recoveryRoot);
    assert.equal(recoveryDirectories.length, 1);
    assert.equal(
      readFileSync(join(recoveryRoot, recoveryDirectories[0], '.ui-init-runtime.json'), 'utf8'),
      runtime,
    );
  } finally {
    dispose(root);
  }
});

test('orphan recovery rejects non-directory and symlink parents without moving state or running pnpm', async (t) => {
  const cases = [
    {
      name: 'regular file',
      arrange(recoveryRoot) {
        writeFileSync(recoveryRoot, 'preserve recovery parent\n');
        return null;
      },
    },
    {
      name: 'external directory symlink',
      arrange(recoveryRoot) {
        const external = mkdtempSync(join(tmpdir(), 'gin-vben-external-recovery-'));
        symlinkSync(external, recoveryRoot);
        return external;
      },
    },
    {
      name: 'dangling symlink',
      arrange(recoveryRoot) {
        symlinkSync(join(recoveryRoot, '..', 'missing-recovery-target'), recoveryRoot);
        return null;
      },
    },
  ];
  for (const testCase of cases) {
    await t.test(testCase.name, () => {
      const root = fixture();
      let external;
      try {
        const receipt = join(root, '.ui-init-receipt.json');
        const receiptBytes = '{"schema":0}\n';
        writeFileSync(receipt, receiptBytes);
        const stateRoot = join(root, '..', '.runtime', 'install');
        mkdirSync(stateRoot, { recursive: true });
        const recoveryRoot = join(stateRoot, 'recovery');
        external = testCase.arrange(recoveryRoot);
        const protectedPaths = [
          receipt,
          recoveryRoot,
          join(root, '.ui-profile.json'),
          join(root, 'apps'),
          join(stateRoot, 'transaction.json'),
          ...(external ? [external] : []),
        ];
        const before = protectedPaths.map(filesystemSnapshot);
        const pnpmLog = join(root, `unsafe-recovery-${testCase.name.replaceAll(' ', '-')}.log`);
        const runner = join(root, 'scripts', 'unsafe-recovery-pnpm.mjs');
        writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

        const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], {
          INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
          INIT_PNPM_COMMAND: process.execPath,
          INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
          INIT_PNPM_LOG: pnpmLog,
        });

        assert.equal(result.status, 3, output(result));
        assert.match(output(result), /INIT_STATE=inconsistent/);
        assert.match(output(result), /INIT_REASON=RECEIPT_WITHOUT_PROFILE/);
        assert.equal(readFileSync(receipt, 'utf8'), receiptBytes);
        assert.equal(existsSync(pnpmLog), false);
        assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
      } finally {
        if (external) rmSync(external, { force: true, recursive: true });
        dispose(root);
      }
    });
  }
});

test('orphan recovery lstat errors fail closed before creating a recovery directory', async () => {
  const root = fixture();
  try {
    const receipt = join(root, '.ui-init-receipt.json');
    writeFileSync(receipt, '{"schema":0}\n');
    const recoveryRoot = join(root, '..', '.runtime', 'install', 'recovery');
    const protectedPaths = [receipt, recoveryRoot, join(root, 'apps'), join(root, '.ui-profile.json')];
    const before = protectedPaths.map(filesystemSnapshot);
    const recovered = await recoverSafeLocalState(root, {
      lstat: () => { throw Object.assign(new Error('I/O failure'), { code: 'EIO' }); },
    });
    assert.equal(recovered.recovered, false);
    assert.equal(recovered.reason, 'RECEIPT_WITHOUT_PROFILE');
    assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
    assert.equal(existsSync(join(root, 'node_modules')), false);
  } finally {
    dispose(root);
  }
});

test('dangling orphaned runtime state is fail-closed for check and automatic recovery', () => {
  const root = fixture();
  try {
    const runtime = join(root, '.ui-init-runtime.json');
    symlinkSync('missing-runtime-target.json', runtime);
    const stateRoot = join(root, '..', '.runtime', 'install');
    const protectedPaths = [
      runtime,
      join(root, '.ui-profile.json'),
      join(root, 'apps'),
      join(stateRoot, 'recovery'),
      join(stateRoot, 'transaction.json'),
    ];
    const before = protectedPaths.map(filesystemSnapshot);

    const checked = run(root, 'init.mjs', ['--check']);
    assert.equal(checked.status, 3, output(checked));
    assert.match(output(checked), /INIT_STATE=inconsistent/);
    assert.match(output(checked), /INIT_REASON=RUNTIME_WITHOUT_PROFILE/);
    assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);

    const pnpmLog = join(root, 'dangling-runtime-pnpm.log');
    const runner = join(root, 'scripts', 'dangling-runtime-pnpm.mjs');
    writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');
    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: pnpmLog,
    });

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_STATE=inconsistent/);
    assert.match(output(result), /INIT_REASON=RUNTIME_WITHOUT_PROFILE/);
    assert.equal(existsSync(pnpmLog), false);
    assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
  } finally {
    dispose(root);
  }
});

test('check explains a recoverable first-time state without asking users to inspect hidden files', () => {
  const root = fixture();
  try {
    const receiptPath = join(root, '.ui-init-receipt.json');
    const receipt = '{"schema":0}\n';
    writeFileSync(receiptPath, receipt);

    const result = run(root, 'init.mjs', ['--check', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /检测到可自动恢复的首次初始化状态。/);
    assert.match(output(result), /直接重新运行 pnpm run init，程序会先备份现场再继续。/);
    assert.match(output(result), /INIT_REASON=RECEIPT_WITHOUT_PROFILE/);
    assert.match(output(result), /INIT_ACTION=RUN_INIT_AUTO_RECOVERY/);
    assert.doesNotMatch(output(result), /\.ui-init-receipt\.json|git status|for %F|Get-Content/);
    assert.equal(readFileSync(receiptPath, 'utf8'), receipt);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'recovery')), false);
  } finally {
    dispose(root);
  }
});

test('legacy runtime records remain isolated compatibility state', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    writeFileSync(join(root, '.ui-init-runtime.json'), JSON.stringify({ schema: 1, port: 8080, pid: 12345 }));
    const active = run(root, 'profile-gate.mjs', ['--command', 'preview']);
    assert.equal(active.status, 3, output(active));
    assert.match(output(active), /INIT_STATE=installing/);
    assert.match(output(active), /INIT_ERROR=INITIALIZATION_IN_PROGRESS/);

    writeFileSync(join(root, '.ui-init-runtime.json'), '{"schema":0}\n');
    const invalid = run(root, 'profile-gate.mjs', ['--command', 'preview']);
    assert.equal(invalid.status, 3, output(invalid));
    assert.match(output(invalid), /INIT_STATE=inconsistent/);
    assert.match(output(invalid), /INIT_ERROR=PROFILE_INVALID/);
  } finally {
    dispose(root);
  }
});

test('check is read-only and non-terminal cleanup/reset confirmations are explicit', () => {
  const root = fixture();
  try {
    const check = run(root, 'init.mjs', ['--check', '--port', '9090']);
    assert.equal(check.status, 0, output(check));
    assert.match(output(check), /INIT_STATE=pristine/);
    assert.match(output(check), /INIT_URL=http:\/\/127\.0\.0\.1:9090\/install/);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);

    const missingCleanupConfirmation = run(root, 'init.mjs', ['--ui', 'antd', '--no-open']);
    assert.equal(missingCleanupConfirmation.status, 2, output(missingCleanupConfirmation));
    assert.match(output(missingCleanupConfirmation), /INIT_ERROR=CLEANUP_CONFIRMATION_REQUIRED/);

    const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(prepared.status, 0, output(prepared));
    const missingResetConfirmation = run(root, 'init.mjs', ['--reset', '--no-open']);
    assert.equal(missingResetConfirmation.status, 2, output(missingResetConfirmation));
    assert.match(output(missingResetConfirmation), /INIT_ERROR=RESET_CONFIRMATION_REQUIRED/);

    const reset = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(reset.status, 0, output(reset));
    assert.deepEqual(
      [...output(reset).matchAll(/^INIT_STAGE=(.+)$/gm)].map((match) => match[1]),
      ['reset:preflight', 'reset:workspace', 'reset:complete'],
    );
    assert.match(output(reset), /INIT_STATE=pristine/);
    assert.match(output(reset), /INIT_NEXT=RESET_COMPLETE/);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    assert.equal(existsSync(join(root, '.ui-init-receipt.json')), false);
    for (const ui of ['antd', 'ele', 'naive']) assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
  } finally {
    dispose(root);
  }
});

test('installer marker is schema checked against the selected profile and installed state rejects reset', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']).status, 0);
    const marker = join(root, '..', '.runtime', 'install', '.installed');
    writeFileSync(marker, JSON.stringify({ schema_version: 1, selected_ui: 'naive' }));
    const mismatch = run(root, 'profile-gate.mjs', ['--command', 'build']);
    assert.equal(mismatch.status, 3, output(mismatch));
    assert.match(output(mismatch), /INIT_STATE=inconsistent/);
    assert.match(output(mismatch), /INIT_ERROR=PROFILE_INVALID/);

    writeFileSync(marker, JSON.stringify({
      schema_version: 1, installer_version: '0.4.0-dev', installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'ele', mode: 'embedded', artifact_hash: 'a'.repeat(64), manifest_hash: 'b'.repeat(64),
    }));
    const reset = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(reset.status, 3, output(reset));
    assert.match(output(reset), /INIT_STATE=installed/);
    assert.match(output(reset), /INIT_ERROR=RESET_UNAVAILABLE_INSTALLED/);
  } finally {
    dispose(root);
  }
});

test('build remains blocked while the installer marker lock exists', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    writeFileSync(join(stateRoot, '.installed'), JSON.stringify({
      schema_version: 1, installer_version: '0.4.0-dev', installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'antd', mode: 'dev', artifact_hash: 'a'.repeat(64), manifest_hash: 'b'.repeat(64),
    }));
    writeFileSync(join(stateRoot, '.installed.lock'), '');
    const result = run(root, 'profile-gate.mjs', ['--command', 'build']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_STATE=installing/);
    assert.match(output(result), /INIT_ERROR=INITIALIZATION_IN_PROGRESS/);
  } finally {
    dispose(root);
  }
});

test('the generic build dispatches only to the selected UI package', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    writeFileSync(join(stateRoot, '.installed'), JSON.stringify({
      schema_version: 1, installer_version: '0.4.0-dev', installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'ele', mode: 'dev', artifact_hash: 'a'.repeat(64), manifest_hash: 'b'.repeat(64),
    }));
    const runner = join(root, 'scripts', 'record-dispatch.mjs');
    const log = join(root, 'dispatch.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_DISPATCH_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));
    const result = run(root, 'selected-dispatch.mjs', ['--command', 'build'], {
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_DISPATCH_LOG: log,
    });
    assert.equal(result.status, 0, output(result));
    assert.equal(readFileSync(log, 'utf8'), '-F @vben/web-ele run build');
  } finally {
    dispose(root);
  }
});

test('an externally installed non-destructive selection can run without an init receipt', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    writeFileSync(join(root, '.ui-profile.local.json'), `${JSON.stringify({
      schema: 1,
      selectedUi: 'ele',
      packageName: '@vben/web-ele',
      appDirectory: 'apps/web-ele',
    }, null, 2)}\n`);

    // The documented quick start performs the filtered pnpm install directly,
    // so no init-owned dependency receipt exists at this public CLI seam.
    const gate = run(root, 'profile-gate.mjs', ['--command', 'dev']);
    assert.equal(gate.status, 0, output(gate));
    assert.match(output(gate), /INIT_STATE=ui_prepared/);
    assert.match(output(gate), /INIT_ACTION=RUN_SELECTED_APP/);
    assert.match(output(gate), /INIT_NEXT=RUN_DEV/);
    assert.match(output(gate), /INIT_ERROR=NONE/);

    const runner = join(root, 'scripts', 'record-workspace-dispatch.mjs');
    const log = join(root, 'workspace-dispatch.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_DISPATCH_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));
    const dispatch = run(root, 'selected-dispatch.mjs', ['--command', 'dev'], {
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_DISPATCH_LOG: log,
    });
    assert.equal(dispatch.status, 0, output(dispatch));
    assert.equal(readFileSync(log, 'utf8'), '-F @vben/web-ele run dev');
  } finally {
    dispose(root);
  }
});

test('an environment UI override treats the durable UI receipt as a cache miss and dispatches the override', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    const initialized = run(root, 'init.mjs', ['--ui', 'ele', '--no-open']);
    assert.equal(initialized.status, 0, output(initialized));
    const receipt = join(root, '..', '.runtime', 'install', 'workspace-dependencies.json');
    const receiptBytes = readFileSync(receipt);

    const gate = run(root, 'profile-gate.mjs', ['--command', 'dev'], { ADMIN_UI: 'naive' });
    assert.equal(gate.status, 0, output(gate));
    assert.match(output(gate), /INIT_SELECTED_UI=naive/);
    assert.match(output(gate), /INIT_ACTION=RUN_SELECTED_APP/);

    const runner = join(root, 'scripts', 'record-override-dispatch.mjs');
    const log = join(root, 'override-dispatch.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_DISPATCH_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));
    const dispatch = run(root, 'selected-dispatch.mjs', ['--command', 'dev'], {
      ADMIN_UI: 'naive',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_DISPATCH_LOG: log,
    });
    assert.equal(dispatch.status, 0, output(dispatch));
    assert.equal(readFileSync(log, 'utf8'), '-F @vben/web-naive run dev');
    assert.deepEqual(readFileSync(receipt), receiptBytes);
    assert.equal(JSON.parse(readFileSync(join(root, '.ui-profile.local.json'), 'utf8')).selectedUi, 'ele');
  } finally {
    dispose(root);
  }
});

test('ui install runs the selected dependency closure and refreshes its optional receipt', () => {
  const root = fixture();
  try {
    const source = readFileSync(join(sourceRoot, 'scripts', 'ui-install.mjs'), 'utf8');
    assert.ok(
      source.indexOf('const releaseAdmin = await acquireAdminInitLease(root)')
        < source.indexOf('const resolved = resolveWorkspaceProfile(root)'),
      'the selected profile must be resolved only after the lease is held',
    );
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    writeFileSync(join(root, '.ui-profile.local.json'), `${JSON.stringify({
      schema: 1,
      selectedUi: 'ele',
      packageName: '@vben/web-ele',
      appDirectory: 'apps/web-ele',
    })}\n`);
    const runner = join(root, 'scripts', 'record-ui-install.mjs');
    const log = join(root, 'ui-install.log');
    writeFileSync(runner, [
      'import { appendFileSync } from "node:fs";',
      'appendFileSync(process.env.INIT_PNPM_LOG, `${process.argv.slice(2).join(" ")}\\n`);',
    ].join('\n'));
    const result = run(root, 'ui-install.mjs', [], {
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });
    assert.equal(result.status, 0, output(result));
    assert.equal(readFileSync(log, 'utf8'), 'install --filter @vben/web-ele... --frozen-lockfile\n');
    const receipt = JSON.parse(readFileSync(join(root, '..', '.runtime', 'install', 'workspace-dependencies.json'), 'utf8'));
    assert.equal(receipt.selectedUi, 'ele');
    assert.equal(receipt.dependenciesReady, true);

    const repeated = run(root, 'ui-install.mjs', [], {
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });
    assert.equal(repeated.status, 0, output(repeated));
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), [
      'install --filter @vben/web-ele... --frozen-lockfile',
      'install --filter @vben/web-ele... --frozen-lockfile',
    ]);
  } finally {
    dispose(root);
  }
});

test('ui install never certifies dependencies when its lockfile changes during pnpm', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\nrevision: before\n');
    writeFileSync(join(root, '.ui-profile.local.json'), `${JSON.stringify({
      schema: 1,
      selectedUi: 'ele',
      packageName: '@vben/web-ele',
      appDirectory: 'apps/web-ele',
    })}\n`);
    const runner = join(root, 'scripts', 'change-lockfile-during-install.mjs');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_LOCKFILE, "lockfileVersion: 9\\nrevision: after\\n");',
    ].join('\n'));

    const result = run(root, 'ui-install.mjs', [], {
      INIT_LOCKFILE: join(root, 'pnpm-lock.yaml'),
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
    });
    assert.equal(result.status, 1, output(result));
    assert.match(output(result), /UI_INSTALL_ERROR=DEPENDENCY_INSTALL_FAILED/);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'workspace-dependencies.json')), false);
  } finally {
    dispose(root);
  }
});

test('workspace init hands its exact admin lease to the dependency supervisor', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    const runner = join(root, 'scripts', 'record-supervised-workspace-install.mjs');
    const log = join(root, 'supervised-workspace-install.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_PNPM_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));

    const result = run(root, 'init.mjs', ['--ui', 'ele', '--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });
    assert.equal(result.status, 0, output(result));
    assert.equal(readFileSync(log, 'utf8'), 'install --filter @vben/web-ele... --frozen-lockfile');
    const receipt = JSON.parse(readFileSync(
      join(root, '..', '.runtime', 'install', 'workspace-dependencies.json'),
      'utf8',
    ));
    assert.equal(receipt.selectedUi, 'ele');
    assert.equal(existsSync(adminLeasePath(root)), false);
    assert.equal(existsSync(join(root, '..', '.runtime', 'install', 'dependency-install.lock')), false);
  } finally {
    dispose(root);
  }
});

test('a killed ui install parent leaves its supervisor lease covering the live pnpm runner', async () => {
  const root = fixture();
  let first;
  try {
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    writeFileSync(join(root, '.ui-profile.local.json'), `${JSON.stringify({
      schema: 1,
      selectedUi: 'ele',
      packageName: '@vben/web-ele',
      appDirectory: 'apps/web-ele',
    })}\n`);
    const runner = join(root, 'scripts', 'slow-ui-install-runner.mjs');
    const ready = join(root, 'slow-ui-install-ready');
    const log = join(root, 'slow-ui-install.log');
    writeFileSync(runner, [
      'import { appendFileSync, writeFileSync } from "node:fs";',
      'appendFileSync(process.env.INIT_PNPM_LOG, "START\\n");',
      'writeFileSync(process.env.INIT_PNPM_READY, String(process.pid));',
      'await new Promise((resolveDelay) => setTimeout(resolveDelay, 800));',
      'appendFileSync(process.env.INIT_PNPM_LOG, "END\\n");',
    ].join('\n'));
    const env = {
      ...process.env,
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
      INIT_PNPM_READY: ready,
    };
    first = spawn(process.execPath, [join(root, 'scripts', 'ui-install.mjs')], {
      cwd: root,
      env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    const firstExit = new Promise((resolveExit) => {
      first.once('exit', (status, signal) => resolveExit({ status, signal }));
    });
    const readyDeadline = Date.now() + 2_000;
    while (!existsSync(ready) && Date.now() < readyDeadline) {
      await new Promise((resolveDelay) => setTimeout(resolveDelay, 10));
    }
    assert.equal(existsSync(ready), true);

    first.kill('SIGKILL');
    const killed = await firstExit;
    assert.notEqual(killed.status, 0);

    const blocked = run(root, 'ui-select.mjs', ['naive']);
    assert.equal(blocked.status, 1, output(blocked));
    assert.match(output(blocked), /INIT_BUSY/);
    assert.equal(JSON.parse(readFileSync(join(root, '.ui-profile.local.json'), 'utf8')).selectedUi, 'ele');
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), ['START']);

    const stateRoot = join(root, '..', '.runtime', 'install');
    const completionDeadline = Date.now() + 3_000;
    while (
      (
        !existsSync(log)
        || !readFileSync(log, 'utf8').includes('END\n')
        || existsSync(join(stateRoot, 'dependency-install.lock'))
      )
      && Date.now() < completionDeadline
    ) await new Promise((resolveDelay) => setTimeout(resolveDelay, 20));
    assert.deepEqual(readFileSync(log, 'utf8').trim().split('\n'), ['START', 'END']);
    assert.equal(existsSync(join(stateRoot, 'dependency-install.lock')), false);

    const switched = run(root, 'ui-select.mjs', ['naive']);
    assert.equal(switched.status, 0, output(switched));
    assert.equal(JSON.parse(readFileSync(join(root, '.ui-profile.local.json'), 'utf8')).selectedUi, 'naive');
  } finally {
    if (first && first.exitCode === null && first.signalCode === null) first.kill('SIGKILL');
    dispose(root);
  }
});

test('an interrupted installed UI switch resumes dependency preparation instead of reporting already installed', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    const initialized = run(root, 'init.mjs', ['--ui', 'ele', '--no-open']);
    assert.equal(initialized.status, 0, output(initialized));

    const stateRoot = join(root, '..', '.runtime', 'install');
    writeFileSync(join(stateRoot, '.installed'), JSON.stringify({
      schema_version: 1,
      installer_version: '0.4.0-dev',
      installed_at: '2026-08-28T00:00:00Z',
      selected_ui: 'ele',
      mode: 'dev',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    }));

    const crashRunner = join(root, 'crash-workspace-initialize.mjs');
    const stateModule = new URL('../scripts/init-state.mjs', import.meta.url).href;
    writeFileSync(crashRunner, [
      `import { initializeWorkspace } from ${JSON.stringify(stateModule)};`,
      `await initializeWorkspace(${JSON.stringify(root)}, 'naive', {`,
      '  afterSelection: () => process.exit(89),',
      '});',
      '',
    ].join('\n'));
    const crashed = spawnSync(process.execPath, [crashRunner], {
      cwd: root,
      encoding: 'utf8',
      env: { ...process.env },
    });
    assert.equal(crashed.status, 89, output(crashed));
    const pendingBeforeRetry = JSON.parse(readFileSync(join(stateRoot, 'workspace-transaction.json'), 'utf8'));
    assert.equal(pendingBeforeRetry.phase, 'dependencies_pending');
    assert.equal(pendingBeforeRetry.selectedUi, 'naive');

    const resumed = run(root, 'init.mjs', ['--no-open']);
    assert.equal(resumed.status, 0, output(resumed));
    assert.doesNotMatch(output(resumed), /INIT_NEXT=ALREADY_INSTALLED/);
    assert.match(output(resumed), /INIT_STAGE=prepare:dependencies/);
    assert.equal(existsSync(join(stateRoot, 'workspace-transaction.json')), false);
    assert.equal(
      JSON.parse(readFileSync(join(stateRoot, 'workspace-dependencies.json'), 'utf8')).selectedUi,
      'naive',
    );
  } finally {
    dispose(root);
  }
});

test('an installed environment-selected target cannot bypass its durable UI switch', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, 'pnpm-workspace.yaml'), 'packages:\n  - apps/*\n');
    writeFileSync(join(root, 'pnpm-lock.yaml'), 'lockfileVersion: 9\n');
    assert.equal(run(root, 'init.mjs', ['--ui', 'ele', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    writeFileSync(join(stateRoot, '.installed'), JSON.stringify({
      schema_version: 1,
      installer_version: '0.4.0-dev',
      installed_at: '2026-08-28T00:00:00Z',
      selected_ui: 'ele',
      mode: 'dev',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    }));

    const switched = run(root, 'init.mjs', ['--ui', 'naive', '--no-open'], {
      ADMIN_UI: 'naive',
    });
    assert.equal(switched.status, 0, output(switched));
    assert.doesNotMatch(output(switched), /INIT_NEXT=ALREADY_INSTALLED/);
    assert.equal(
      JSON.parse(readFileSync(join(root, '.ui-profile.local.json'), 'utf8')).selectedUi,
      'naive',
    );
    assert.equal(
      JSON.parse(readFileSync(join(stateRoot, 'workspace-dependencies.json'), 'utf8')).selectedUi,
      'naive',
    );
  } finally {
    dispose(root);
  }
});

test('init materializes selected UI runtime env files without overwriting local values', () => {
  const root = fixture();
  try {
    const selected = join(root, 'apps', 'web-ele');
    const localDevelopment = 'VITE_APP_TITLE=Local Admin\nVITE_GLOB_API_URL=/custom-api\n';
    writeFileSync(join(selected, '.env.development'), localDevelopment);

    const result = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 0, output(result));
    assert.equal(readFileSync(join(selected, '.env.development'), 'utf8'), localDevelopment);
    assert.equal(
      readFileSync(join(selected, '.env.production'), 'utf8'),
      readFileSync(join(selected, '.env.production.example'), 'utf8'),
    );
  } finally {
    dispose(root);
  }
});

test('selected dispatch repairs runtime env files for an already-installed legacy profile', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']).status, 0);
    const selected = join(root, 'apps', 'web-ele');
    rmSync(join(selected, '.env.development'), { force: true });
    rmSync(join(selected, '.env.production'), { force: true });
    const stateRoot = join(root, '..', '.runtime', 'install');
    writeFileSync(join(stateRoot, '.installed'), JSON.stringify({
      schema_version: 1, installer_version: '0.4.0-dev', installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'ele', mode: 'dev', artifact_hash: 'a'.repeat(64), manifest_hash: 'b'.repeat(64),
    }));
    const runner = join(root, 'scripts', 'record-runtime-env-dispatch.mjs');
    writeFileSync(runner, 'process.exit(0);\n');

    const result = run(root, 'selected-dispatch.mjs', ['--command', 'dev'], {
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
    });
    assert.equal(result.status, 0, output(result));
    assert.equal(
      readFileSync(join(selected, '.env.development'), 'utf8'),
      readFileSync(join(selected, '.env.development.example'), 'utf8'),
    );
    assert.equal(
      readFileSync(join(selected, '.env.production'), 'utf8'),
      readFileSync(join(selected, '.env.production.example'), 'utf8'),
    );
  } finally {
    dispose(root);
  }
});

test('selected dispatch leaves existing legacy runtime env files byte-for-byte unchanged', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    const selected = join(root, 'apps', 'web-antd');
    const legacyDevelopment = [
      '# legacy local settings',
      'VITE_PORT=6999',
      'VITE_GLOB_API_URL=/custom-api',
      'VITE_LOCAL_ONLY=keep-me',
      '',
    ].join('\n');
    const legacyProduction = 'VITE_GLOB_API_URL=https://api.example.test\n';
    writeFileSync(join(selected, '.env.development'), legacyDevelopment);
    writeFileSync(join(selected, '.env.production'), legacyProduction);
    const stateRoot = join(root, '..', '.runtime', 'install');
    writeFileSync(join(stateRoot, '.installed'), JSON.stringify({
      schema_version: 1, installer_version: '0.4.0-dev', installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'antd', mode: 'dev', artifact_hash: 'a'.repeat(64), manifest_hash: 'b'.repeat(64),
    }));
    const runner = join(root, 'scripts', 'record-runtime-env-dispatch.mjs');
    writeFileSync(runner, 'process.exit(0);\n');

    const result = run(root, 'selected-dispatch.mjs', ['--command', 'dev'], {
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
    });
    assert.equal(result.status, 0, output(result));

    assert.equal(readFileSync(join(selected, '.env.development'), 'utf8'), legacyDevelopment);
    assert.equal(readFileSync(join(selected, '.env.production'), 'utf8'), legacyProduction);
  } finally {
    dispose(root);
  }
});

test('selected dispatch reports a stable runtime env diagnostic without exposing file contents', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'naive', '--confirm-cleanup', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    writeFileSync(join(stateRoot, '.installed'), JSON.stringify({
      schema_version: 1, installer_version: '0.4.0-dev', installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'naive', mode: 'dev', artifact_hash: 'a'.repeat(64), manifest_hash: 'b'.repeat(64),
    }));
    const template = join(root, 'apps', 'web-naive', '.env.development.example');
    rmSync(template);
    mkdirSync(template);

    const result = run(root, 'selected-dispatch.mjs', ['--command', 'dev']);
    assert.equal(result.status, 1, output(result));
    assert.match(output(result), /RUNTIME_ENV_PREPARATION_FAILED/);
    assert.match(output(result), /RUNTIME_ENV_ERROR=RUNTIME_ENV_TEMPLATE_INVALID/);
    assert.doesNotMatch(output(result), /VITE_|\/custom-api|password/);
  } finally {
    dispose(root);
  }
});

test('runtime env publication preserves a local file created during the no-clobber race', async () => {
  const root = fixture();
  try {
    const selected = join(root, 'apps', 'web-ele');
    const target = join(selected, '.env.development');
    const localDevelopment = 'VITE_GLOB_API_URL=/concurrent-api\n';
    let raced = false;
    await ensureSelectedUIRuntimeEnv(root, {
      schema: 1,
      selectedUi: 'ele',
      packageName: '@vben/web-ele',
      appDirectory: 'apps/web-ele',
    }, {
      publish: async (file, contents, label) => {
        if (!raced && file === target) {
          raced = true;
          writeFileSync(file, localDevelopment);
          throw Object.assign(new Error('target appeared'), { code: 'EEXIST' });
        }
        const temporary = `${file}.${label}-test`;
        writeFileSync(temporary, contents, { flag: 'wx', mode: 0o600 });
        linkSync(temporary, file);
        rmSync(temporary);
      },
    });

    assert.equal(raced, true);
    assert.equal(readFileSync(target, 'utf8'), localDevelopment);
    assert.equal(
      readFileSync(join(selected, '.env.production'), 'utf8'),
      readFileSync(join(selected, '.env.production.example'), 'utf8'),
    );
  } finally {
    dispose(root);
  }
});

test('launcher injection receives the selected install URL while no launcher is never reported as started', () => {
  const root = fixture();
  try {
    const launcher = join(root, 'scripts', 'fake-launcher.mjs');
    writeFileSync(launcher, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_LAUNCH_LOG, process.argv.slice(2).join(" "));');
    const log = join(root, 'launcher.log');
    const result = run(root, 'init.mjs', ['--ui', 'naive', '--confirm-cleanup', '--port', '9191'], {
      INIT_LAUNCHER: launcher,
      INIT_LAUNCH_LOG: log,
    });
    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_URL=http:\/\/127\.0\.0\.1:9191\/install/);
    assert.match(output(result), /INIT_NEXT=INSTALLER_LAUNCHED/);
    assert.equal(readFileSync(log, 'utf8'), 'http://127.0.0.1:9191/install');

    const invalidPort = run(root, 'init.mjs', ['--check', '--port', '0']);
    assert.equal(invalidPort.status, 2, output(invalidPort));
    assert.match(output(invalidPort), /INIT_ERROR=PORT_INVALID/);
  } finally {
    dispose(root);
  }
});

test('init uses the ordinary API install page without a temporary runtime launcher', () => {
  assert.equal(existsSync(join(sourceRoot, 'scripts', 'init-runtime.mjs')), false);
  const source = readFileSync(join(sourceRoot, 'scripts', 'init.mjs'), 'utf8');
  assert.doesNotMatch(source, /cmd\/init|build:installer|runInstallerRuntime/);
  assert.match(source, /installURL/);
});

test('profile remains source-controlled while receipts stay local and public scripts dispatch only the selected package', () => {
  const root = fixture();
  try {
    const ignore = readFileSync(join(sourceRoot, '..', '.gitignore'), 'utf8');
    assert.doesNotMatch(ignore, /^\/admin\/\.ui-profile\.json$/m);
    assert.match(ignore, /^\/admin\/\.ui-init-receipt\.json$/m);
    const pkg = JSON.parse(readFileSync(join(sourceRoot, 'package.json'), 'utf8'));
    for (const command of ['build', 'dev', 'preview']) {
      assert.match(pkg.scripts[command], /profile-gate/);
      assert.match(pkg.scripts[command], /selected-dispatch/);
      assert.doesNotMatch(pkg.scripts[command], /turbo-run|turbo build/);
    }
    assert.match(pkg.scripts.build, /NODE_OPTIONS=--max-old-space-size=8192/);
    for (const command of [
      'build:analyze',
      'build:antd',
      'build:ele',
      'build:naive',
      'dev:antd',
      'dev:ele',
      'dev:naive',
    ]) {
      assert.equal(Object.hasOwn(pkg.scripts, command), false, command);
    }
  } finally {
    dispose(root);
  }
});

test('a fresh clone profile without local receipt is prepared, while invalid receipts or extra templates are inconsistent', () => {
  const root = fixture();
  try {
    rmSync(join(root, 'apps', 'web-ele'), { recursive: true });
    rmSync(join(root, 'apps', 'web-naive'), { recursive: true });
    writeFileSync(join(root, '.ui-profile.json'), JSON.stringify({
      schema: 1, selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd',
    }));
    const clone = run(root, 'init.mjs', ['--check']);
    assert.equal(clone.status, 0, output(clone));
    assert.match(output(clone), /INIT_STATE=ui_prepared/);

    const marker = join(root, '..', '.runtime', 'install', '.installed');
    mkdirSync(join(root, '..', '.runtime', 'install'), { recursive: true });
    writeFileSync(marker, JSON.stringify({
      schema_version: 1, installer_version: '0.4.0-dev', installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'antd', mode: 'embedded', artifact_hash: 'a'.repeat(64), manifest_hash: 'b'.repeat(64),
    }));
    const installedClone = run(root, 'init.mjs', ['--check']);
    assert.equal(installedClone.status, 0, output(installedClone));
    assert.match(output(installedClone), /INIT_STATE=installed/);
    rmSync(marker);

    writeFileSync(join(root, '.ui-init-receipt.json'), '{"schema":0}\n');
    const corruptReceipt = run(root, 'profile-gate.mjs', ['--command', 'build']);
    assert.equal(corruptReceipt.status, 3, output(corruptReceipt));
    assert.match(output(corruptReceipt), /INIT_STATE=inconsistent/);

    rmSync(join(root, '.ui-init-receipt.json'));
    mkdirSync(join(root, 'apps', 'web-ele'));
    writeFileSync(join(root, 'apps', 'web-ele', 'package.json'), JSON.stringify({ name: '@vben/web-ele' }));
    const extraTemplate = run(root, 'init.mjs', ['--check']);
    assert.equal(extraTemplate.status, 3, output(extraTemplate));
    assert.match(output(extraTemplate), /INIT_STATE=inconsistent/);
    assert.match(output(extraTemplate), /INIT_SELECTED_UI=antd/);
    assert.match(output(extraTemplate), /INIT_REASON=EXTRA_TEMPLATE_PRESENT/);

    const blocked = run(root, 'profile-gate.mjs', ['--command', 'dev']);
    assert.equal(blocked.status, 3, output(blocked));
    assert.match(output(blocked), /INIT_SELECTED_UI=antd/);
    assert.match(output(blocked), /INIT_ACTION=REMOVE_UNSELECTED_UI_WORKSPACE/);
    assert.match(output(blocked), /INIT_NEXT=REMOVE_UNSELECTED_UI_WORKSPACE/);
    assert.match(output(blocked), /INIT_ERROR=UNSELECTED_UI_WORKSPACE_PRESENT/);
  } finally {
    dispose(root);
  }
});

test('an installed selected UI remains runnable when git pull restores partial unselected template paths', () => {
  const root = fixture();
  try {
    const initialized = run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']);
    assert.equal(initialized.status, 0, output(initialized));

    const marker = join(root, '..', '.runtime', 'install', '.installed');
    writeFileSync(marker, JSON.stringify({
      schema_version: 1,
      installer_version: '0.4.0-dev',
      installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'ele',
      mode: 'dev',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    }));

    // A pull can materialize changed tracked files below the two unselected
    // template paths without restoring their package manifests. These fragments
    // are not pnpm workspaces and must not erase the committed UI selection.
    for (const ui of ['antd', 'naive']) {
      const directory = join(root, 'apps', `web-${ui}`, 'src', 'api', 'core');
      mkdirSync(directory, { recursive: true });
      writeFileSync(join(directory, 'pulled-update.ts'), 'export const pulledUpdate = true;\n');
    }

    const dev = run(root, 'profile-gate.mjs', ['--command', 'dev']);
    assert.equal(dev.status, 0, output(dev));
    assert.match(output(dev), /INIT_STATE=installed/);
    assert.match(output(dev), /INIT_SELECTED_UI=ele/);
    assert.match(output(dev), /INIT_REASON=NONE/);
    assert.match(output(dev), /INIT_ACTION=RUN_SELECTED_APP/);
    assert.match(output(dev), /INIT_NEXT=RUN_DEV/);
    assert.match(output(dev), /INIT_ERROR=NONE/);

    const runner = join(root, 'scripts', 'record-pulled-layout-dispatch.mjs');
    const log = join(root, 'pulled-layout-dispatch.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_PNPM_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));
    const dispatch = run(root, 'selected-dispatch.mjs', ['--command', 'dev'], {
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });
    assert.equal(dispatch.status, 0, output(dispatch));
    assert.equal(readFileSync(log, 'utf8'), '-F @vben/web-ele run dev');
  } finally {
    dispose(root);
  }
});

test('a prepared selected UI remains resumable when git pull restores only partial unselected paths', () => {
  const root = fixture();
  try {
    const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(prepared.status, 0, output(prepared));
    const pulledDirectory = join(root, 'apps', 'web-ele', 'src');
    mkdirSync(pulledDirectory, { recursive: true });
    writeFileSync(join(pulledDirectory, 'pulled.ts'), 'export const pulled = true;\n');

    const resumed = run(root, 'init.mjs', ['--no-open']);

    assert.equal(resumed.status, 0, output(resumed));
    assert.match(output(resumed), /INIT_STATE=ui_prepared/);
    assert.match(output(resumed), /INIT_SELECTED_UI=antd/);
    assert.equal(existsSync(join(root, 'apps', 'web-ele', 'package.json')), false);
  } finally {
    dispose(root);
  }
});

test('a fresh clone with a tracked single-UI profile still installs local dependencies', () => {
  const root = fixture();
  try {
    rmSync(join(root, 'apps', 'web-ele'), { recursive: true });
    rmSync(join(root, 'apps', 'web-naive'), { recursive: true });
    writeFileSync(join(root, '.ui-profile.json'), JSON.stringify({
      schema: 1, selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd',
    }));
    const runner = join(root, 'scripts', 'fresh-clone-pnpm.mjs');
    const log = join(root, 'fresh-clone-pnpm.log');
    writeFileSync(runner, [
      'import { writeFileSync } from "node:fs";',
      'writeFileSync(process.env.INIT_PNPM_LOG, process.argv.slice(2).join(" "));',
    ].join('\n'));
    const result = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: log,
    });
    assert.equal(result.status, 0, output(result));
    assert.deepEqual(
      [...output(result).matchAll(/^INIT_STAGE=(.+)$/gm)].map((match) => match[1]),
      ['prepare:preflight', 'prepare:dependencies', 'prepare:complete'],
    );
    assert.equal(readFileSync(log, 'utf8'), 'install --frozen-lockfile');
    assert.match(output(result), /INIT_NEXT=OPEN_INSTALLER/);
  } finally {
    dispose(root);
  }
});

test('reset without a local receipt preserves a fresh-clone profile and reports a stable error', () => {
  const root = fixture();
  try {
    rmSync(join(root, 'apps', 'web-ele'), { recursive: true });
    rmSync(join(root, 'apps', 'web-naive'), { recursive: true });
    writeFileSync(join(root, '.ui-profile.json'), JSON.stringify({
      schema: 1, selectedUi: 'antd', packageName: '@vben/web-antd', appDirectory: 'apps/web-antd',
    }));
    const reset = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(reset.status, 3, output(reset));
    assert.match(output(reset), /INIT_STATE=ui_prepared/);
    assert.match(output(reset), /INIT_ERROR=RESET_RECEIPT_UNAVAILABLE/);
    assert.equal(existsSync(join(root, '.ui-profile.json')), true);
  } finally {
    dispose(root);
  }
});

test('reset rejects a backup receipt outside the UUID transaction namespace', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    const backupRoot = join(root, '..', '.runtime', 'install', 'ui-backup');
    const [transactionId] = readdirSync(backupRoot);
    const invalidId = 'not-a-transaction';
    renameSync(join(backupRoot, transactionId), join(backupRoot, invalidId));
    const receiptPath = join(backupRoot, invalidId, 'receipt.json');
    const receipt = JSON.parse(readFileSync(receiptPath, 'utf8'));
    receipt.transactionId = invalidId;
    writeFileSync(receiptPath, JSON.stringify(receipt));

    const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=RESET_RECEIPT_UNAVAILABLE/);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
  } finally {
    dispose(root);
  }
});

test('reset rejects backup transactions with unexpected top-level entries', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'ele', '--confirm-cleanup', '--no-open']).status, 0);
    const backupRoot = join(root, '..', '.runtime', 'install', 'ui-backup');
    const [transactionId] = readdirSync(backupRoot);
    writeFileSync(join(backupRoot, transactionId, 'unexpected.txt'), 'keep');

    const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=RESET_RECEIPT_UNAVAILABLE/);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
  } finally {
    dispose(root);
  }
});

test('reset rejects a symlinked ui-backup root without touching external templates or source state', () => {
  const root = fixture();
  const externalParent = mkdtempSync(join(tmpdir(), 'gin-vben-external-ui-backup-'));
  try {
    const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(prepared.status, 0, output(prepared));
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backupRoot = join(stateRoot, 'ui-backup');
    const externalBackup = join(externalParent, 'target');
    renameSync(backupRoot, externalBackup);
    symlinkSync(externalBackup, backupRoot);
    const protectedPaths = [
      backupRoot,
      externalBackup,
      join(root, 'apps'),
      join(root, '.ui-profile.json'),
      join(stateRoot, 'transaction.json'),
    ];
    const before = protectedPaths.map(filesystemSnapshot);

    const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);

    assert.equal(result.status, 3, output(result));
    assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
  } finally {
    rmSync(externalParent, { force: true, recursive: true });
    dispose(root);
  }
});

test('dependency resume rejects a symlinked ui-backup root before pnpm or receipt publication', () => {
  const root = fixture();
  const externalParent = mkdtempSync(join(tmpdir(), 'gin-vben-external-resume-backup-'));
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'ele',
      phase: 'dependencies_pending',
      moves: [
        { source: 'apps/web-antd', backup: 'apps/web-antd' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const profile = { schema: 1, selectedUi: 'ele', packageName: '@vben/web-ele', appDirectory: 'apps/web-ele' };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const externalBackup = join(externalParent, 'target');
    const appsBackup = join(externalBackup, transaction.id, 'apps');
    mkdirSync(appsBackup, { recursive: true });
    renameSync(join(root, 'apps', 'web-antd'), join(appsBackup, 'web-antd'));
    renameSync(join(root, 'apps', 'web-naive'), join(appsBackup, 'web-naive'));
    mkdirSync(stateRoot, { recursive: true });
    symlinkSync(externalBackup, join(stateRoot, 'ui-backup'));
    writeFileSync(join(stateRoot, 'transaction.json'), `${JSON.stringify(transaction, null, 2)}\n`);
    writeFileSync(join(root, '.ui-profile.json'), `${JSON.stringify(profile, null, 2)}\n`);
    const pnpmLog = join(root, 'symlinked-backup-resume-pnpm.log');
    const runner = join(root, 'scripts', 'symlinked-backup-resume-pnpm.mjs');
    writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');
    const protectedPaths = [
      join(stateRoot, 'ui-backup'),
      externalBackup,
      join(stateRoot, 'transaction.json'),
      join(root, '.ui-profile.json'),
      join(root, 'apps'),
    ];
    const before = protectedPaths.map(filesystemSnapshot);

    const result = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: pnpmLog,
    });

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=INITIALIZATION_RESUME_INVALID/);
    assert.equal(existsSync(pnpmLog), false);
    assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
  } finally {
    rmSync(externalParent, { force: true, recursive: true });
    dispose(root);
  }
});

test('reset treats dangling marker, lock, profile, and restore destinations as occupied', async (t) => {
  for (const name of ['marker', 'lock', 'profile', 'restore destination']) {
    await t.test(name, () => {
      const root = fixture();
      try {
        const prepared = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
        assert.equal(prepared.status, 0, output(prepared));
        const stateRoot = join(root, '..', '.runtime', 'install');
        const target = name === 'marker'
          ? join(stateRoot, '.installed')
          : name === 'lock'
            ? join(stateRoot, '.installed.lock')
            : name === 'profile'
              ? join(root, '.ui-profile.json')
              : join(root, 'apps', 'web-ele');
        if (name === 'profile') rmSync(target);
        symlinkSync(`missing-${name.replaceAll(' ', '-')}`, target);
        const protectedPaths = [
          target,
          join(root, 'apps'),
          join(root, '.ui-profile.json'),
          join(stateRoot, 'ui-backup'),
          join(stateRoot, 'transaction.json'),
        ];
        const before = protectedPaths.map(filesystemSnapshot);

        const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);

        assert.equal(result.status, 3, output(result));
        assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
      } finally {
        dispose(root);
      }
    });
  }
});

test('rerun completes a UI move transaction interrupted between templates', () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backup = join(stateRoot, 'ui-backup', transaction.id);
    mkdirSync(join(backup, 'apps'), { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backup, 'apps', 'web-ele'));
    const path = join(stateRoot, 'transaction.json');
    writeFileSync(path, JSON.stringify(transaction));
    const resumed = run(root, 'init.mjs', ['--no-open']);
    assert.equal(resumed.status, 0, output(resumed));
    assert.deepEqual(
      [...output(resumed).matchAll(/^INIT_STAGE=(.+)$/gm)].map((match) => match[1]),
      ['prepare:preflight', 'prepare:workspace', 'prepare:dependencies', 'prepare:complete'],
    );
    assert.match(output(resumed), /INIT_STATE=ui_prepared/);
    assert.match(output(resumed), /INIT_SELECTED_UI=antd/);
    assert.equal(existsSync(path), false);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
    assert.equal(existsSync(join(backup, 'apps', 'web-ele')), true);
    assert.equal(existsSync(join(backup, 'apps', 'web-naive')), true);
  } finally {
    dispose(root);
  }
});

test('moving UI resume rejects a symlinked admin apps parent before another source move or pnpm', () => {
  const root = fixture();
  const externalParent = mkdtempSync(join(tmpdir(), 'gin-vben-external-apps-resume-'));
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backup = join(stateRoot, 'ui-backup', transaction.id, 'apps');
    mkdirSync(backup, { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(backup, 'web-ele'));
    const transactionPath = join(stateRoot, 'transaction.json');
    writeFileSync(transactionPath, `${JSON.stringify(transaction, null, 2)}\n`);
    const externalApps = join(externalParent, 'apps');
    renameSync(join(root, 'apps'), externalApps);
    symlinkSync(externalApps, join(root, 'apps'));
    const pnpmLog = join(root, 'symlinked-apps-resume-pnpm.log');
    const runner = join(root, 'scripts', 'symlinked-apps-resume-pnpm.mjs');
    writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');
    const protectedPaths = [join(root, 'apps'), externalApps, join(stateRoot, 'ui-backup'), transactionPath];
    const before = protectedPaths.map(filesystemSnapshot);

    const result = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: pnpmLog,
    });

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=INITIALIZATION_RESUME_INVALID/);
    assert.equal(existsSync(pnpmLog), false);
    assert.deepEqual(protectedPaths.map(filesystemSnapshot), before);
  } finally {
    rmSync(externalParent, { force: true, recursive: true });
    dispose(root);
  }
});

test('resume preserves a conflicting profile byte-for-byte and never runs pnpm', () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    mkdirSync(stateRoot, { recursive: true });
    const transactionPath = join(stateRoot, 'transaction.json');
    const transactionBytes = `${JSON.stringify(transaction, null, 2)}\n`;
    writeFileSync(transactionPath, transactionBytes);
    const profilePath = join(root, '.ui-profile.json');
    const profileBytes = '{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}\n';
    writeFileSync(profilePath, profileBytes);
    const pnpmLog = join(root, 'resume-conflict-pnpm.log');
    const runner = join(root, 'scripts', 'resume-conflict-pnpm.mjs');
    writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

    const result = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: pnpmLog,
    });

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=INITIALIZATION_RESUME_INVALID/);
    assert.equal(readFileSync(profilePath, 'utf8'), profileBytes);
    assert.equal(readFileSync(transactionPath, 'utf8'), transactionBytes);
    assert.equal(existsSync(pnpmLog), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
    }
  } finally {
    dispose(root);
  }
});

test('resume rejects unexpected backup entries before moving another template or running pnpm', () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      phase: 'moving_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const stateRoot = join(root, '..', '.runtime', 'install');
    const transactionDirectory = join(stateRoot, 'ui-backup', transaction.id);
    mkdirSync(join(transactionDirectory, 'apps'), { recursive: true });
    renameSync(join(root, 'apps', 'web-ele'), join(transactionDirectory, 'apps', 'web-ele'));
    writeFileSync(join(transactionDirectory, 'unexpected.txt'), 'preserve me\n');
    const transactionPath = join(stateRoot, 'transaction.json');
    const transactionBytes = `${JSON.stringify(transaction, null, 2)}\n`;
    writeFileSync(transactionPath, transactionBytes);
    const pnpmLog = join(root, 'unexpected-backup-pnpm.log');
    const runner = join(root, 'scripts', 'unexpected-backup-pnpm.mjs');
    writeFileSync(runner, 'import { writeFileSync } from "node:fs"; writeFileSync(process.env.INIT_PNPM_LOG, "ran");\n');

    const result = run(root, 'init.mjs', ['--no-open'], {
      INIT_DEPENDENCY_INSTALL_TEST_MODE: '',
      INIT_PNPM_COMMAND: process.execPath,
      INIT_PNPM_PREFIX_ARGS: JSON.stringify([runner]),
      INIT_PNPM_LOG: pnpmLog,
    });

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_ERROR=INITIALIZATION_RESUME_INVALID/);
    assert.equal(readFileSync(transactionPath, 'utf8'), transactionBytes);
    assert.equal(readFileSync(join(transactionDirectory, 'unexpected.txt'), 'utf8'), 'preserve me\n');
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), true);
    assert.equal(existsSync(pnpmLog), false);
  } finally {
    dispose(root);
  }
});

test('init never overwrites a server-owned installation transaction', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'naive', '--confirm-cleanup', '--no-open']).status, 0);
    const transactionPath = join(root, '..', '.runtime', 'install', 'transaction.json');
    const transaction = {
      schema: 1,
      owner: 'server-installer',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'naive',
      mode: 'dev',
      phase: 'applying',
      currentStep: 'database',
    };
    writeFileSync(transactionPath, `${JSON.stringify(transaction)}\n`);
    const before = readFileSync(transactionPath, 'utf8');

    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_STATE=installing/);
    assert.match(output(result), /INIT_ERROR=INITIALIZATION_IN_PROGRESS/);
    assert.equal(readFileSync(transactionPath, 'utf8'), before);
  } finally {
    dispose(root);
  }
});

test('reset compare-and-claim preserves a server-owned installation transaction', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'naive', '--confirm-cleanup', '--no-open']).status, 0);
    const transactionPath = join(root, '..', '.runtime', 'install', 'transaction.json');
    const transaction = {
      schema: 1,
      owner: 'server-installer',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'naive',
      mode: 'dev',
      phase: 'applying',
      currentStep: 'database',
    };
    writeFileSync(transactionPath, `${JSON.stringify(transaction)}\n`);
    const before = readFileSync(transactionPath, 'utf8');
    const profileBefore = readFileSync(join(root, '.ui-profile.json'), 'utf8');

    const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_STATE=installing/);
    assert.match(output(result), /INIT_ERROR=INIT_BUSY/);
    assert.equal(readFileSync(transactionPath, 'utf8'), before);
    assert.equal(readFileSync(join(root, '.ui-profile.json'), 'utf8'), profileBefore);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-antd')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
  } finally {
    dispose(root);
  }
});

test('reset removes only its exact new claim when an installed marker commits before the first restore move', async () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    const transactionPath = join(stateRoot, 'transaction.json');
    const markerPath = join(stateRoot, '.installed');
    const profilePath = join(root, '.ui-profile.json');
    const profileBefore = readFileSync(profilePath, 'utf8');
    const backupRoot = join(stateRoot, 'ui-backup');
    const [transactionId] = readdirSync(backupRoot);
    const markerBytes = `${JSON.stringify({
      schema_version: 1,
      installer_version: '0.4.0-dev',
      installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'antd',
      mode: 'dev',
      artifact_hash: 'a'.repeat(64),
      manifest_hash: 'b'.repeat(64),
    })}\n`;

    await assert.rejects(
      resetInitialization(root, {
        afterClaim: async () => writeFileSync(markerPath, markerBytes),
      }),
      /RESET_UNAVAILABLE_INSTALLED/,
    );

    assert.equal(readFileSync(markerPath, 'utf8'), markerBytes);
    assert.equal(readFileSync(profilePath, 'utf8'), profileBefore);
    assert.equal(existsSync(transactionPath), false);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
    assert.equal(existsSync(join(backupRoot, transactionId, 'apps', 'web-ele')), true);
    assert.equal(existsSync(join(backupRoot, transactionId, 'apps', 'web-naive')), true);
  } finally {
    dispose(root);
  }
});

test('reset preserves a server transaction that wins the no-clobber claim race', async () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    const transactionPath = join(stateRoot, 'transaction.json');
    const profilePath = join(root, '.ui-profile.json');
    const profileBefore = readFileSync(profilePath, 'utf8');
    const serverTransactionBytes = `${JSON.stringify({
      schema: 1,
      owner: 'server-installer',
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      mode: 'dev',
      phase: 'applying',
      currentStep: 'marker',
    })}\n`;

    await assert.rejects(
      resetInitialization(root, {
        beforeClaim: async () => writeFileSync(transactionPath, serverTransactionBytes),
      }),
      /INIT_BUSY/,
    );

    assert.equal(readFileSync(transactionPath, 'utf8'), serverTransactionBytes);
    assert.equal(readFileSync(profilePath, 'utf8'), profileBefore);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), false);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
  } finally {
    dispose(root);
  }
});

test('reset resumes a durable transaction after one template was restored before a crash', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backupRoot = join(stateRoot, 'ui-backup');
    const [transactionId] = readdirSync(backupRoot);
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: transactionId,
      selectedUi: 'antd',
      phase: 'resetting_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    writeFileSync(join(stateRoot, 'transaction.json'), `${JSON.stringify(transaction, null, 2)}\n`);
    renameSync(
      join(backupRoot, transactionId, 'apps', 'web-ele'),
      join(root, 'apps', 'web-ele'),
    );

    const resumed = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);

    assert.equal(resumed.status, 0, output(resumed));
    assert.match(output(resumed), /INIT_STATE=pristine/);
    assert.match(output(resumed), /INIT_NEXT=RESET_COMPLETE/);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    assert.equal(existsSync(join(backupRoot, transactionId)), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
    }
  } finally {
    dispose(root);
  }
});

test('ordinary init reports the reset continuation command without reversing a reset transaction', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backupRoot = join(stateRoot, 'ui-backup');
    const [transactionId] = readdirSync(backupRoot);
    const transaction = {
      schema: 1,
      owner: 'admin-init',
      id: transactionId,
      selectedUi: 'antd',
      phase: 'resetting_ui',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const transactionPath = join(stateRoot, 'transaction.json');
    const transactionBytes = `${JSON.stringify(transaction, null, 2)}\n`;
    writeFileSync(transactionPath, transactionBytes);
    renameSync(join(backupRoot, transactionId, 'apps', 'web-ele'), join(root, 'apps', 'web-ele'));

    const result = run(root, 'init.mjs', ['--no-open']);

    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /pnpm run init -- --reset --confirm-reset/);
    assert.match(output(result), /INIT_REASON=RESET_TRANSACTION_PRESENT/);
    assert.match(output(result), /INIT_NEXT=RUN_RESET/);
    assert.match(output(result), /INIT_ERROR=RESET_IN_PROGRESS/);
    assert.equal(readFileSync(transactionPath, 'utf8'), transactionBytes);
    assert.equal(existsSync(join(root, 'apps', 'web-ele')), true);
    assert.equal(existsSync(join(root, 'apps', 'web-naive')), false);
  } finally {
    dispose(root);
  }
});

test('reset preflight resumes after backup cleanup completed before profile and transaction cleanup', () => {
  const root = fixture();
  try {
    assert.equal(run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']).status, 0);
    const stateRoot = join(root, '..', '.runtime', 'install');
    const backupRoot = join(stateRoot, 'ui-backup');
    const [transactionId] = readdirSync(backupRoot);
    const transactionDirectory = join(backupRoot, transactionId);
    const moves = [
      { source: 'apps/web-ele', backup: 'apps/web-ele' },
      { source: 'apps/web-naive', backup: 'apps/web-naive' },
    ];
    for (const move of moves) {
      renameSync(join(transactionDirectory, move.backup), join(root, move.source));
    }
    rmSync(transactionDirectory, { recursive: true });
    writeFileSync(join(stateRoot, 'transaction.json'), `${JSON.stringify({
      schema: 1,
      owner: 'admin-init',
      id: transactionId,
      selectedUi: 'antd',
      phase: 'resetting_ui',
      moves,
    }, null, 2)}\n`);

    const result = run(root, 'init.mjs', ['--reset', '--confirm-reset', '--no-open']);

    assert.equal(result.status, 0, output(result));
    assert.match(output(result), /INIT_STATE=pristine/);
    assert.match(output(result), /INIT_NEXT=RESET_COMPLETE/);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    assert.equal(existsSync(join(stateRoot, 'transaction.json')), false);
    assert.equal(existsSync(transactionDirectory), false);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
    }
  } finally {
    dispose(root);
  }
});

test('initialization journals fsync file contents and parent directories around atomic publication', () => {
  const source = readFileSync(join(sourceRoot, 'scripts', 'init-state.mjs'), 'utf8');
  assert.match(source, /await handle\.sync\(\)/);
  assert.match(source, /await syncDirectory\(dirname\(file\)\)/);
  assert.match(source, /await rename\(temporary, file\)/);
  assert.match(source, /await link\(temporary, file\)/);
});
