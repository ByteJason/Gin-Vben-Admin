import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");
const templates = ["web-antd", "web-ele", "web-naive"];

test("B10.3 all management UIs expose bounded role data-scope editing", async () => {
  for (const template of templates) {
    const api = await read(`admin/apps/${template}/src/api/core/iam.ts`);
    const view = await read(`admin/apps/${template}/src/views/iam/roles/index.vue`);
    const zh = JSON.parse(await read(`admin/apps/${template}/src/locales/langs/zh-CN/page.json`));
    const en = JSON.parse(await read(`admin/apps/${template}/src/locales/langs/en-US/page.json`));
    assert.match(api, /IAMRoleDataScopeBinding/);
    assert.match(api, /IAMRoleDataScopesReplaceInput/);
    assert.match(api, /replaceIAMRoleDataScopesApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.replaceIAMRoleDataScopes/);
    assert.match(api, /roleDataScopeBindingLimit = 50/);
    assert.match(api, /roleDataScopeIdsLimit = 200/);
    assert.match(view, /listIAMDataScopesApi/);
    assert.match(view, /replaceIAMRoleDataScopesApi/);
    assert.match(view, /dataScopeEditorOpen/);
    assert.match(view, /roleDataScopeEditor/);
    assert.match(view, /roleDataScopeResource/);
    assert.match(view, /roleDataScopeScope/);
    assert.match(view, /roleDataScopeIds/);
    assert.match(view, /dataScopeBindings\.length/);
    assert.match(view, /roleDataScopeBindingLimit/);
    assert.match(view, /roleDataScopeIdsLimit/);
    assert.match(view, /dataScopesLoading/);
    assert.match(view, /roleDataScopeBindingsEmpty/);
    assert.match(view, /roleDataScopeBindingsLoadError/);
    assert.match(view, /roleDataScopeSaveError/);
    assert.doesNotMatch(view, /password|token|secret/i);
    for (const locale of [zh.iam, en.iam]) {
      for (const key of [
        "roleDataScopeEdit",
        "roleDataScopeTitle",
        "roleDataScopeDescription",
        "roleDataScopeAdd",
        "roleDataScopeRemove",
        "roleDataScopeResource",
        "roleDataScopeScope",
        "roleDataScopeIds",
        "roleDataScopeIdsHelp",
        "roleDataScopeBindingsLoading",
        "roleDataScopeBindingsEmpty",
        "roleDataScopeBindingsLoadError",
        "roleDataScopeSave",
        "roleDataScopeSaving",
        "roleDataScopeDone",
        "roleDataScopeSaveError",
        "roleDataScopeBindingLimit",
        "roleDataScopeIdsLimit",
      ]) {
        assert.equal(typeof locale[key], "string", `${template}.${key}`);
      }
    }
  }
});
