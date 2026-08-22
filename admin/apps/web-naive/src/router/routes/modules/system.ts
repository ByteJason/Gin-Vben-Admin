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
    ],
  },
];

export default routes;
