import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');

function generate() {
  const result = spawnSync(process.execPath, ['scripts/prepare-runtime-compose.mjs'], {
    cwd: root,
    encoding: 'utf8',
  });
  assert.equal(result.status, 0, result.stdout + result.stderr);
}

test('runtime fixtures cover read-write MySQL/PostgreSQL and Redis HA modes', () => {
  generate();
  const readWrite = readFileSync(join(root, '.runtime/compose/read-write.yaml'), 'utf8');
  for (const service of ['mysql-primary', 'mysql-replica', 'postgres-primary', 'postgres-replica']) {
    assert.match(readWrite, new RegExp(`\\n  ${service}:`), service);
  }
  const ha = readFileSync(join(root, '.runtime/compose/ha.yaml'), 'utf8');
  for (const service of [
    'redis-sentinel-a',
    'redis-sentinel-b',
    'redis-sentinel-c',
    'redis-cluster-a',
    'redis-cluster-b',
    'redis-cluster-c',
    'redis-cluster-d',
    'redis-cluster-e',
    'redis-cluster-f',
  ]) {
    assert.match(ha, new RegExp(`\\n  ${service}:`), service);
  }
  assert.match(ha, /cluster-enabled/);
  assert.match(ha, /--sentinel/);
});
