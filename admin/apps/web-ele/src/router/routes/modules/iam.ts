import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    meta: {
      authority: [
        'iam:users:read',
        'iam:roles:read',
        'iam:menus:read',
        'iam:permissions:read',
        'iam:policies:read',
        'iam:data-scopes:read',
      ],
      icon: 'lucide:users',
      order: 20,
      title: $t('page.iam.group'),
    },
    name: 'menu-identity',
    path: '/iam',
    children: [
      {
        component: () => import('#/views/iam/users/index.vue'),
        meta: {
          authority: ['iam:users:read'],
          icon: 'lucide:user-round-search',
          title: $t('page.iam.users'),
        },
        name: 'menu-identity-users',
        path: 'users',
      },
      {
        component: () => import('#/views/iam/roles/index.vue'),
        meta: {
          authority: ['iam:roles:read'],
          icon: 'lucide:shield-check',
          title: $t('page.iam.roles'),
        },
        name: 'menu-identity-roles',
        path: 'roles',
      },
      {
        component: () => import('#/views/iam/menus/index.vue'),
        meta: {
          authority: ['iam:menus:read'],
          icon: 'lucide:menu',
          title: $t('page.iam.menus'),
        },
        name: 'menu-identity-menus',
        path: 'menus',
      },
      {
        component: () => import('#/views/iam/permissions/index.vue'),
        meta: {
          authority: ['iam:permissions:read'],
          icon: 'lucide:key-round',
          title: $t('page.iam.permissions'),
        },
        name: 'menu-identity-permissions',
        path: 'permissions',
      },
      {
        component: () => import('#/views/iam/policies/index.vue'),
        meta: {
          authority: ['iam:policies:read'],
          hideInMenu: true,
          icon: 'lucide:shield-alert',
          title: $t('page.iam.policies'),
        },
        name: 'IAMPolicies',
        path: 'policies',
      },
      {
        component: () => import('#/views/iam/data-scopes/index.vue'),
        meta: {
          authority: ['iam:data-scopes:read'],
          hideInMenu: true,
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
