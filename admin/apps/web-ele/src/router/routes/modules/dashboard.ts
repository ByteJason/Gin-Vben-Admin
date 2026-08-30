import type { RouteRecordRaw } from 'vue-router';
import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      authority: ['dashboard:overview:read'],
      icon: 'lucide:layout-dashboard',
      order: -1,
      title: $t('page.navigation.dashboard'),
    },
    name: 'menu-dashboard',
    path: '/dashboard',
    redirect: '/dashboard/analytics',
    children: [
      {
        name: 'menu-dashboard-analytics',
        path: 'analytics',
        component: () => import('#/views/dashboard/analytics/index.vue'),
        meta: {
          authority: ['dashboard:overview:read'],
          affixTab: true,
          icon: 'lucide:area-chart',
          title: $t('page.dashboard.analytics'),
        },
      },
    ],
  },
  ...[
    ['/analytics', 'LegacyAnalytics'],
    ['/workspace', 'LegacyWorkspace'],
    ['/dashboard/workspace', 'LegacyDashboardWorkspace'],
  ].map(
    ([path, name]) =>
      ({
        name,
        path,
        redirect: '/dashboard/analytics',
        meta: {
          hideInBreadcrumb: true,
          hideInMenu: true,
          hideInTab: true,
          title: $t('page.dashboard.analytics'),
        },
      }) as RouteRecordRaw,
  ),
];
export default routes;
