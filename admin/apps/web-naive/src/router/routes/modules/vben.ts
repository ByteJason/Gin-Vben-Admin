import type { RouteRecordRaw } from 'vue-router';
import { $t } from '#/locales';

// The profile page remains a development-only compatibility entry; upstream
// Vben links and component examples are intentionally not part of production routes.
const routes: RouteRecordRaw[] = import.meta.env.DEV
  ? [
      {
        name: 'Profile',
        path: '/profile',
        component: () => import('#/views/_core/profile/index.vue'),
        meta: { hideInMenu: true, title: $t('page.auth.profile') },
      },
    ]
  : [];
export default routes;
