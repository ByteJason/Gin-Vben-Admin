import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const apps = ["web-antd", "web-ele", "web-naive"];

for (const app of apps) {
  test(`B10.3 ${app} exposes a bounded data-scope reader`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/data-scopes/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/iam.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} data-scope view`);
    assert.equal(existsSync(new URL(routePath, root)), true, `${app} route`);
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);
    assert.match(api, /IAMDataScope/);
    assert.match(api, /function listIAMDataScopesApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.listIAMDataScopes/);
    assert.match(view, /listIAMDataScopesApi/);
    assert.match(view, /iam-data-scopes/);
    assert.match(view, /dataScopesLoading/);
    assert.match(view, /dataScopesLoadError/);
    assert.match(view, /dataScopesEmpty/);
    assert.match(view, /dataScopeSubject/);
    assert.match(view, /dataScopeRole/);
    assert.match(view, /dataScopeDomain/);
    assert.match(view, /dataScopeResource/);
    assert.match(view, /dataScopeScope/);
    assert.match(view, /dataScopeIds/);
    assert.match(view, /readOnly/);
    assert.match(route, /path:\s*'data-scopes'/);
    assert.match(route, /views\/iam\/data-scopes\/index\.vue/);
    assert.doesNotMatch(
      view,
      /POST|PUT|PATCH|DELETE|createIAMDataScopeApi|updateIAMDataScopeApi|deleteIAMDataScopeApi/,
    );
    assert.doesNotMatch(api, /createIAMDataScopeApi|updateIAMDataScopeApi|deleteIAMDataScopeApi/);
  });
}

test("B10.3 data-scope reader keeps bilingual bounded copy", () => {
  for (const app of apps) {
    for (const locale of ["zh-CN", "en-US"]) {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of [
        "dataScopes",
        "dataScopesDescription",
        "dataScopesLoading",
        "dataScopesLoadError",
        "dataScopesEmpty",
        "dataScopesTable",
        "dataScopeSubject",
        "dataScopeRole",
        "dataScopeDomain",
        "dataScopeResource",
        "dataScopeScope",
        "dataScopeIds",
        "dataScopeAll",
        "dataScopeOwn",
        "dataScopeOrg",
        "dataScopeCustom",
      ]) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    }
  }
});
