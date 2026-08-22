import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const read = (path) => readFileSync(new URL(path, root), 'utf8');
const apps = ['web-antd', 'web-ele', 'web-naive'];

test('B10.2 UI create/edit exposes equivalent bounded user form', () => {
  for (const app of apps) {
    const apiPath = `admin/apps/${app}/src/api/core/iam.ts`;
    const viewPath = `admin/apps/${app}/src/views/iam/users/index.vue`;
    assert.equal(existsSync(new URL(apiPath, root)), true, `${app} api`);
    assert.equal(existsSync(new URL(viewPath, root)), true, `${app} view`);
    const api = read(apiPath);
    const view = read(viewPath);
    for (const fn of [
      'createIAMUserApi',
      'getIAMUserApi',
      'updateIAMUserApi',
    ]) {
      assert.match(api, new RegExp(`function ${fn}`), `${app} ${fn}`);
    }
    assert.match(api, /ADMIN_ENDPOINTS\.createIAMUser/);
    assert.match(api, /ADMIN_ENDPOINTS\.getIAMUser/);
    assert.match(api, /ADMIN_ENDPOINTS\.updateIAMUser/);
    assert.match(view, /createUser|openCreate/);
    assert.match(view, /editUser|openEdit/);
    assert.match(view, /role="dialog"/);
    for (const field of ['username', 'nickname', 'email', 'phone', 'orgId', 'active']) {
      assert.match(view, new RegExp(`iam-user-${field}`), `${app} ${field}`);
    }
    assert.match(view, /minlength|length|8/);
    assert.doesNotMatch(view, /passwordHash|response\.password|Authorization/);
  }
});

test('B10.2 UI create/edit keeps bilingual copy for actions and validation', () => {
  for (const app of apps) {
    for (const locale of ['zh-CN', 'en-US']) {
      const text = read(`admin/apps/${app}/src/locales/langs/${locale}/page.json`);
      for (const key of ['create', 'edit', 'save', 'cancel', 'required', 'invalidEmail']) {
        assert.match(text, new RegExp(`"${key}"\\s*:`), `${app}/${locale}/${key}`);
      }
    }
  }
});
