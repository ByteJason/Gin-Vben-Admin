import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');

const apps = ['web-antd', 'web-ele', 'web-naive'];

test('B10.2 UI users list has equivalent route, API adapter, and page for every template', () => {
  for (const app of apps) {
    const route = `admin/apps/${app}/src/router/routes/modules/iam.ts`;
    const api = `admin/apps/${app}/src/api/core/iam.ts`;
    const view = `admin/apps/${app}/src/views/iam/users/index.vue`;
    assert.equal(existsSync(new URL(route, root)), true, `${app} route`);
    assert.equal(existsSync(new URL(api, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(view, root)), true, `${app} view`);

    const routeText = read(route);
    const apiText = read(api);
    const viewText = read(view);
    assert.match(routeText, /path:\s*['"]\/iam['"]/);
    assert.match(routeText, /path:\s*['"]users['"]/);
    assert.match(routeText, /name:\s*['"]menu-identity-users['"]/);
    assert.match(routeText, /component:\s*\(\) => import\(['"]#\/views\/iam\/users\/index\.vue['"]\)/);
    assert.match(routeText, /authority:\s*\[['"]iam:users:read['"]\]/);
    assert.match(apiText, /ADMIN_ENDPOINTS\.listIAMUsers/);
    assert.match(apiText, /pageSize/);
    assert.match(apiText, /roleId/);
    assert.match(apiText, /orgId/);
    assert.match(viewText, /iam-users-page/);
    assert.match(viewText, /aria-busy/);
    assert.match(viewText, /role="alert"/);
    assert.match(viewText, /role="table"|<table/);
    assert.match(viewText, /pageSize/);
    assert.match(viewText, /status/);
    assert.doesNotMatch(viewText, /passwordHash|response\.password|Authorization/);
  }
});

test('B10.2 UI users list has bilingual page copy and bounded pagination', () => {
  for (const app of apps) {
    const zh = read(`admin/apps/${app}/src/locales/langs/zh-CN/page.json`);
    const en = read(`admin/apps/${app}/src/locales/langs/en-US/page.json`);
    for (const source of [zh, en]) {
      assert.match(source, /"iam"\s*:/);
      assert.match(source, /"users"\s*:/);
      assert.match(source, /"search"\s*:/);
      assert.match(source, /"empty"\s*:/);
    }
  }
});
