import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const apps = ["web-antd", "web-ele", "web-naive"];

for (const app of apps) {
  test(`B10.3 ${app} exposes a bounded policy reader`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/policies/index.vue`;
    const routePath = `admin/apps/${app}/src/router/routes/modules/iam.ts`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} policy view`);
    assert.equal(existsSync(new URL(routePath, root)), true, `${app} route`);
    const api = read(apiPath);
    const view = read(viewPath);
    const route = read(routePath);
    assert.match(api, /IAMPolicy/);
    assert.match(api, /function listIAMPoliciesApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.listIAMPolicies/);
    assert.match(view, /listIAMPoliciesApi/);
    assert.match(view, /iam-policies/);
    assert.match(view, /policiesLoading/);
    assert.match(view, /policiesLoadError/);
    assert.match(view, /policiesEmpty/);
    assert.match(view, /policySubject/);
    assert.match(view, /policyRole/);
    assert.match(view, /policyDomain/);
    assert.match(view, /policyMethod/);
    assert.match(view, /policyPath/);
    assert.match(view, /policyEffect/);
    assert.match(view, /readOnly/);
    assert.match(route, /path:\s*'policies'/);
    assert.match(route, /views\/iam\/policies\/index\.vue/);
    assert.doesNotMatch(
      view,
      /POST|PUT|PATCH|DELETE|createIAMPolicyApi|updateIAMPolicyApi|deleteIAMPolicyApi/,
    );
    assert.doesNotMatch(view, /listIAMDataScopesApi|createIAMDataScopeApi/);
    assert.doesNotMatch(api, /createIAMPolicyApi|updateIAMPolicyApi|deleteIAMPolicyApi/);
  });
}

test("B10.3 policy reader keeps bilingual bounded copy", () => {
  for (const app of apps) {
    for (const locale of ["zh-CN", "en-US"]) {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of [
        "policies",
        "policiesDescription",
        "policiesLoading",
        "policiesLoadError",
        "policiesEmpty",
        "policiesTable",
        "policySubject",
        "policyRole",
        "policyDomain",
        "policyMethod",
        "policyPath",
        "policyEffect",
        "policyAllow",
        "policyDeny",
      ]) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    }
  }
});
