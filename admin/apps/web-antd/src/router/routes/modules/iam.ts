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
      {
        component: () => import('#/views/iam/roles/index.vue'),
        meta: {
          icon: 'lucide:shield-check',
          title: $t('page.iam.roles'),
        },
        name: 'IAMRoles',
        path: 'roles',
      },
      {
        component: () => import('#/views/iam/menus/index.vue'),
        meta: {
          icon: 'lucide:menu',
          title: $t('page.iam.menus'),
        },
        name: 'IAMMenus',
        path: 'menus',
      },
      {
        component: () => import('#/views/iam/permissions/index.vue'),
        meta: {
          icon: 'lucide:key-round',
          title: $t('page.iam.permissions'),
        },
        name: 'IAMPermissions',
        path: 'permissions',
      },
      {
        component: () => import('#/views/iam/policies/index.vue'),
        meta: {
          icon: 'lucide:shield-alert',
          title: $t('page.iam.policies'),
        },
        name: 'IAMPolicies',
        path: 'policies',
      },
      {
        component: () => import('#/views/iam/data-scopes/index.vue'),
        meta: {
          icon: 'lucide:database',
          title: $t('page.iam.dataScopes'),
        },
        name: 'IAMDataScopes',
        path: 'data-scopes',
      },
    ],
  },
];

export default routes;
