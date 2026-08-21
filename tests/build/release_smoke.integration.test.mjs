import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const runner = join(root, 'scripts', 'release-smoke.mjs');

function run(...args) {
  return spawnSync(process.execPath, [runner, ...args], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, BUILD_INTEGRATION: '' },
  });
}

test('release smoke check validates all release modes without Docker or writes', () => {
  const result = run('--check');
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /RELEASE_SMOKE_MODE=check/);
  for (const mode of ['api_only', 'embedded', 'standalone']) {
    assert.match(result.stdout, new RegExp(`MODE_CHECK_OK=${mode}`));
  }
  assert.match(result.stdout, /HTTP_SMOKE=skipped/);
});

test('release smoke check supports serial UI selection and manifest contract', () => {
  const result = run('--check', '--ui', 'antd,ele,naive');
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stdout, /UI_CHECK=antd/);
  assert.match(result.stdout, /UI_CHECK=ele/);
  assert.match(result.stdout, /UI_CHECK=naive/);
  assert.match(result.stdout, /MANIFEST_CONTRACT_OK/);
});

test('release smoke integration remains opt-in', () => {
  const result = run('--check');
  assert.equal(result.status, 0);
  assert.equal(result.stdout.includes('DOCKER_COMPOSE_UP'), false);
});
