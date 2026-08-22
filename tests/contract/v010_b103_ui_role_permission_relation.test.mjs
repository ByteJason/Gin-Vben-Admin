import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");
const templates = ["web-antd", "web-ele", "web-naive"];

test("B10.3 all management UIs expose bounded role-permission editing", async () => {
  for (const template of templates) {
    const api = await read(`admin/apps/${template}/src/api/core/iam.ts`);
    const view = await read(`admin/apps/${template}/src/views/iam/roles/index.vue`);
    const zh = JSON.parse(await read(`admin/apps/${template}/src/locales/langs/zh-CN/page.json`));
    const en = JSON.parse(await read(`admin/apps/${template}/src/locales/langs/en-US/page.json`));
    assert.match(api, /permissionIds\??: string\[\]/);
    assert.match(api, /IAMRolePermissionsReplaceInput/);
    assert.match(api, /replaceIAMRolePermissionsApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.replaceIAMRolePermissions/);
    assert.match(api, /rolePermissionLimit = 200/);
    assert.match(view, /listIAMPermissionsApi/);
    assert.match(view, /replaceIAMRolePermissionsApi/);
    assert.match(view, /permissionIds/);
    assert.match(view, /type="checkbox"/);
    assert.match(view, /rolePermissionEditor/);
    assert.match(view, /rolePermissionsLoading/);
    assert.match(view, /rolePermissionsEmpty/);
    assert.match(view, /rolePermissionsLoadError/);
    assert.match(view, /rolePermissionSaveError/);
    assert.doesNotMatch(view, /password|token|secret/i);
    for (const locale of [zh.iam, en.iam]) {
      for (const key of [
        "rolePermissions",
        "rolePermissionEdit",
        "rolePermissionTitle",
        "rolePermissionDescription",
        "rolePermissionEmpty",
        "rolePermissionsLoading",
        "rolePermissionsLoadError",
        "rolePermissionSave",
        "rolePermissionSaving",
        "rolePermissionDone",
        "rolePermissionSaveError",
        "rolePermissionLimit",
      ]) {
        assert.equal(typeof locale[key], "string", `${template}.${key}`);
      }
    }
  }
});
