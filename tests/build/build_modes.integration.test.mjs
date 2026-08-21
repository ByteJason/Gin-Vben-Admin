import assert from 'node:assert/strict';
import { existsSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import test from 'node:test';

const root = join(import.meta.dirname, '..', '..');
const standaloneArtifact = join(root, '.runtime', 'build', 'standalone');

test(
  'standalone build packages one management UI, installer, API, and manifest',
  { skip: process.env.BUILD_INTEGRATION !== '1' },
  () => {
    rmSync(standaloneArtifact, { force: true, recursive: true });
    const result = spawnSync(
      process.execPath,
      ['scripts/build.mjs', '--mode', 'standalone', '--ui', 'antd'],
      { cwd: root, encoding: 'utf8' },
    );

    try {
      const binaryName = process.platform === 'win32' ? 'server-api.exe' : 'server-api';
      assert.equal(result.status, 0, result.stdout + result.stderr);
      assert.match(result.stdout, /BUILD_OK/);
      assert.equal(existsSync(join(standaloneArtifact, binaryName)), true);
      assert.equal(existsSync(join(standaloneArtifact, 'html', 'index.html')), true);
      assert.equal(existsSync(join(standaloneArtifact, 'html', 'install', 'index.html')), true);

      const manifest = JSON.parse(
        readFileSync(join(standaloneArtifact, 'artifact-manifest.json'), 'utf8'),
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
      rmSync(standaloneArtifact, { force: true, recursive: true });
    }
  },
);

test(
  'embedded build packages installer and all management UIs into one Go artifact',
  { skip: process.env.BUILD_INTEGRATION !== '1' },
  () => {
    const artifact = join(root, '.runtime', 'build', 'embedded');
    const webassets = join(root, 'server', 'internal', 'platform', 'webassets', 'dist');
    const binaryName = process.platform === 'win32' ? 'server-api.exe' : 'server-api';
    rmSync(artifact, { force: true, recursive: true });

    const result = spawnSync(
      process.execPath,
      ['scripts/build.mjs', '--mode', 'embedded', '--ui', 'all'],
      { cwd: root, encoding: 'utf8' },
    );

    try {
      assert.equal(result.status, 0, result.stdout + result.stderr);
      assert.match(result.stdout, /BUILD_OK/);
      assert.equal(existsSync(join(artifact, binaryName)), true);
      assert.equal(existsSync(join(webassets, 'install', 'index.html')), true);
      for (const ui of ['antd', 'ele', 'naive']) {
        assert.equal(existsSync(join(webassets, 'admin', ui, 'index.html')), true);
      }

      const assetManifest = JSON.parse(
        readFileSync(join(webassets, 'asset-manifest.json'), 'utf8'),
      );
      assert.equal(assetManifest.schema, 1);
      assert.equal(assetManifest.mode, 'embedded');
      assert.equal(assetManifest.ui, 'all');
      assert.match(assetManifest.files['install/index.html'], /^[a-f0-9]{64}$/);
      assert.match(assetManifest.files['admin/antd/index.html'], /^[a-f0-9]{64}$/);

      const artifactManifest = JSON.parse(
        readFileSync(join(artifact, 'artifact-manifest.json'), 'utf8'),
      );
      assert.equal(artifactManifest.mode, 'embedded');
      assert.match(artifactManifest.files[binaryName], /^[a-f0-9]{64}$/);
    } finally {
      rmSync(artifact, { force: true, recursive: true });
    }
  },
);
