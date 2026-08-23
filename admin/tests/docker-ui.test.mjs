import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import { resolveDockerSelection } from '../scripts/docker-build-ui.mjs';

function fixture(profile) {
  const root = mkdtempSync(join(tmpdir(), 'gin-vben-docker-ui-'));
  for (const ui of ['antd', 'ele', 'naive']) {
    const app = join(root, 'apps', `web-${ui}`);
    mkdirSync(app, { recursive: true });
    writeFileSync(join(app, 'package.json'), JSON.stringify({ name: `@vben/web-${ui}` }));
  }
  if (profile) writeFileSync(join(root, '.ui-profile.json'), `${JSON.stringify(profile)}\n`);
  return root;
}

test('Docker UI resolves a valid tracked profile without requiring the local marker', () => {
  const root = fixture({ schema: 1, selectedUi: 'naive', packageName: '@vben/web-naive', appDirectory: 'apps/web-naive' });
  try {
    assert.deepEqual(resolveDockerSelection(root, ''), {
      selectedUi: 'naive',
      packageName: '@vben/web-naive',
      appDirectory: 'apps/web-naive',
    });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('Docker UI rejects an explicit value that conflicts with the tracked profile', () => {
  const root = fixture({ schema: 1, selectedUi: 'ele', packageName: '@vben/web-ele', appDirectory: 'apps/web-ele' });
  try {
    assert.throws(() => resolveDockerSelection(root, 'antd'), /UI_PROFILE_MISMATCH/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('Docker UI accepts an explicit template in a pristine CI checkout', () => {
  const root = fixture();
  try {
    assert.equal(resolveDockerSelection(root, 'antd').packageName, '@vben/web-antd');
    assert.throws(() => resolveDockerSelection(root, ''), /UI_PROFILE_REQUIRED/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('Docker build caches Corepack and pnpm downloads across bounded retries', () => {
  const dockerfile = readFileSync(join(import.meta.dirname, '..', '..', 'deploy', 'admin.Dockerfile'), 'utf8');
  assert.match(dockerfile, /--mount=type=cache[^\n]*target=\/root\/\.cache\/node\/corepack/);
  assert.match(dockerfile, /--mount=type=cache[^\n]*target=\/pnpm\/store/);
  assert.match(dockerfile, /pnpm config set store-dir \/pnpm\/store/);
  assert.match(dockerfile, /ARG NPM_REGISTRY=https:\/\/registry\.npmjs\.org/);
  assert.match(dockerfile, /pnpm config set registry "\$\{NPM_REGISTRY\}" --location=project/);
  assert.match(dockerfile, /pnpm config set fetch-timeout 600000 --location=project/);
  assert.match(dockerfile, /pnpm -r run --if-present stub/);
});
