import assert from 'node:assert/strict';
import { cpSync, existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const sourceRoot = join(import.meta.dirname, '..');
const fixtureRepositories = new Map();

function fixture() {
  const repository = mkdtempSync(join(tmpdir(), 'gin-vben-init-'));
  const root = join(repository, 'admin');
  mkdirSync(join(root, 'scripts'), { recursive: true });
  for (const name of ['init.mjs', 'init-runtime.mjs', 'init-state.mjs', 'profile-gate.mjs', 'selected-dispatch.mjs']) {
    cpSync(join(sourceRoot, 'scripts', name), join(root, 'scripts', name));
  }
  for (const ui of ['antd', 'ele', 'naive']) {
    const directory = join(root, 'apps', `web-${ui}`);
    mkdirSync(directory, { recursive: true });
    writeFileSync(join(directory, 'package.json'), JSON.stringify({ name: `@vben/web-${ui}` }));
  }
  mkdirSync(join(root, 'apps', 'install'), { recursive: true });
  fixtureRepositories.set(root, repository);
  return root;
}

function dispose(root) {
  rmSync(fixtureRepositories.get(root) ?? root, { force: true, recursive: true });
  fixtureRepositories.delete(root);
}

function run(root, script, args = [], env = {}) {
  return spawnSync(process.execPath, [join(root, 'scripts', script), ...args], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, INIT_RUNTIME_TEST_MODE: 'simulate', ...env },
  });
}

function output(result) {
  return `${result.stdout}${result.stderr}`;
}

test('init requires an explicit UI when stdin is not a terminal', () => {
  const root = fixture();
  try {
    const result = run(root, 'init.mjs', ['--no-open']);
    assert.equal(result.status, 2, output(result));
    assert.match(output(result), /INIT_STATE=pristine/);
    assert.match(output(result), /INIT_ERROR=UI_REQUIRED/);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
  } finally {
    dispose(root);
  }
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
    assert.match(output(first), /INIT_PLAN_BACKUP=\.runtime\/init-backup\/<transaction>/);
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
    const backups = join(root, '..', '.runtime', 'init-backup');
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

test('init completes a read-only path preflight before confirmation or source moves', () => {
  const root = fixture();
  try {
    writeFileSync(join(root, '..', '.runtime'), 'not-a-directory');

    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_PREFLIGHT=failed/);
    assert.match(output(result), /INIT_ERROR=PREFLIGHT_FAILED/);
    assert.equal(existsSync(join(root, '.ui-init-transaction.json')), false);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    for (const ui of ['antd', 'ele', 'naive']) assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
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

    writeFileSync(join(root, 'apps', 'install', '.installed'), JSON.stringify({
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
    writeFileSync(join(root, '.ui-init-transaction.json'), '{"schema":1}\n');
    const active = run(root, 'profile-gate.mjs', ['--command', 'preview']);
    assert.equal(active.status, 3, output(active));
    assert.match(output(active), /INIT_STATE=inconsistent/);
    assert.match(output(active), /INIT_ERROR=PROFILE_INVALID/);
  } finally {
    dispose(root);
  }
});

test('an orphaned local receipt is inconsistent and init never overwrites it', () => {
  const root = fixture();
  try {
    const receiptPath = join(root, '.ui-init-receipt.json');
    const receipt = '{"schema":1,"transactionId":"12345678-1234-1234-1234-123456789abc","selectedUi":"antd","moves":[]}\n';
    writeFileSync(receiptPath, receipt);

    const result = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(result.status, 3, output(result));
    assert.match(output(result), /INIT_STATE=inconsistent/);
    assert.match(output(result), /INIT_ERROR=STATE_INCONSISTENT/);
    assert.equal(readFileSync(receiptPath, 'utf8'), receipt);
    assert.equal(existsSync(join(root, '.ui-profile.json')), false);
    for (const ui of ['antd', 'ele', 'naive']) assert.equal(existsSync(join(root, 'apps', `web-${ui}`)), true);
  } finally {
    dispose(root);
  }
});

test('runtime installer state, not a completed source-move transaction, represents an active installation', () => {
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
    const marker = join(root, 'apps', 'install', '.installed');
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

test('runtime launcher builds the installer, starts cmd/init, and owns the runtime receipt', () => {
  const source = readFileSync(join(sourceRoot, 'scripts', 'init-runtime.mjs'), 'utf8');
  assert.match(source, /build:installer/);
  assert.match(source, /cmd\/init/);
  assert.match(source, /--assets/);
  assert.match(source, /127\.0\.0\.1/);
  assert.match(source, /publishRuntime/);
  assert.match(source, /clearRuntime/);
  assert.match(source, /SIGINT/);
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
    assert.match(pkg.scripts['build:analyze'], /profile-gate/);
    assert.match(pkg.scripts['build:analyze'], /selected-dispatch/);
    for (const ui of ['antd', 'ele', 'naive']) {
      assert.equal(pkg.scripts[`build:${ui}`], `pnpm -F @vben/web-${ui} run build`);
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

    writeFileSync(join(root, 'apps', 'install', '.installed'), JSON.stringify({
      schema_version: 1, installer_version: '0.4.0-dev', installed_at: '2026-08-24T00:00:00Z',
      selected_ui: 'antd', mode: 'embedded', artifact_hash: 'a'.repeat(64), manifest_hash: 'b'.repeat(64),
    }));
    const installedClone = run(root, 'init.mjs', ['--check']);
    assert.equal(installedClone.status, 0, output(installedClone));
    assert.match(output(installedClone), /INIT_STATE=installed/);
    rmSync(join(root, 'apps', 'install', '.installed'));

    writeFileSync(join(root, '.ui-init-receipt.json'), '{"schema":0}\n');
    const corruptReceipt = run(root, 'profile-gate.mjs', ['--command', 'build']);
    assert.equal(corruptReceipt.status, 3, output(corruptReceipt));
    assert.match(output(corruptReceipt), /INIT_STATE=inconsistent/);

    rmSync(join(root, '.ui-init-receipt.json'));
    mkdirSync(join(root, 'apps', 'web-ele'));
    const extraTemplate = run(root, 'init.mjs', ['--check']);
    assert.equal(extraTemplate.status, 3, output(extraTemplate));
    assert.match(output(extraTemplate), /INIT_STATE=inconsistent/);
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

test('a valid exclusive transaction makes concurrent init stable and leaves the transaction untouched', () => {
  const root = fixture();
  try {
    const transaction = {
      schema: 1,
      id: '12345678-1234-1234-1234-123456789abc',
      selectedUi: 'antd',
      moves: [
        { source: 'apps/web-ele', backup: 'apps/web-ele' },
        { source: 'apps/web-naive', backup: 'apps/web-naive' },
      ],
    };
    const path = join(root, '.ui-init-transaction.json');
    writeFileSync(path, JSON.stringify(transaction));
    const second = run(root, 'init.mjs', ['--ui', 'antd', '--confirm-cleanup', '--no-open']);
    assert.equal(second.status, 3, output(second));
    assert.match(output(second), /INIT_STATE=installing/);
    assert.match(output(second), /INIT_ERROR=INITIALIZATION_IN_PROGRESS/);
    assert.deepEqual(JSON.parse(readFileSync(path, 'utf8')), transaction);
  } finally {
    dispose(root);
  }
});
