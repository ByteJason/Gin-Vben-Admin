import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const runner = join(root, 'scripts', 'migration-smoke.mjs');

function run(env = {}, ...args) {
  return spawnSync(process.execPath, [runner, ...args], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, MIGRATION_SMOKE_INTEGRATION: '', ...env },
  });
}

test('migration smoke is offline by default and records the 0.6 to 0.9 contract', () => {
  const result = run({}, '--check');
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /MIGRATION_SMOKE_SCOPE=0\.6\.0-dev=>0\.9\.0-rc/);
  assert.match(result.stdout, /"step":"backup-preflight".*"status":"skipped"/);
  assert.match(result.stdout, /"step":"migration-status".*"status":"skipped"/);
  assert.match(result.stdout, /"step":"cleanup".*"status":"passed"/);
  assert.match(result.stdout, /MIGRATION_SMOKE_STATUS=OK/);
  assert.equal(result.stdout.includes('DROP DATABASE'), false);
  assert.equal(result.stdout.includes('FLUSHDB'), false);
});

test('migration smoke rejects integration DSNs outside the isolated loopback contract', () => {
  const result = run({
    MIGRATION_SMOKE_INTEGRATION: '1',
    MIGRATION_SMOKE_MYSQL_DSN: 'root:secret@tcp(prod.example:3306)/prod',
    MIGRATION_SMOKE_POSTGRES_DSN: 'postgres://postgres:secret@prod.example:5432/prod',
  }, '--integration');
  assert.notEqual(result.status, 0);
  assert.match(result.stdout + result.stderr, /loopback|isolated|refused/i);
});

test('migration smoke source encodes safe command and rollback boundaries', () => {
  const source = readFileSync(runner, 'utf8');
  assert.match(source, /0\.6\.0-dev/);
  assert.match(source, /0\.9\.0-rc/);
  assert.match(source, /server[\\/]cmd[\\/]migrate/);
  assert.match(source, /backup-preflight/);
  assert.match(source, /health\/live/);
  assert.match(source, /rollback|restore/i);
  assert.equal(source.includes('DROP DATABASE'), false);
  assert.equal(source.includes('FLUSHDB'), false);
});
