import assert from 'node:assert/strict';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');

test('single-node deployment fixture has a self-contained compose entrypoint', () => {
  const composePath = join(root, 'deploy/docker-compose.yml');
  assert.equal(existsSync(composePath), true, composePath);
  const compose = readFileSync(composePath, 'utf8');
  for (const service of ['server:', 'admin:', 'mysql:', 'redis:']) assert.match(compose, new RegExp(`\\n  ${service}`));
  assert.match(compose, /DATABASE_MODE:\s*single/);
  assert.match(compose, /REDIS_MODE:\s*single/);
  assert.match(compose, /condition: service_healthy/);
  assert.match(compose, /deploy\/server\.Dockerfile/);
  assert.match(compose, /deploy\/admin\.Dockerfile/);
  assert.doesNotMatch(compose, /postgres|sentinel|cluster|prometheus|grafana|mailpit/i);
});

test('deployment Dockerfiles use Alpine runtime images and do not embed credentials', () => {
  for (const name of ['server.Dockerfile', 'admin.Dockerfile']) {
    const file = readFileSync(join(root, 'deploy', name), 'utf8');
    assert.match(file, /FROM .*alpine/i, name);
    assert.doesNotMatch(file, /password|secret|token/i, name);
  }
});

test('deploy directory contains only the single-node entrypoints', () => {
  const allowed = new Set(['admin.Dockerfile', 'docker-compose.yml', 'server.Dockerfile']);
  const entries = readdirSync(join(root, 'deploy'), { withFileTypes: true });
  assert.deepEqual(entries.filter((entry) => entry.isFile()).map((entry) => entry.name).sort(), [...allowed].sort());
  assert.equal(entries.some((entry) => entry.isDirectory()), false, 'HA/observability assets belong under .runtime/compose');
});
