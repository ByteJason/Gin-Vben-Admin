import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

for (const app of apps) {
  test(`B10.2 ${app} exposes a confirmed bounded soft-delete action`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/users/index.vue`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} view`);
    const api = read(apiPath);
    const view = read(viewPath);
    assert.match(api, /function deleteIAMUserApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.deleteIAMUser/);
    assert.match(view, /deleteIAMUserApi/);
    assert.match(view, /deleteUser|softDelete/);
    assert.match(view, /confirm/);
    assert.match(view, /deleteError/);
    assert.match(view, /deleted/);
    assert.doesNotMatch(view, /resetIAMUserPasswordApi/);
    assert.doesNotMatch(view, /passwordHash|response\.password|Authorization/);
  });
}

test('B10.2 UI soft-delete keeps bilingual copy for confirmation and feedback', () => {
  for (const app of apps) {
    for (const locale of ['zh-CN', 'en-US']) {
      const text = read(
        `admin/apps/${app}/src/locales/langs/${locale}/page.json`,
      );
      for (const key of ['delete', 'confirmDelete', 'deleted', 'deleteError']) {
        assert.match(
          text,
          new RegExp(`"${key}"\\s*:`),
          `${app}/${locale}/${key}`,
        );
      }
    }
  }
});
