import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:activity',
      order: 30,
      title: $t('page.observability.group'),
    },
    name: 'SystemObservability',
    path: '/system',
    children: [
      {
        component: () => import('#/views/system/observability/index.vue'),
        meta: {
          icon: 'lucide:gauge',
          title: $t('page.observability.title'),
        },
        name: 'ObservabilitySettings',
        path: 'observability',
      },
      {
        component: () => import('#/views/system/settings/index.vue'),
        meta: {
          icon: 'lucide:sliders-horizontal',
          title: $t('page.settings.title'),
        },
        name: 'SystemSettings',
        path: 'settings',
      },
      {
        component: () => import('#/views/system/audit/index.vue'),
        meta: {
          icon: 'lucide:scroll-text',
          title: $t('page.audit.title'),
        },
        name: 'SystemAudit',
        path: 'audit',
      },
      {
        component: () => import('#/views/system/files/index.vue'),
        meta: {
          icon: 'lucide:folder-open',
          title: $t('page.files.title'),
        },
        name: 'SystemFiles',
        path: 'files',
      },
      {
        component: () => import('#/views/system/mail/index.vue'),
        meta: { icon: 'lucide:mail', title: 'SMTP Mail' },
        name: 'SystemMail',
        path: 'mail',
      },
      {
        component: () => import('#/views/system/monitor/index.vue'),
        meta: { icon: 'lucide:activity', title: 'Operations Monitor' },
        name: 'SystemMonitor',
        path: 'monitor',
      },
      {
        component: () => import('#/views/system/dictionary/index.vue'),
        meta: {
          icon: 'lucide:book-open',
          title: $t('page.dictionary.title'),
        },
        name: 'SystemDictionary',
        path: 'dictionary',
      },
      {
        component: () => import('#/views/system/tasks/index.vue'),
        meta: { icon: 'lucide:workflow', title: $t('page.tasks.title') },
        name: 'SystemTasks',
        path: 'tasks',
      },
    ],
  },
];

export default routes;
