import assert from 'node:assert/strict';
import { test } from 'node:test';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const requiredApps = ['web-antd', 'web-ele', 'web-naive'];

test('B1 workspace exposes exactly the supported UI templates', () => {
  const apps = readdirSync(resolve(root, 'apps'), { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort();
  assert.deepEqual(apps, [...requiredApps].sort());
});

test('B1 workspace is rooted at admin and has no runtime simulators', () => {
  const workspace = readFileSync(resolve(root, 'pnpm-workspace.yaml'), 'utf8');
  const pkg = readFileSync(resolve(root, 'package.json'), 'utf8');
  assert.match(workspace, /apps\/\*/);
  assert.doesNotMatch(pkg, /mock|nitro|h3\b/i);
});

test('B1 contains frontend build closure', () => {
  for (const path of ['packages', 'internal', 'scripts', 'pnpm-lock.yaml']) {
    assert.ok(existsSync(resolve(root, path)), path);
  }
});
