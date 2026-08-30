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
      ],
      icon: 'lucide:users',
      order: 20,
      title: $t('page.navigation.iam'),
    },
    name: 'menu-identity',
    path: '/iam',
    children: [
      {
        component: () => import('#/views/iam/roles/index.vue'),
        name: 'menu-identity-roles',
        path: 'roles',
        meta: {
          authority: ['iam:roles:read'],
          icon: 'lucide:shield-check',
          title: $t('page.iam.roles'),
        },
      },
      {
        component: () => import('#/views/iam/menus/index.vue'),
        name: 'menu-identity-menus',
        path: 'menus',
        meta: {
          authority: ['iam:menus:read'],
          icon: 'lucide:menu',
          title: $t('page.iam.menus'),
        },
      },
      {
        component: () => import('#/views/iam/permissions/index.vue'),
        name: 'menu-identity-permissions',
        path: 'permissions',
        meta: {
          authority: ['iam:permissions:read'],
          icon: 'lucide:key-round',
          title: $t('page.iam.permissions'),
        },
      },
      {
        component: () => import('#/views/iam/users/index.vue'),
        name: 'menu-identity-users',
        path: 'users',
        meta: {
          authority: ['iam:users:read'],
          icon: 'lucide:user-round-search',
          title: $t('page.iam.users'),
        },
      },
      {
        component: () => import('#/views/iam/policies/index.vue'),
        name: 'IAMPolicies',
        path: 'policies',
        meta: {
          authority: ['iam:policies:read'],
          hideInMenu: true,
          hideInTab: true,
          title: $t('page.iam.policies'),
        },
      },
      {
        component: () => import('#/views/iam/data-scopes/index.vue'),
        name: 'IAMDataScopes',
        path: 'data-scopes',
        meta: {
          authority: ['iam:data-scopes:read'],
          hideInMenu: true,
          hideInTab: true,
          title: $t('page.iam.dataScopes'),
        },
      },
    ],
  },
];
export default routes;
