import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const apps = ["web-antd", "web-ele", "web-naive"];

for (const app of apps) {
  test(`B10.3 ${app} exposes a bounded permission metadata matrix`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/permissions/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/iam.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} permission view`);
    assert.equal(existsSync(new URL(routePath, root)), true, `${app} route`);
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);
    assert.match(api, /IAMPermission/);
    assert.match(api, /function listIAMPermissionsApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.listIAMPermissions/);
    assert.match(view, /listIAMPermissionsApi/);
    assert.match(view, /iam-permissions/);
    assert.match(view, /permissionsLoading/);
    assert.match(view, /permissionsLoadError/);
    assert.match(view, /permissionsEmpty/);
    assert.match(view, /permissionMethod/);
    assert.match(view, /permissionPath/);
    assert.match(view, /permissionActive/);
    assert.match(view, /readOnly/);
    assert.match(route, /path:\s*'permissions'/);
    assert.match(route, /views\/iam\/permissions\/index\.vue/);
    assert.doesNotMatch(
      view,
      /POST|PUT|PATCH|DELETE|createIAMPermissionApi|updateIAMPermissionApi|deleteIAMPermissionApi/,
    );
    assert.doesNotMatch(view, /listIAMPoliciesApi|listIAMDataScopesApi/);
    assert.doesNotMatch(
      api,
      /createIAMPermissionApi|updateIAMPermissionApi|deleteIAMPermissionApi/,
    );
  });
}

test("B10.3 permission matrix keeps bilingual bounded copy", () => {
  for (const app of apps) {
    for (const locale of ["zh-CN", "en-US"]) {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of [
        "permissions",
        "permissionsDescription",
        "permissionsLoading",
        "permissionsLoadError",
        "permissionsEmpty",
        "permissionsTable",
        "permissionId",
        "permissionName",
        "permissionMethod",
        "permissionPath",
        "permissionActive",
        "permissionActiveYes",
        "permissionActiveNo",
      ]) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    }
  }
});
