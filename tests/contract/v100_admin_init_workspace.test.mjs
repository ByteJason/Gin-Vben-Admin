import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import test from 'node:test';

const root = resolve(import.meta.dirname, '..', '..');
const read = (...parts) => readFileSync(join(root, ...parts), 'utf8');

test('INIT-100 keeps one admin workspace and moves the installer below admin apps', () => {
  assert.equal(existsSync(join(root, 'install')), false, 'legacy root install workspace');
  assert.equal(existsSync(join(root, 'admin/apps/install/package.json')), true);
  assert.equal(existsSync(join(root, 'admin/apps/install/src/index.html')), true);
  assert.equal(existsSync(join(root, 'admin/apps/install/pnpm-lock.yaml')), false, 'nested lockfile');

  const installer = JSON.parse(read('admin/apps/install/package.json'));
  assert.equal(installer.name, '@gin-vben-admin/install');
  assert.equal(Object.hasOwn(installer.scripts ?? {}, 'dev'), false, 'installer must stay out of turbo-run dev selection');

  const lockfile = read('admin/pnpm-lock.yaml');
  assert.match(lockfile, /apps\/install:/);
});

test('INIT-100 web installer shows the CLI-selected UI as read-only state', () => {
  const html = read('admin/apps/install/src/index.html');
  const script = read('admin/apps/install/src/app.js');

  assert.doesNotMatch(html, /id="ui-choice"|name="selectedUi"/);
  assert.match(html, /id="selected-ui-summary"/);
  assert.match(html, /命令行.*选择|已选择.*管理界面/);
  assert.doesNotMatch(html, /id="confirm-cleanup"/);
  assert.doesNotMatch(script, /uiChoice|confirmCleanup/);
  assert.doesNotMatch(script, /selectedUi\s*:/);
  assert.match(script, /JSON\.stringify\(\{\s*mode\s*\}\)/);
  assert.match(script, /重新启动|重启/);
  assert.match(script, /pnpm run dev/);
  assert.match(script, /pnpm run build/);
  assert.match(script, /\.focus\(\)/, 'state and error transitions move focus');
  assert.match(script, /aria-valuetext/);
});

test('INIT-100 build, test, and deploy entrypoints use the selected profile without legacy install paths', () => {
  const build = read('scripts/build.mjs');
  const verify = read('scripts/verify.mjs');
  const playwright = read('admin/playwright.config.ts');
  const dockerfile = read('deploy/admin.Dockerfile');

  for (const source of [build, verify]) {
    assert.doesNotMatch(source, /(?:^|["'` ])(?:\.\.\/)?install\//m);
    assert.match(source, /admin(?:['"`, ]+|\/)apps(?:['"`, ]+|\/)install|admin\/apps\/install/);
  }
  assert.doesNotMatch(playwright, /\.\.\/install\//);
  assert.match(playwright, /apps\/install\/dist/);
  assert.match(dockerfile, /\.ui-profile\.json/);
  assert.match(dockerfile, /ADMIN_UI/);
  assert.match(dockerfile, /UI_PROFILE_MISMATCH/);
  assert.doesNotMatch(dockerfile, /ARG UI_APP=web-antd/);
});

test('INIT-100 uses the ordinary API installer without a temporary runtime', () => {
  assert.equal(existsSync(join(root, 'server/cmd/init')), false);
  assert.equal(existsSync(join(root, 'admin/scripts/init-runtime.mjs')), false);
  const cli = read('admin/scripts/init.mjs');
  assert.match(cli, /--no-open/);
  assert.match(cli, /--port/);
  assert.match(cli, /health\/live/);
  assert.match(cli, /api\/system\/install\/v1\/status/);
  assert.doesNotMatch(cli, /cmd\/init|init-runtime/);
});

test('INIT-100 public clone and acceptance docs use init before public dev/build', () => {
  const rootReadme = read('README.md');
  const adminReadme = read('admin/README.md');
  const acceptance = read('docs/manual-acceptance/1.0.0-dev-end-to-end.md');

  for (const document of [rootReadme, adminReadme, acceptance]) {
    assert.match(document, /pnpm run init/);
    assert.match(document, /pnpm run (?:dev|build)/);
  }
  assert.match(rootReadme, /ADMIN_UI=antd docker compose/);
  assert.match(acceptance, /只读/);
  assert.doesNotMatch(acceptance, /选择 UI=`antd`/);
});

test('INIT-100 documents durable automatic recovery without hidden-file troubleshooting', () => {
  const rootReadme = read('README.md');
  const adminReadme = read('admin/README.md');
  const acceptance = read('docs/manual-acceptance/1.0.0-dev-end-to-end.md');

  for (const document of [rootReadme, adminReadme]) {
    assert.match(document, /中断/);
    assert.match(document, /\.runtime\/install\/transaction\.json/);
    assert.match(document, /重新运行|重新打开/);
    assert.match(document, /不要手动删除/);
  }
  assert.match(acceptance, /transaction\.json|持久化事务/);
  assert.match(acceptance, /自动恢复|可重试/);
  assert.doesNotMatch(acceptance, /for %F|Get-Content.*ui-init/);
});
