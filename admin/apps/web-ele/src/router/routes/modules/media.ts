import type { RouteRecordRaw } from 'vue-router';
import { $t } from '#/locales';
const routes: RouteRecordRaw[] = [
  {
    name: 'menu-media',
    path: '/media',
    redirect: '/media/library',
    meta: {
      authority: ['system:files:read', 'media:library:read'],
      icon: 'lucide:images',
      order: 50,
      title: $t('page.navigation.media'),
    },
    children: [
      {
        name: 'menu-media-library',
        path: 'library',
        component: () => import('#/views/system/files/index.vue'),
        meta: {
          authority: ['system:files:read', 'media:library:read'],
          icon: 'lucide:folder-open',
          title: $t('page.navigation.mediaLibrary'),
        },
      },
    ],
  },
];
export default routes;
