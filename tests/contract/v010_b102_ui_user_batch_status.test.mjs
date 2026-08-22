import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

for (const app of apps) {
  test(`B10.2 ${app} exposes bounded batch user status controls`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/users/index.vue`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} view`);
    const api = read(apiPath);
    const view = read(viewPath);
    assert.match(api, /IAMUserBatchStatusInput/);
    assert.match(api, /IAMUserBatchStatusResponse/);
    assert.match(api, /function batchUpdateIAMUserStatusApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.batchUpdateIAMUserStatus/);
    assert.match(view, /selectedIds/);
    assert.match(view, /iam-users-select-all/);
    assert.match(view, /iam-user-select-/);
    assert.match(view, /batchEnable/);
    assert.match(view, /batchDisable/);
    assert.match(view, /confirm/);
    assert.doesNotMatch(view, /deleteIAMUserApi|resetIAMUserPasswordApi/);
    assert.doesNotMatch(view, /passwordHash|response\.password|Authorization/);
  });
}

test('B10.2 UI batch status keeps bilingual copy for selection and feedback', () => {
  for (const app of apps) {
    for (const locale of ['zh-CN', 'en-US']) {
      const text = read(
        `admin/apps/${app}/src/locales/langs/${locale}/page.json`,
      );
      for (const key of [
        'selectAll',
        'selectedCount',
        'batchEnable',
        'batchDisable',
        'batchConfirmDisable',
        'batchUpdated',
        'batchError',
      ]) {
        assert.match(
          text,
          new RegExp(`"${key}"\\s*:`),
          `${app}/${locale}/${key}`,
        );
      }
    }
  }
});
