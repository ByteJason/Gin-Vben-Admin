import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const runner = join(root, 'scripts', 'security-gates.mjs');

function run(...args) {
  return spawnSync(process.execPath, [runner, ...args], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env },
  });
}

test('security gate emits a deterministic local policy report with explicit tool exceptions', () => {
  assert.equal(existsSync(runner), true);
  const first = run('--output', '.runtime/security/security-report.json');
  assert.equal(first.status, 0, first.stdout + first.stderr);
  const report = JSON.parse(first.stdout);
  assert.equal(report.schema, 1);
  assert.equal(report.scope, 'local-only');
  assert.equal(report.policy.highCritical, 0);
  assert.equal(report.policy.productionClaim, false);
  assert.ok(Array.isArray(report.checks));
  assert.ok(report.checks.some((item) => item.id === 'secret-scan' && item.status === 'passed'));
  assert.ok(report.toolExceptions.every((item) => item.owner && item.reason && item.expires));
  const second = run('--output', '.runtime/security/security-report.json', '--check');
  assert.equal(second.status, 0, second.stdout + second.stderr);
  assert.deepEqual(JSON.parse(second.stdout), report);
});

test('security gate refuses non-loopback DAST targets and never runs destructive commands', () => {
  const result = run('--integration', '--target', 'https://example.invalid');
  assert.notEqual(result.status, 0);
  assert.match(result.stdout + result.stderr, /loopback|remote|refus/i);
  const source = readFileSync(runner, 'utf8');
  assert.equal(source.includes('DROP DATABASE'), false);
  assert.equal(source.includes('FLUSHDB'), false);
});

test('security gate fails an unregistered high severity fixture', () => {
  const result = run('--fixture', 'HIGH:unregistered');
  assert.notEqual(result.status, 0);
  assert.match(result.stdout + result.stderr, /High|Critical|unregistered/i);
});
