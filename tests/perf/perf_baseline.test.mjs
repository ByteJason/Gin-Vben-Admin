import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const runner = join(root, 'scripts', 'perf-baseline.mjs');

function run(args = [], env = {}) {
  return spawnSync(process.execPath, [runner, ...args], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, ...env },
  });
}

test('offline baseline emits the fixed DEC-025 contract without a production claim', () => {
  const workspace = mkdtempSync(join(tmpdir(), 'gin-vben-admin-perf-'));
  try {
    const output = join(workspace, 'baseline.json');
    const result = run(['--output', output]);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    const report = JSON.parse(readFileSync(output, 'utf8'));
    assert.equal(report.schema, 1);
    assert.equal(report.version, '0.9.0-rc');
    assert.equal(report.scope, 'local-only');
    assert.deepEqual(report.environment, { cpu: 4, memory_gib: 8, isolation: 'dedicated' });
    assert.deepEqual(report.fixtures, { users: 100000, roles: 1000, audit_events: 1000000 });
    assert.deepEqual(report.workloads, {
      read_api: { concurrency: 100 },
      login: { concurrency: 20 },
    });
    assert.deepEqual(report.duration_minutes, { warmup: 10, steady: 30 });
    assert.equal(report.acceptance.status, 'not_evaluated');
    assert.equal(report.acceptance.production_capacity_claim, false);
    assert.match(result.stdout, /PERF_BASELINE_MODE=offline-contract/);
    assert.match(result.stdout, /PERF_BASELINE_STATUS=NOT_EVALUATED/);
  } finally {
    rmSync(workspace, { recursive: true, force: true });
  }
});

test('offline baseline is reproducible and --check detects drift', () => {
  const workspace = mkdtempSync(join(tmpdir(), 'gin-vben-admin-perf-check-'));
  try {
    const output = join(workspace, 'baseline.json');
    let result = run(['--output', output]);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    result = run(['--output', output, '--check']);
    assert.equal(result.status, 0, result.stdout + result.stderr);
    assert.match(result.stdout, /PERF_BASELINE_CHECK_OK/);
    const report = JSON.parse(readFileSync(output, 'utf8'));
    report.duration_minutes.steady = 31;
    writeFileSync(output, `${JSON.stringify(report, null, 2)}\n`);
    result = run(['--output', output, '--check']);
    assert.notEqual(result.status, 0);
    assert.match(`${result.stdout}${result.stderr}`, /PERF_BASELINE_CHECK_FAILED/);
  } finally {
    rmSync(workspace, { recursive: true, force: true });
  }
});

test('integration mode rejects non-loopback targets before any request', () => {
  const result = run(['--integration', '--base-url', 'http://example.com'], { PERF_INTEGRATION: '1' });
  assert.notEqual(result.status, 0);
  assert.match(`${result.stdout}${result.stderr}`, /loopback/);
  assert.doesNotMatch(`${result.stdout}${result.stderr}`, /PERF_REQUEST_SENT/);
});

test('integration mode is explicit and never upgrades an unavailable loopback into a pass', () => {
  const result = run(
    ['--integration', '--base-url', 'http://127.0.0.1:1'],
    { PERF_INTEGRATION: '1' },
  );
  assert.notEqual(result.status, 0);
  assert.match(`${result.stdout}${result.stderr}`, /PERF_INTEGRATION_STATUS=ERROR/);
  assert.match(`${result.stdout}${result.stderr}`, /PERF_PRODUCTION_CAPACITY_CLAIM=false/);
  assert.doesNotMatch(`${result.stdout}${result.stderr}`, /PERF_ACCEPTANCE=PASSED/);
});
