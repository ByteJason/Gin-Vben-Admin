import assert from 'node:assert/strict';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
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

test('repository exposes the required code boundaries', () => {
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
    'LICENSE',
    'LICENSES/Vue-Vben-Admin-MIT.txt',
    'NOTICE',
  ]) {
    assert.equal(existsSync(join(root, path)), true, path);
  }
  const allowed = new Set([
    '.dev-docs', '.git', '.github', '.idea', '.pnpm-store', '.runtime', 'LICENSES', 'admin', 'contracts', 'deploy', 'docs',
    'scripts', 'server', 'tests',
  ]);
  const unexpected = readdirSync(root, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && !allowed.has(entry.name))
    .map((entry) => entry.name);
  assert.deepEqual(unexpected, []);
});

test('OpenAPI scopes stay separate and expose the HTTP seams', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const client = readFileSync(join(root, 'contracts/openapi/client-v1.yaml'), 'utf8');
  assert.match(admin, /\/health\/live/);
  assert.match(admin, /\/health\/ready/);
  assert.match(admin, /\/api\/admin\/v1\/ping/);
  assert.match(admin, /X-Request-ID/);
  assert.doesNotMatch(client, /\/api\/admin\/v1/);
});

test('bootstrap check is cross-platform and verification succeeds', () => {
  const bootstrap = runNode('scripts/bootstrap.mjs', '--check');
  assert.equal(bootstrap.status, 0, bootstrap.stdout + bootstrap.stderr);
  const verify = runNode('scripts/verify.mjs', '--scope', 'basic');
  assert.equal(verify.status, 0, verify.stdout + verify.stderr);
  assert.match(verify.stdout, /VERIFY_OK/);
});

test('container build prepares the workspace stubs', () => {
  const dockerfile = readFileSync(join(root, 'admin/Dockerfile'), 'utf8');
  assert.match(dockerfile, /pnpm -r run --if-present stub/);
});

test('install flow advertises the explicit migration command', () => {
  const readme = readFileSync(join(root, 'README.md'), 'utf8');
  assert.match(readme, /go -C server run \.\/cmd\/migrate status/);
  assert.match(readme, /go -C server run \.\/cmd\/migrate up/);
});

test('CI covers the three host platforms and core gates', () => {
  const workflowPath = join(root, '.github/workflows/ci.yml');
  assert.equal(existsSync(workflowPath), true, workflowPath);
  const workflow = readFileSync(workflowPath, 'utf8');
  for (const platform of ['ubuntu-latest', 'macos-latest', 'windows-latest']) {
    assert.match(workflow, new RegExp(platform));
  }
  assert.match(workflow, /pnpm\/action-setup/);
  assert.match(workflow, /node --test tests\/contract\/contract\.test\.mjs/);
  assert.match(workflow, /pnpm --dir admin run test:smoke/);
  assert.match(workflow, /go -C server test \.\/\.\.\./);
});

test('dev orchestrator exposes a cross-platform check mode', () => {
  const source = readFileSync(join(root, 'scripts/dev.mjs'), 'utf8');
  assert.match(source, /shell:\s*false/);
  const check = runNode('scripts/dev.mjs', '--check', '--ui', 'antd');
  assert.equal(check.status, 0, check.stdout + check.stderr);
  assert.match(check.stdout, /DEV_CHECK_OK/);
  assert.match(check.stdout, /go -C server run \.\/cmd\/api/);
  assert.match(check.stdout, /pnpm --dir admin run dev:antd/);
});

test('v0.2 public surface documents config topologies and explicit migrations', () => {
  const readme = readFileSync(join(root, 'README.md'), 'utf8');
  const example = readFileSync(join(root, 'server/configs/server.example.yaml'), 'utf8');
  assert.match(readme, /0\.2\.0-dev/);
  assert.match(readme, /cmd\/migrate/);
  assert.match(readme, /migrate status/);
  for (const mode of ['single', 'read_write', 'cluster_endpoint']) assert.match(example, new RegExp(mode));
  for (const mode of ['single', 'sentinel', 'cluster']) assert.match(example, new RegExp(mode));
  assert.equal(existsSync(join(root, 'server/cmd/migrate')), true);
});

test('readiness error contract uses the dependency-unavailable code', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  const errors = readFileSync(join(root, 'contracts/errors/error-codes.yaml'), 'utf8');
  assert.match(admin, /40001/);
  assert.match(admin, /const: 40001/);
  assert.match(errors, /code: 40001/);
  assert.match(errors, /dependency_unavailable/);
});

test('verification awaits asynchronous repository checks', () => {
  const source = readFileSync(join(root, 'scripts/verify.mjs'), 'utf8');
  assert.match(source, /Promise\.all/);
  assert.doesNotMatch(source, /required\.filter\(\(item\) => !exists\(item\)\)/);
});

test('single-node integration CI runs the explicit gated suite', () => {
  const workflow = readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8');
  for (const variable of [
    'DATA_PLATFORM_INTEGRATION',
    'TEST_MYSQL_DSN',
    'TEST_POSTGRES_DSN',
    'TEST_REDIS_ADDR',
  ]) {
    assert.match(workflow, new RegExp(variable));
  }
  assert.match(workflow, /go -C server test \.\/tests\/integration/);
});

test('development compose wires the API to the default single-node dependencies', () => {
  const compose = readFileSync(join(root, 'deploy/compose.dev.yaml'), 'utf8');
  const server = compose.slice(compose.indexOf('  server:'), compose.indexOf('\n  admin:'));
  for (const variable of ['DATABASE_ENABLED', 'DATABASE_DRIVER', 'DATABASE_MODE', 'DATABASE_DSN', 'REDIS_ENABLED', 'REDIS_MODE', 'REDIS_ADDR']) {
    assert.match(server, new RegExp(variable));
  }
  assert.doesNotMatch(server, /postgres:\s*\n\s+condition:/);
  assert.match(compose, /postgres:\s*\n\s+profiles: \["postgres"\]/);
});

test('readiness contract describes dependency check states', () => {
  const admin = readFileSync(join(root, 'contracts/openapi/admin-v1.yaml'), 'utf8');
  assert.match(admin, /checks:/);
  assert.match(admin, /enum: \[up, down\]/);
  assert.match(admin, /enum: \[ok, ready, unavailable\]/);
});
