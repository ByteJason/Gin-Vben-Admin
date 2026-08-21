import assert from 'node:assert/strict';
import { existsSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const artifact = join(root, '.runtime', 'build', 'standalone');

test(
  'standalone build packages one management UI, installer, API, and manifest',
  { skip: process.env.BUILD_INTEGRATION !== '1' },
  () => {
    rmSync(artifact, { force: true, recursive: true });
    const result = spawnSync(
      process.execPath,
      ['scripts/build.mjs', '--mode', 'standalone', '--ui', 'antd'],
      { cwd: root, encoding: 'utf8' },
    );

    try {
      const binaryName = process.platform === 'win32' ? 'server-api.exe' : 'server-api';
      assert.equal(result.status, 0, result.stdout + result.stderr);
      assert.match(result.stdout, /BUILD_OK/);
      assert.equal(existsSync(join(artifact, binaryName)), true);
      assert.equal(existsSync(join(artifact, 'html', 'index.html')), true);
      assert.equal(existsSync(join(artifact, 'html', 'install', 'index.html')), true);

      const manifest = JSON.parse(
        readFileSync(join(artifact, 'artifact-manifest.json'), 'utf8'),
      );
      assert.equal(manifest.schema, 1);
      assert.equal(manifest.mode, 'standalone');
      assert.equal(manifest.ui, 'antd');
      assert.match(manifest.files[binaryName], /^[a-f0-9]{64}$/);
      assert.match(manifest.files['html/index.html'], /^[a-f0-9]{64}$/);
      assert.match(manifest.files['html/install/index.html'], /^[a-f0-9]{64}$/);
      assert.equal(existsSync(join(root, 'admin', 'apps', 'web-ele')), true);
      assert.equal(existsSync(join(root, 'admin', 'apps', 'web-naive')), true);
    } finally {
      rmSync(artifact, { force: true, recursive: true });
    }
  },
);
