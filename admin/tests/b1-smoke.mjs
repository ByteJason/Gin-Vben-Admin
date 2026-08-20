import assert from 'node:assert/strict';
import { test } from 'node:test';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const requiredApps = ['web-antd', 'web-ele', 'web-naive'];
const removedApps = ['web-antdv-next', 'web-tdesign', 'backend-mock'];

test('B1 keeps the three selected UI templates', () => {
  for (const app of requiredApps) assert.ok(existsSync(resolve(root, 'apps', app)), app);
});

test('B1 removes unsupported UI and mock runtimes', () => {
  for (const app of removedApps) assert.equal(existsSync(resolve(root, 'apps', app)), false, app);
});

test('B1 workspace is rooted at admin and has no Nitro mock chain', () => {
  const workspace = readFileSync(resolve(root, 'pnpm-workspace.yaml'), 'utf8');
  const pkg = readFileSync(resolve(root, 'package.json'), 'utf8');
  assert.match(workspace, /apps\/\*/);
  assert.doesNotMatch(pkg, /backend-mock|nitropack|h3\b/);
  assert.doesNotMatch(workspace, /backend-mock/);
});

test('B1 contains frontend build closure', () => {
  for (const path of ['packages', 'internal', 'scripts', 'pnpm-lock.yaml']) {
    assert.ok(existsSync(resolve(root, path)), path);
  }
});
