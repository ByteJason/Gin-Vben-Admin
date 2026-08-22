import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

for (const app of apps) {
  test(`B10.3 ${app} exposes a bounded menu editor and route consumer`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/menus/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/iam.ts`;
    const accessPath = `admin/apps/${app}/src/router/access.ts`;
    const menuApiPath = `admin/apps/${app}/src/api/core/menu.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} menu view`);
    assert.equal(existsSync(new URL(routePath, root)), true, `${app} route`);
    assert.equal(existsSync(new URL(accessPath, root)), true, `${app} access`);
    assert.equal(
      existsSync(new URL(menuApiPath, root)),
      true,
      `${app} menu API`,
    );
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);
    const access = read(accessPath);
    const menuApi = read(menuApiPath);
    assert.match(api, /IAMMenu/);
    assert.match(api, /function listIAMMenusApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.listIAMMenus/);
    assert.match(api, /function createIAMMenuApi/);
    assert.match(api, /function getIAMMenuApi/);
    assert.match(api, /function updateIAMMenuApi/);
    assert.match(api, /function deleteIAMMenuApi/);
    assert.match(api, /function reorderIAMMenusApi/);
    assert.match(api, /function listIAMComponentsApi/);
    assert.match(api, /IAMMenuCreateInput/);
    assert.match(api, /IAMMenuReorderInput/);
    assert.match(view, /listIAMMenusApi/);
    assert.match(view, /iam-menus/);
    assert.match(view, /menuLoading|menusLoading/);
    assert.match(view, /menuError|menusLoadError/);
    assert.match(view, /menuEmpty|menusEmpty/);
    assert.match(view, /parentId|menuParent/);
    assert.match(view, /visible|menuVisible/);
    assert.match(view, /menuDepth|depth|children/);
    assert.match(view, /createIAMMenuApi/);
    assert.match(view, /updateIAMMenuApi/);
    assert.match(view, /deleteIAMMenuApi/);
    assert.match(view, /reorderIAMMenusApi/);
    assert.match(view, /listIAMComponentsApi/);
    assert.match(view, /menuType|component/);
    assert.match(view, /menuForm|dialog/);
    assert.match(view, /menuSaveError|menuSave/);
    assert.match(route, /path:\s*'menus'/);
    assert.match(route, /views\/iam\/menus\/index\.vue/);
    assert.match(access, /getAllMenusApi/);
    assert.match(access, /fetchMenuListAsync/);
    assert.match(access, /pageMap|layoutMap/);
    assert.match(route, /children|meta/);
    assert.match(menuApi, /RouteRecordStringComponent/);
    assert.match(menuApi, /MENU_ENDPOINT/);
  });
}

test('B10.3 menu reader keeps bilingual bounded copy', () => {
  for (const app of apps) {
    for (const locale of ['zh-CN', 'en-US']) {
      const text = read(
        `admin/apps/${app}/src/locales/langs/${locale}/page.json`,
      );
      for (const key of [
        'menus',
        'menusDescription',
        'menusLoading',
        'menusLoadError',
        'menusEmpty',
        'menusTable',
        'menuId',
        'menuParent',
        'menuName',
        'menuPath',
        'menuVisible',
        'menuActive',
        'menuVisibleYes',
        'menuVisibleNo',
        'menuRoot',
        'menuCreate',
        'menuEdit',
        'menuDelete',
        'menuSave',
        'menuCancel',
        'menuType',
        'menuComponent',
        'menuRedirect',
        'menuIcon',
        'menuPermission',
        'menuSort',
        'menuKeepAlive',
        'menuExternal',
        'menuDirectory',
        'menuPage',
        'menuButton',
        'menuSaving',
        'menuSaveError',
        'menuSaved',
        'menuDeleteConfirm',
        'menuDeleteError',
        'menuReorder',
        'menuReorderSaved',
        'menuReorderError',
        'menuComponentsLoadError',
        'menuCreateChild',
        'menuFormDescription',
        'menuReorderHelp',
        'menuDeleted',
      ]) {
        assert.match(
          text,
          new RegExp(`"${key}"\\s*:`),
          `${app}/${locale}/${key}`,
        );
      }
    }
  }
});
