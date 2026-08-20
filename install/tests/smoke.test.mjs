import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const root = join(import.meta.dirname, '..');

test('installation shell is independent and exposes an accessible status region', () => {
  const html = readFileSync(join(root, 'src/index.html'), 'utf8');
  const script = readFileSync(join(root, 'src/app.js'), 'utf8');
  const styles = readFileSync(join(root, 'src/styles.css'), 'utf8');

  assert.match(html, /lang="zh-CN"/);
  assert.match(html, /<main\b/);
  assert.match(html, /href="#install-main"/);
  assert.match(html, /aria-live="polite"/);
  assert.match(html, /id="capability-list"/);
  assert.match(html, /aria-current="step"/);
  assert.match(script, /\/api\/system\/install\/v1\/status/);
  assert.match(script, /\/api\/system\/install\/v1\/capabilities/);
  assert.match(script, /credentials:\s*'same-origin'/);
  assert.match(script, /textContent/);
  assert.doesNotMatch(script, /innerHTML/);
  assert.doesNotMatch(script, /localStorage|sessionStorage/);
  assert.doesNotMatch(`${html}\n${script}`, /admin\/apps|web-antd|web-ele|web-naive/);
  assert.match(styles, /:focus-visible/);
  assert.match(styles, /prefers-reduced-motion:\s*reduce/);
  assert.match(styles, /min-height:\s*44px/);
  assert.match(styles, /@media\s*\(max-width:\s*480px\)/);
});

test('installation workspace builds a self-contained static dist', () => {
  rmSync(join(root, 'dist'), { force: true, recursive: true });
  const result = spawnSync(process.execPath, [join(root, 'scripts/build.mjs')], {
    cwd: root,
    encoding: 'utf8',
  });
  assert.equal(result.status, 0, result.stdout + result.stderr);
  for (const path of ['index.html', 'app.js', 'styles.css', 'asset-manifest.json']) {
    assert.equal(existsSync(join(root, 'dist', path)), true, path);
  }
  const manifest = JSON.parse(readFileSync(join(root, 'dist/asset-manifest.json'), 'utf8'));
  assert.deepEqual(Object.keys(manifest.files).sort(), ['app.js', 'index.html', 'styles.css']);
  for (const digest of Object.values(manifest.files)) {
    assert.match(digest, /^[a-f0-9]{64}$/);
  }
});
