import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const root = new URL('../', import.meta.url);
const templates = ['web-antd', 'web-ele', 'web-naive'];

function read(template, path) {
  return readFileSync(new URL(`apps/${template}/src/${path}`, root), 'utf8');
}

const managedPages = [
  ['views/iam/users/index.vue', 'iam:users:manage'],
  ['views/iam/roles/index.vue', 'iam:roles:manage'],
  ['views/iam/menus/index.vue', 'iam:menus:manage'],
  ['views/system/settings/index.vue', 'system:settings:manage'],
  ['views/system/dictionary/index.vue', 'system:dictionary:manage'],
  ['views/system/mail/index.vue', 'system:mail:manage'],
  ['views/system/files/index.vue', 'system:files:manage'],
  ['views/system/observability/index.vue', 'system:observability:manage'],
  ['views/system/tasks/index.vue', 'ops:tasks:manage'],
  ['views/system/import-export/index.vue', 'ops:data-jobs:manage'],
];

const guardedHandlers = new Map([
  [
    'views/iam/users/index.vue',
    {
      canManage: [
        'openCreate',
        'openEdit',
        'submitUserForm',
        'openResetPassword',
        'submitResetPassword',
        'deleteUser',
        'batchUpdate',
      ],
      canManageRoleAssignments: [
        'openRoleAssignment',
        'submitRoleAssignment',
      ],
    },
  ],
  [
    'views/iam/roles/index.vue',
    {
      canEditDataScopes: [
        'openDataScopeEditor',
        'addDataScopeBinding',
        'removeDataScopeBinding',
        'submitDataScopes',
      ],
      canEditPermissions: ['openPermissionEditor', 'submitPermissions'],
      canManage: ['openCreateRole', 'submitRole'],
    },
  ],
  [
    'views/iam/menus/index.vue',
    {
      canEditMenus: ['openCreateMenu', 'openEditMenu', 'saveMenu'],
      canManage: ['deleteMenu', 'setSort', 'saveReorder'],
    },
  ],
  [
    'views/system/settings/index.vue',
    { canManage: ['save', 'testConnection', 'rollback'] },
  ],
  [
    'views/system/dictionary/index.vue',
    {
      canManage: [
        'editType',
        'editItem',
        'saveType',
        'saveItem',
        'removeType',
        'removeItem',
        'importItems',
      ],
    },
  ],
  [
    'views/system/mail/index.vue',
    { canManage: ['edit', 'save', 'test', 'remove'] },
  ],
  [
    'views/system/files/index.vue',
    {
      canManage: [
        'onFileChange',
        'upload',
        'deleteItem',
        'createSignedURL',
        'cleanupDryRun',
      ],
    },
  ],
  [
    'views/system/observability/index.vue',
    { canManage: ['update', 'save'] },
  ],
  [
    'views/system/tasks/index.vue',
    {
      canManage: [
        'editTask',
        'saveTask',
        'removeTask',
        'runTask',
        'cancelRun',
        'retryRun',
      ],
    },
  ],
  [
    'views/system/import-export/index.vue',
    {
      canManage: [
        'chooseFile',
        'commitImport',
        'exportPreview',
        'cancelJob',
        'retryJob',
      ],
    },
  ],
]);

test('management pages render read-only data while gating every write surface', () => {
  for (const template of templates) {
    for (const [path, code] of managedPages) {
      const source = read(template, path);
      assert.match(source, /import \{ useAccess \} from '@vben\/access';/);
      assert.ok(
        source.includes(`hasAccessByCodes(['${code}'])`),
        `${template}/${path} does not consume ${code}`,
      );
      assert.match(
        source,
        /const canManage = computed\(/,
        `${template}/${path} has no reactive manage gate`,
      );
      assert.match(
        source,
        /if \(!canManage\.value\) return;/,
        `${template}/${path} write handlers are not guarded`,
      );
      assert.match(
        source,
        /v-if="canManage"/,
        `${template}/${path} still exposes write controls to read-only users`,
      );
      for (const [gate, handlers] of Object.entries(
        guardedHandlers.get(path) ?? {},
      )) {
        for (const handler of handlers) {
          assert.match(
            source,
            new RegExp(
              `(?:async )?function ${handler}\\([^)]*\\) \\{\\s*if \\(!${gate}\\.value\\) return;`,
            ),
            `${template}/${path} does not guard ${handler} with ${gate}`,
          );
        }
      }
    }
  }
});

test('IAM relationship editors degrade independently from their primary lists', () => {
  for (const template of templates) {
    const users = read(template, 'views/iam/users/index.vue');
    assert.ok(users.includes("hasAccessByCodes(['iam:roles:read'])"));
    assert.ok(users.includes("hasAccessByCodes(['iam:roles:manage'])"));
    assert.match(users, /if \(!canReadRoles\.value\) return;/);
    assert.match(users, /v-if="canManageRoleAssignments"/);
    assert.doesNotMatch(
      users,
      /onMounted\(async \(\) => \{\s*await Promise\.all\(\[loadUsers\(\), loadRoles\(\)\]\);/,
    );

    const roles = read(template, 'views/iam/roles/index.vue');
    for (const code of ['iam:permissions:read', 'iam:data-scopes:read']) {
      assert.ok(
        roles.includes(`hasAccessByCodes(['${code}'])`),
        `${template}/roles does not consume ${code}`,
      );
    }
    for (const gate of [
      'canEditPermissions',
      'canEditDataScopes',
    ]) {
      assert.ok(
        roles.includes(`v-if="${gate}"`),
        `${template}/roles still exposes ${gate} without all dependencies`,
      );
    }
    assert.match(roles, /function loadPermissions\(\) \{\s*if \(!canReadPermissions\.value\) return;/);
    assert.match(roles, /function loadRoleDataScopes\(\) \{\s*if \(!canReadDataScopes\.value\) return;/);

    const menus = read(template, 'views/iam/menus/index.vue');
    assert.ok(menus.includes("hasAccessByCodes(['iam:components:read'])"));
    assert.match(menus, /if \(!canReadComponents\.value\) return;/);
    assert.match(menus, /v-if="canEditMenus"/);
    assert.doesNotMatch(
      menus,
      /onMounted\(async \(\) => \{\s*await Promise\.all\(\[loadMenus\(\), loadComponents\(\)\]\);/,
    );

    const dictionary = read(
      template,
      'views/system/dictionary/index.vue',
    );
    assert.ok(
      dictionary.includes("hasAccessByCodes(['system:settings:read'])"),
    );
    assert.match(
      dictionary,
      /function loadLocalePolicy\(\) \{\s*if \(!canReadSettings\.value\) return;/,
    );
  }
});

test('profile entry is development-only in every application shell', () => {
  for (const template of templates) {
    const layout = read(template, 'layouts/basic.vue');
    assert.match(layout, /const showProfileEntry = import\.meta\.env\.DEV;/);
    assert.match(layout, /\.\.\.\(showProfileEntry\s*\?\s*\[/);
    assert.match(
      layout,
      /showProfileEntry[\s\S]*?router\.push\(\{ name: 'Profile' \}\)/,
    );
  }
});

test('hidden IAM compatibility routes reject direct URLs without read access', () => {
  for (const template of templates) {
    const routes = read(template, 'router/routes/modules/iam.ts');
    for (const [path, code] of [
      ['policies', 'iam:policies:read'],
      ['data-scopes', 'iam:data-scopes:read'],
    ]) {
      assert.match(
        routes,
        new RegExp(
          `component: \\(\\) => import\\('#/views/iam/${path}/index\\.vue'\\),[\\s\\S]*?meta: \\{[\\s\\S]*?authority: \\['${code}'\\],[\\s\\S]*?hideInMenu: true`,
        ),
        `${template}/${path} has no direct-navigation authority gate`,
      );
    }
  }
});
