import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const runner = join(root, 'scripts', 'release', 'package.mjs');

function run(...args) {
  return spawnSync(process.execPath, [runner, ...args], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env },
  });
}

test('release package check declares the accepted RC artifact matrix and provenance boundary', () => {
  assert.equal(existsSync(runner), true);
  const result = run('--check');
  assert.equal(result.status, 0, result.stdout + result.stderr);
  const report = JSON.parse(result.stdout);
  assert.equal(report.schema, 1);
  assert.equal(report.version, '0.9.0-rc');
  assert.equal(report.registryPublish, false);
  assert.equal(report.signed, false);
  assert.ok(report.targets.some((item) => item.os === 'linux' && item.arch === 'amd64'));
  assert.ok(report.targets.some((item) => item.os === 'linux' && item.arch === 'arm64'));
  assert.ok(report.targets.some((item) => item.os === 'darwin' && item.arch === 'arm64'));
  assert.ok(report.targets.some((item) => item.os === 'windows' && item.arch === 'amd64'));
  assert.ok(report.checksums && report.provenance);
});

test('release package rejects remote registry and signing actions', () => {
  const result = run('--publish', 'https://registry.example.invalid', '--sign');
  assert.notEqual(result.status, 0);
  assert.match(result.stderr + result.stdout, /remote|registry|sign/i);
  const source = readFileSync(runner, 'utf8');
  assert.equal(source.includes('docker push'), false);
  assert.equal(source.includes('cosign sign'), false);
});
