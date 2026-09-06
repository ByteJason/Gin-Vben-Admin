import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const root = new URL('../', import.meta.url);
const apps = ['web-antd', 'web-ele', 'web-naive'];
const resources = [
  ['system/tasks/index.vue', 'taskDrawerOpen'],
  ['system/dictionary/index.vue', 'typeEditorOpen'],
  ['system/mail/index.vue', 'accountEditorOpen'],
  ['system/mail/index.vue', 'callerEditorOpen'],
  ['system/mail/index.vue', 'templateEditorOpen'],
  ['system/files/index.vue', 'categoryEditorOpen'],
  ['iam/users/index.vue', 'formOpen'],
  ['iam/roles/index.vue', 'roleFormOpen'],
  ['iam/menus/index.vue', 'formOpen'],
];

function source(app, path) {
  return readFileSync(new URL(`apps/${app}/src/views/${path}`, root), 'utf8');
}

test('all CRUD resource editors use the shared responsive drawer contract', () => {
  for (const app of apps) {
    for (const [path, state] of resources) {
      const code = source(app, path);
      assert.match(code, /ManagementDrawer/, `${app}/${path} has no drawer`);
      assert.match(
        code,
        new RegExp(`:open="${state}"`),
        `${app}/${path} does not bind ${state}`,
      );
      assert.match(code, /@close=/, `${app}/${path} has no close path`);
    }
  }
});

test('simple confirmation actions remain lightweight dialogs', () => {
  for (const app of apps) {
    const users = source(app, 'iam/users/index.vue');
    assert.match(users, /v-if="resetOpen && canManage"/);
    const tasks = source(app, 'system/tasks/index.vue');
    assert.match(
      tasks,
      /window\.confirm\(String\(\$t\('page\.tasks\.deleteConfirm'\)\)\)/,
    );
  }
});

test('the shared drawer exposes mobile width, focus and submit-lock guarantees', () => {
  const component = readFileSync(
    new URL(
      'packages/effects/common-ui/src/components/management-drawer/management-drawer.vue',
      root,
    ),
    'utf8',
  );
  assert.match(component, /useFocusTrap/);
  assert.match(component, /width: 100vw/);
  assert.match(component, /height: 100dvh/);
  assert.match(component, /:submitting="busy"/);
  assert.match(component, /closeOnClickModal: false/);
});
