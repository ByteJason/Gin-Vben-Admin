import assert from 'node:assert/strict';
import { existsSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const script = join(root, 'scripts', 'build.mjs');

function run(...args) {
  return spawnSync(process.execPath, [script, ...args], {
    cwd: root,
    encoding: 'utf8',
  });
}

test('build orchestrator validates all delivery modes without side effects', () => {
  for (const mode of ['embedded', 'standalone', 'api_only', 'dev']) {
    const result = run('--mode', mode, '--ui', 'antd', '--check');
    assert.equal(result.status, 0, `${mode}: ${result.stdout}${result.stderr}`);
    assert.match(result.stdout, new RegExp(`BUILD_MODE=${mode}`));
    assert.match(result.stdout, /BUILD_CHECK_OK/);
  }
  assert.equal(existsSync(join(root, 'server', 'internal', 'platform', 'webassets', 'dist')), false);
});

test('build orchestrator rejects unknown modes and UI names', () => {
  const mode = run('--mode', 'runtime-compile', '--ui', 'antd', '--check');
  assert.equal(mode.status, 2, mode.stdout + mode.stderr);
  const ui = run('--mode', 'embedded', '--ui', 'tdesign', '--check');
  assert.equal(ui.status, 2, ui.stdout + ui.stderr);
});

test('api-only validation does not require a management UI', () => {
  const result = run('--mode', 'api_only', '--check');
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /BUILD_UI=none/);
});

test('api-only build produces a bounded Go artifact', () => {
  const suffix = process.platform === 'win32' ? '.exe' : '';
  const relativeOutput = `server/bin/server-api-contract-${process.pid}${suffix}`;
  const output = join(root, relativeOutput);
  rmSync(output, { force: true });

  try {
    const result = run('--mode', 'api_only', '--output', relativeOutput);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.match(result.stdout, /BUILD_OK/);
    assert.equal(existsSync(output), true);
  } finally {
    rmSync(output, { force: true });
  }
});

test('build output cannot escape the generated binary directory', () => {
  const result = run('--mode', 'api_only', '--output', 'README.md');
  assert.equal(result.status, 2, result.stdout + result.stderr);
  assert.match(result.stderr, /output must be a file below server\/bin/);
});
