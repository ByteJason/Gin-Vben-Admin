import type { RouteRecordRaw } from 'vue-router';
import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    component: () => import('#/views/dashboard/analytics/index.vue'),
    meta: {
      authority: ['dashboard:overview:read'],
      affixTab: true,
      icon: 'lucide:layout-dashboard',
      order: -1,
      title: $t('page.navigation.dashboard'),
    },
    name: 'menu-overview',
    path: '/dashboard',
  },
  ...[
    ['/dashboard/analytics', 'LegacyDashboardAnalytics'],
    ['/analytics', 'LegacyAnalytics'],
    ['/workspace', 'LegacyWorkspace'],
    ['/dashboard/workspace', 'LegacyDashboardWorkspace'],
  ].map(
    ([path, name]) =>
      ({
        name,
        path,
        redirect: '/dashboard',
        meta: {
          hideInBreadcrumb: true,
          hideInMenu: true,
          hideInTab: true,
          title: $t('page.navigation.dashboard'),
        },
      }) as RouteRecordRaw,
  ),
];
export default routes;
