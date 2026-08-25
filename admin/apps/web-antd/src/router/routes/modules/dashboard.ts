import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      authority: ['dashboard:overview:read'],
      icon: 'lucide:layout-dashboard',
      order: -1,
      title: $t('page.dashboard.title'),
    },
    name: 'menu-overview',
    path: '/dashboard',
    redirect: '/dashboard/analytics',
    children: [
      {
        name: 'menu-overview-runtime',
        path: 'analytics',
        component: () => import('#/views/dashboard/analytics/index.vue'),
        meta: {
          authority: ['dashboard:overview:read'],
          affixTab: true,
          icon: 'lucide:area-chart',
          title: $t('page.dashboard.analytics'),
        },
      },
      {
        name: 'Workspace',
        path: 'workspace',
        redirect: '/dashboard/analytics',
        meta: {
          hideInBreadcrumb: true,
          hideInMenu: true,
          hideInTab: true,
          title: $t('page.dashboard.workspace'),
        },
      },
    ],
  },
  {
    name: 'LegacyAnalytics',
    path: '/analytics',
    redirect: '/dashboard/analytics',
    meta: {
      hideInBreadcrumb: true,
      hideInMenu: true,
      hideInTab: true,
      title: $t('page.dashboard.analytics'),
    },
  },
  {
    name: 'LegacyWorkspace',
    path: '/workspace',
    redirect: '/dashboard/analytics',
    meta: {
      hideInBreadcrumb: true,
      hideInMenu: true,
      hideInTab: true,
      title: $t('page.dashboard.workspace'),
    },
  },
];

export default routes;
