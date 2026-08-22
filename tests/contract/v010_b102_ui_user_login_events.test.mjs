import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const apps = ["web-antd", "web-ele", "web-naive"];

for (const app of apps) {
  test(`B10.2 ${app} exposes a bounded login-events reader`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/users/index.vue`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} view`);
    const api = read(apiPath);
    const view = read(viewPath);
    assert.match(api, /IAMUserLoginEvent/);
    assert.match(api, /IAMUserLoginEventPage/);
    assert.match(api, /function listIAMUserLoginEventsApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.listIAMUserLoginEvents/);
    assert.match(api, /from\?|to\?|limit\?|offset\?/);
    assert.match(view, /listIAMUserLoginEventsApi/);
    assert.match(view, /openLoginEvents/);
    assert.match(view, /iam-user-login-events/);
    assert.match(view, /datetime-local/);
    assert.match(view, /loginEventsPage|offset/);
    assert.match(view, /loginEventsError|loginEventsEmpty/);
    assert.doesNotMatch(view, /event\.details/);
    assert.doesNotMatch(view, /passwordHash|response\.password|Authorization/);
  });
}

test("B10.2 UI login-events keeps bilingual copy and bounded filters", () => {
  for (const app of apps) {
    for (const locale of ["zh-CN", "en-US"]) {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of [
        "loginEvents",
        "loginEventsTitle",
        "loginEventsDescription",
        "loginEventsFrom",
        "loginEventsTo",
        "loginEventsOutcome",
        "loginEventsRequestId",
        "loginEventsAction",
        "loginEventsResource",
        "loginEventsTable",
        "loginEventsEmpty",
        "loginEventsError",
        "loginEventsLoading",
        "loginEventsApply",
        "loginEventsReset",
        "loginEventsDateError",
        "loginEventsPrevious",
        "loginEventsNext",
      ]) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    }
  }
});
