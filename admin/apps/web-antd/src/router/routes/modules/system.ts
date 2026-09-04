import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

type Authority = string | string[];

const page = (
  name: string,
  path: string,
  component: () => Promise<any>,
  authority: Authority,
  title: string,
  icon: string,
  hideInMenu = false,
): RouteRecordRaw => ({
  name,
  path,
  component,
  meta: {
    authority: Array.isArray(authority) ? authority : [authority],
    icon,
    title: $t(title),
    ...(hideInMenu ? { hideInMenu: true, hideInTab: true } : {}),
  },
});

const routes: RouteRecordRaw[] = [
  {
    name: 'menu-system-config',
    path: '/system',
    redirect: '/system/settings',
    meta: {
      authority: [
        'system:dictionary:read',
        'system:settings:read',
        'system:mail:read',
      ],
      icon: 'lucide:settings',
      order: 30,
      title: $t('page.navigation.system'),
    },
    children: [
      page(
        'menu-system-dictionary',
        'dictionary',
        () => import('#/views/system/dictionary/index.vue'),
        'system:dictionary:read',
        'page.dictionary.title',
        'lucide:book-open',
      ),
      {
        component: () => import('#/views/system/settings/index.vue'),
        meta: {
          authority: ['system:settings:read'],
          icon: 'lucide:settings',
          title: $t('page.navigation.settings'),
        },
        name: 'menu-system-settings',
        path: '/system/settings',
      },
      {
        path: '/system/parameters',
        redirect: '/system/settings',
        name: 'LegacySystemParameters',
        meta: { hideInMenu: true, hideInTab: true, title: $t('page.navigation.settings') },
      },
      page(
        'menu-system-mail',
        'mail',
        () => import('#/views/system/mail/index.vue'),
        'system:mail:read',
        'page.navigation.mail',
        'lucide:mail',
      ),
      {
        path: '/system/observability',
        redirect: '/system/settings',
        name: 'LegacySystemObservability',
        meta: { hideInMenu: true, hideInTab: true, title: $t('page.navigation.settings') },
      },
    ],
  },
  {
    name: 'menu-operations',
    path: '/ops',
    redirect: '/ops/server-status',
    meta: {
      authority: [
        'ops:monitor:read',
        'ops:server-status:read',
        'ops:audit:read',
        'ops:operation-history:read',
        'ops:login-logs:read',
        'ops:tasks:read',
        'ops:data-jobs:read',
      ],
      icon: 'lucide:activity',
      order: 10,
      title: $t('page.navigation.ops'),
    },
    children: [
      page(
        'menu-operations-monitor',
        'server-status',
        () => import('#/views/system/monitor/index.vue'),
        ['ops:monitor:read', 'ops:server-status:read'],
        'page.navigation.serverStatus',
        'lucide:monitor-cog',
      ),
      page(
        'menu-operations-audit',
        'operation-history',
        () => import('#/views/ops/operation-history/index.vue'),
        ['ops:audit:read', 'ops:operation-history:read'],
        'page.navigation.operationHistory',
        'lucide:scroll-text',
      ),
      page(
        'menu-operations-login-logs',
        'login-logs',
        () => import('#/views/ops/login-logs/index.vue'),
        ['ops:audit:read', 'ops:login-logs:read'],
        'page.navigation.loginLogs',
        'lucide:log-in',
      ),
      page(
        'menu-operations-tasks',
        'tasks',
        () => import('#/views/system/tasks/index.vue'),
        'ops:tasks:read',
        'page.navigation.tasks',
        'lucide:workflow',
      ),
      page(
        'menu-operations-data-jobs',
        'data-jobs',
        () => import('#/views/system/import-export/index.vue'),
        'ops:data-jobs:read',
        'page.navigation.dataJobs',
        'lucide:file-spreadsheet',
      ),
    ],
  },
  {
    name: 'LegacyConfiguration',
    path: '/configuration',
    redirect: '/system',
    meta: {
      hideInMenu: true,
      hideInTab: true,
      title: $t('page.navigation.system'),
    },
  },
  {
    component: () => import('#/views/system/audit/index.vue'),
    meta: {
      authority: ['ops:audit:read'],
      hideInMenu: true,
      hideInTab: true,
      title: $t('page.audit.title'),
    },
    // legacy audit route keeps bookmarked URLs while the production menu lives under /ops.
    // name: 'menu-operations-audit'
    // authority: ['ops:audit:read']
    name: 'LegacySystemAudit',
    path: '/system/audit',
  },
  {
    component: () => import('#/views/system/files/index.vue'),
    meta: {
      authority: ['system:files:read'],
      hideInMenu: true,
      hideInTab: true,
      title: $t('page.navigation.files'),
    },
    name: 'menu-system-files',
    path: '/system/files',
  },
  ...[
    ['/system/monitor', '/ops/server-status', 'LegacySystemMonitor'],
    ['/system/tasks', '/ops/tasks', 'LegacySystemTasks'],
    ['/system/import-export', '/ops/data-jobs', 'LegacySystemDataJobs'],
  ].map(
    ([path, redirect, name]) =>
      ({
        path,
        redirect,
        name,
        meta: {
          hideInMenu: true,
          hideInTab: true,
          title: $t('page.navigation.ops'),
        },
      }) as RouteRecordRaw,
  ),
];

export default routes;
