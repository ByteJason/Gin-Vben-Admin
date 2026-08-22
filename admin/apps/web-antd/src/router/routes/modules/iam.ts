import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      icon: 'lucide:users',
      order: 20,
      title: $t('page.iam.group'),
    },
    name: 'IAM',
    path: '/iam',
    children: [
      {
        component: () => import('#/views/iam/users/index.vue'),
        meta: {
          icon: 'lucide:user-round-search',
          title: $t('page.iam.users'),
        },
        name: 'IAMUsers',
        path: 'users',
      },
    ],
  },
];

export default routes;
