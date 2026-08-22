import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const apps = ["web-antd", "web-ele", "web-naive"];

for (const app of apps) {
  test(`B10.3 ${app} exposes a bounded menu metadata reader`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/menus/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/iam.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} menu view`);
    assert.equal(existsSync(new URL(routePath, root)), true, `${app} route`);
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);
    assert.match(api, /IAMMenu/);
    assert.match(api, /function listIAMMenusApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.listIAMMenus/);
    assert.match(view, /listIAMMenusApi/);
    assert.match(view, /iam-menus/);
    assert.match(view, /menuLoading|menusLoading/);
    assert.match(view, /menuError|menusLoadError/);
    assert.match(view, /menuEmpty|menusEmpty/);
    assert.match(view, /parentId|menuParent/);
    assert.match(view, /visible|menuVisible/);
    assert.match(view, /menuDepth|depth|children/);
    assert.match(route, /path:\s*'menus'/);
    assert.match(route, /views\/iam\/menus\/index\.vue/);
    assert.doesNotMatch(api, /createIAMMenuApi/);
    assert.doesNotMatch(view, /POST|createIAMMenuApi|updateIAMMenuApi|deleteIAMMenuApi/);
  });
}

test("B10.3 menu reader keeps bilingual bounded copy", () => {
  for (const app of apps) {
    for (const locale of ["zh-CN", "en-US"]) {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of [
        "menus",
        "menusDescription",
        "menusLoading",
        "menusLoadError",
        "menusEmpty",
        "menusTable",
        "menuId",
        "menuParent",
        "menuName",
        "menuPath",
        "menuVisible",
        "menuActive",
        "menuVisibleYes",
        "menuVisibleNo",
        "menuRoot",
      ]) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    }
  }
});
