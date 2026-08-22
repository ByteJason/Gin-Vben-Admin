import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const apps = ['web-antd', 'web-ele', 'web-naive'];
const read = (path) => readFileSync(new URL(path, root), 'utf8');

test('B10.7 management page matrix is equivalent across all templates', () => {
  const pages = [
    ['system/settings', 'settings'],
    ['system/audit', 'audit'],
    ['system/files', 'files'],
    ['iam/users', 'iam'],
  ];
  for (const [page, token] of pages) {
    const snapshots = apps.map((app) => {
      const view = `admin/apps/${app}/src/views/${page}/index.vue`;
      const api = `admin/apps/${app}/src/api/core/${token === 'iam' ? 'iam' : token}.ts`;
      const route = read(`admin/apps/${app}/src/router/routes/modules/${page.split('/')[0]}.ts`);
      assert.equal(existsSync(new URL(view, root)), true, `${app}/${view}`);
      assert.match(route, new RegExp(`views/${page}/index\\.vue`), `${app}/${page} route`);
      const content = read(view);
      for (const required of ['loading', 'empty', 'error', 'aria-busy', 'role="alert"', 'tabindex', 'prefers-reduced-motion']) {
        assert.match(content, new RegExp(required, 'i'), `${app}/${page}/${required}`);
      }
      if (existsSync(new URL(api, root))) assert.match(read(api), /requestClient/);
      return content.replaceAll(/web-(antd|ele|naive)/g, 'web-template');
    });
    assert.equal(new Set(snapshots).size, 1, `${page} templates diverged`);
  }
});

test('B10.7 E2E matrix covers backend error/empty states, keyboard focus, axe and breakpoints', () => {
  const specPath = 'admin/tests/e2e/management-equivalence.spec.ts';
  assert.equal(existsSync(new URL(specPath, root)), true, specPath);
  const spec = read(specPath);
  for (const token of ['breakpoints', 'axe', 'page.route', '/system/files', 'networkidle', 'Tab', '375', '1440']) {
    assert.match(spec, new RegExp(token.replace('/', '\\/')), token);
  }
});

for (const app of apps) {
  test(`B10.7 ${app} locale keys stay aligned`, () => {
    const zh = read(`admin/apps/${app}/src/locales/langs/zh-CN/page.json`);
    const en = read(`admin/apps/${app}/src/locales/langs/en-US/page.json`);
    for (const key of ['settings', 'audit', 'files', 'iam']) {
      assert.match(zh, new RegExp(`"${key}"\\s*:`));
      assert.match(en, new RegExp(`"${key}"\\s*:`));
    }
  });
}
