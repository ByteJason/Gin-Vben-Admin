import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const apps = ["web-antd", "web-ele", "web-naive"];

for (const app of apps) {
  test(`B10.2 ${app} exposes bounded role assignment`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/users/index.vue`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} view`);
    const api = read(apiPath);
    const view = read(viewPath);
    assert.match(api, /IAMRoleUsersReplaceInput/);
    assert.match(api, /function replaceIAMRoleUsersApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.replaceIAMRoleUsers/);
    assert.match(api, /method:\s*'PUT'/);
    assert.match(api, /userIds/);
    assert.match(api, /100/);
    assert.match(view, /replaceIAMRoleUsersApi/);
    assert.match(view, /openRoleAssignment/);
    assert.match(view, /iam-user-role-assignment/);
    assert.match(view, /roleAssignmentSelectedRoleIds/);
    assert.match(view, /roleAssignmentLoading/);
    assert.match(view, /roleAssignmentError|roleAssignmentEmpty/);
    assert.match(view, /type="checkbox"/);
    assert.doesNotMatch(view, /roleAssignment[^\n]{0,160}(passwordHash|Authorization|token)/i);
  });
}

test("B10.2 UI role assignment keeps bilingual bounded feedback", () => {
  for (const app of apps) {
    for (const locale of ["zh-CN", "en-US"]) {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of [
        "roleAssignment",
        "roleAssignmentTitle",
        "roleAssignmentDescription",
        "roleAssignmentUser",
        "roleAssignmentRoles",
        "roleAssignmentEmpty",
        "roleAssignmentLoading",
        "roleAssignmentSave",
        "roleAssignmentSaving",
        "roleAssignmentError",
        "roleAssignmentDone",
        "roleAssignmentLimit",
        "roleAssignmentNoChanges",
      ]) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    }
  }
});
