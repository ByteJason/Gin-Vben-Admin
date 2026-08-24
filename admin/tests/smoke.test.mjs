import assert from 'node:assert/strict';
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { test } from 'node:test';

const root = resolve(import.meta.dirname, '..');
const profilePath = resolve(root, '.ui-profile.json');
const requiredApps = existsSync(profilePath)
  ? [`web-${JSON.parse(readFileSync(profilePath, 'utf8')).selectedUi}`]
  : ['web-antd', 'web-ele', 'web-naive'];

test('workspace exposes the supported UI templates', () => {
  const apps = readdirSync(resolve(root, 'apps'), { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .sort();
  assert.deepEqual(apps, ['install', ...requiredApps].sort());
});

test('workspace has the expected package layout', () => {
  const workspace = readFileSync(resolve(root, 'pnpm-workspace.yaml'), 'utf8');
  const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
  assert.match(workspace, /apps\/\*/);
  for (const command of [
    'build:analyze',
    'build:antd',
    'build:ele',
    'build:naive',
    'dev:antd',
    'dev:ele',
    'dev:naive',
  ]) {
    assert.equal(Object.hasOwn(pkg.scripts, command), false, command);
  }
  for (const command of ['build', 'dev', 'preview']) {
    assert.match(pkg.scripts[command], /profile-gate\.mjs/);
    assert.match(pkg.scripts[command], /selected-dispatch\.mjs/);
  }
  assert.doesNotMatch(pkg.scripts['test:e2e:a11y'], /pnpm run build/);
  for (const packageName of ['@vben/web-antd', '@vben/web-ele', '@vben/web-naive']) {
    assert.match(
      pkg.scripts['test:e2e:a11y'],
      new RegExp(`pnpm --filter ${packageName.replace('/', '\\/')} run build`),
    );
  }
  assert.match(pkg.scripts['test:e2e:a11y'], /build:installer/);
  assert.match(pkg.scripts['test:e2e:a11y'], /playwright test/);
  assert.equal(Object.hasOwn(pkg.scripts, 'preinstall'), false);
  for (const command of ['preinstall', 'install', 'postinstall']) {
    assert.doesNotMatch(pkg.scripts[command] ?? '', /\bnpx\b|only-allow/);
  }
});

test('workspace contains its frontend build closure', () => {
  for (const path of ['packages', 'internal', 'scripts', 'pnpm-lock.yaml']) {
    assert.ok(existsSync(resolve(root, path)), path);
  }
});

test('every management template provides complete runtime environment examples', () => {
  for (const app of requiredApps) {
    for (const mode of ['development', 'production']) {
      const template = readFileSync(
        resolve(root, 'apps', app, `.env.${mode}.example`),
        'utf8',
      );
      assert.match(template, /^VITE_APP_TITLE=Gin Vben Admin$/m, `${app} ${mode} title`);
      assert.match(template, /^VITE_GLOB_API_URL=\/api$/m, `${app} ${mode} API base`);
    }
  }
});

test('all management templates expose equivalent observability settings', () => {
  for (const app of requiredApps) {
    const viewPath = resolve(
      root,
      'apps',
      app,
      'src/views/system/observability/index.vue',
    );
    const routePath = resolve(
      root,
      'apps',
      app,
      'src/router/routes/modules/system.ts',
    );
    assert.ok(existsSync(viewPath), `${app} observability view`);
    assert.ok(existsSync(routePath), `${app} observability route`);
    const view = readFileSync(viewPath, 'utf8');
    for (const token of [
      'observability.metrics.enabled',
      'observability.tracing.enabled',
      'observability.tracing.endpoint',
      'observability.tracing.protocol',
      'observability.tracing.sample_rate',
      '<section',
      'aria-labelledby="observability-title"',
      'aria-live="polite"',
    ]) {
      assert.match(view, new RegExp(token.replaceAll('.', '\\.')));
    }
    assert.doesNotMatch(view, /<main\b/);
  }
});
