import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

for (const app of apps) {
  test(`B10.2 ${app} exposes a bounded reset-password dialog`, () => {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/users/index.vue`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} view`);
    const api = read(apiPath);
    const view = read(viewPath);
    assert.match(api, /IAMUserPasswordResetInput/);
    assert.match(api, /function resetIAMUserPasswordApi/);
    assert.match(api, /ADMIN_ENDPOINTS\.resetIAMUserPassword/);
    assert.match(api, /requestClient\.post<void>/);
    assert.match(view, /resetIAMUserPasswordApi/);
    assert.match(view, /openResetPassword|resetPassword/);
    assert.match(view, /iam-user-reset-password/);
    assert.match(view, /minlength|8/);
    assert.match(view, /maxlength=\"128\"/);
    assert.match(view, /TextEncoder/);
    assert.match(view, /resetLoading/);
    assert.match(view, /confirmResetPassword|confirm/);
    assert.match(view, /resetError|resetDone/);
    assert.doesNotMatch(api, /passwordHash|response\.password/);
    assert.doesNotMatch(view, /passwordHash|response\.password|Authorization/);
  });
}

test('B10.2 UI reset-password keeps bilingual copy and bounded credential feedback', () => {
  for (const app of apps) {
    for (const locale of ['zh-CN', 'en-US']) {
      const text = read(
        `admin/apps/${app}/src/locales/langs/${locale}/page.json`,
      );
      for (const key of [
        'resetPassword',
        'resetTitle',
        'resetDescription',
        'resetPasswordLabel',
        'confirmResetPassword',
        'resetDone',
        'resetError',
        'resetSaving',
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
