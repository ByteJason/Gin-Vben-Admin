import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');

function runNode(script, ...args) {
  return spawnSync(process.execPath, [join(root, script), ...args], {
    cwd: root,
    encoding: 'utf8',
  });
}

test('B1 skeleton exposes the separated admin/client/server boundaries', () => {
  for (const path of [
    'admin/apps/web-antd',
    'admin/apps/web-ele',
    'admin/apps/web-naive',
    'server/cmd/api',
    'server/internal/bootstrap',
    'server/Dockerfile',
    'admin/Dockerfile',
    'deploy/compose.dev.yaml',
    'deploy/compose.dependencies.yaml',
    'docs/README.md',
    'contracts/openapi/admin-v1.yaml',
    'contracts/openapi/client-v1.yaml',
  ]) {
    assert.equal(existsSync(join(root, path)), true, path);
  }
  for (const forbidden of ['apps', 'packages', 'internal', 'frontend', 'backend']) {
    assert.equal(existsSync(join(root, forbidden)), false, `root/${forbidden}`);
  }
});

test('OpenAPI scopes stay separate and expose the B1 seams', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const client = readFileSync(join(root, 'contracts/openapi/client-v1.yaml'), 'utf8');
  assert.match(admin, /\/health\/live/);
  assert.match(admin, /\/health\/ready/);
  assert.match(admin, /\/api\/admin\/v1\/auth\/login/);
  assert.match(admin, /X-Request-ID/);
  assert.doesNotMatch(client, /\/api\/admin\/v1/);
});

test('bootstrap check is cross-platform and verify reports a green skeleton', () => {
  const bootstrap = runNode('scripts/bootstrap.mjs', '--check');
  assert.equal(bootstrap.status, 0, bootstrap.stdout + bootstrap.stderr);
  const verify = runNode('scripts/verify.mjs', '--scope', 'skeleton');
  assert.equal(verify.status, 0, verify.stdout + verify.stderr);
  assert.match(verify.stdout, /VERIFY_OK/);
});

test('B1 container build prepares the upstream workspace stubs', () => {
  const dockerfile = readFileSync(join(root, 'admin/Dockerfile'), 'utf8');
  assert.match(dockerfile, /pnpm -r run --if-present stub/);
});

test('B1 install flow does not advertise an unimplemented migration command', () => {
  const readme = readFileSync(join(root, 'README.md'), 'utf8');
  assert.doesNotMatch(readme, /go -C server run \.\/cmd\/migrate up/);
  assert.doesNotMatch(readme, /go run \.\/cmd\/migrate up/);
});
