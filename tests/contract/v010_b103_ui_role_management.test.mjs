import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const apps = ["web-antd", "web-ele", "web-naive"];

for (const app of apps) {
  test(`B10.3 ${app} exposes bounded role management`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/roles/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/iam.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} role view`);
    assert.equal(existsSync(new URL(routePath, root)), true, `${app} route`);
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);
    assert.match(api, /IAMRoleCreateInput/);
    assert.match(api, /function listIAMRolesApi/);
    assert.match(api, /function createIAMRoleApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.listIAMRoles/);
    assert.match(api, /ADMIN_ENDPOINTS\.createIAMRole/);
    assert.match(api, /method:\s*'POST'|requestClient\.post/);
    assert.match(view, /listIAMRolesApi/);
    assert.match(view, /createIAMRoleApi/);
    assert.match(view, /iam-roles/);
    assert.match(view, /roleForm|roleFormOpen/);
    assert.match(view, /roleLoading/);
    assert.match(view, /roleError|roleEmpty/);
    assert.match(view, /dataScope/);
    assert.match(view, /type="checkbox"/);
    assert.match(route, /path:\s*'roles'/);
    assert.match(route, /views\/iam\/roles\/index\.vue/);
    assert.doesNotMatch(view, /passwordHash|Authorization|token/i);
  });
}

test("B10.3 role management keeps bilingual bounded feedback", () => {
  for (const app of apps) {
    for (const locale of ["zh-CN", "en-US"]) {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of [
        "roles",
        "rolesDescription",
        "rolesLoading",
        "rolesLoadError",
        "rolesEmpty",
        "rolesTable",
        "roleId",
        "roleName",
        "roleDataScope",
        "roleActive",
        "roleActiveHelp",
        "roleCreate",
        "roleCreateTitle",
        "roleSave",
        "roleSaving",
        "roleCancel",
        "roleRequired",
        "roleSaveError",
        "roleCreated",
        "roleScopeAll",
        "roleScopeOwn",
        "roleScopeOrg",
        "roleScopeCustom",
      ]) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    }
  }
});
