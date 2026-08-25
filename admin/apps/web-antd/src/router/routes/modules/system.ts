import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      authority: [
        'system:settings:read',
        'system:dictionary:read',
        'system:mail:read',
        'system:files:read',
        'system:observability:read',
      ],
      icon: 'lucide:settings',
      order: 30,
      title: $t('page.navigation.configuration'),
    },
    name: 'menu-system-config',
    path: '/configuration',
    redirect: '/system/settings',
    children: [
      {
        component: () => import('#/views/system/settings/index.vue'),
        meta: {
          authority: ['system:settings:read'],
          icon: 'lucide:sliders-horizontal',
          title: $t('page.navigation.settings'),
        },
        name: 'menu-system-settings',
        path: '/system/settings',
      },
      {
        component: () => import('#/views/system/dictionary/index.vue'),
        meta: {
          authority: ['system:dictionary:read'],
          icon: 'lucide:book-open',
          title: $t('page.dictionary.title'),
        },
        name: 'menu-system-dictionary',
        path: '/system/dictionary',
      },
      {
        component: () => import('#/views/system/mail/index.vue'),
        meta: {
          authority: ['system:mail:read'],
          icon: 'lucide:mail',
          title: $t('page.navigation.mail'),
        },
        name: 'menu-system-mail',
        path: '/system/mail',
      },
      {
        component: () => import('#/views/system/files/index.vue'),
        meta: {
          authority: ['system:files:read'],
          icon: 'lucide:folder-open',
          title: $t('page.navigation.files'),
        },
        name: 'menu-system-files',
        path: '/system/files',
      },
      {
        component: () => import('#/views/system/observability/index.vue'),
        meta: {
          authority: ['system:observability:read'],
          icon: 'lucide:gauge',
          title: $t('page.observability.title'),
        },
        name: 'menu-system-observability',
        path: '/system/observability',
      },
    ],
  },
  {
    meta: {
      authority: [
        'ops:monitor:read',
        'ops:audit:read',
        'ops:tasks:read',
        'ops:data-jobs:read',
      ],
      icon: 'lucide:activity',
      order: 40,
      title: $t('page.navigation.operations'),
    },
    name: 'menu-operations',
    path: '/operations',
    redirect: '/system/monitor',
    children: [
      {
        component: () => import('#/views/system/monitor/index.vue'),
        meta: {
          authority: ['ops:monitor:read'],
          icon: 'lucide:monitor-cog',
          title: $t('page.navigation.monitor'),
        },
        name: 'menu-operations-monitor',
        path: '/system/monitor',
      },
      {
        component: () => import('#/views/system/audit/index.vue'),
        meta: {
          authority: ['ops:audit:read'],
          icon: 'lucide:scroll-text',
          title: $t('page.audit.title'),
        },
        name: 'menu-operations-audit',
        path: '/system/audit',
      },
      {
        component: () => import('#/views/system/tasks/index.vue'),
        meta: {
          authority: ['ops:tasks:read'],
          icon: 'lucide:workflow',
          title: $t('page.navigation.tasks'),
        },
        name: 'menu-operations-tasks',
        path: '/system/tasks',
      },
      {
        component: () => import('#/views/system/import-export/index.vue'),
        meta: {
          authority: ['ops:data-jobs:read'],
          icon: 'lucide:file-spreadsheet',
          title: $t('page.navigation.dataJobs'),
        },
        name: 'menu-operations-data-jobs',
        path: '/system/import-export',
      },
    ],
  },
];

export default routes;
