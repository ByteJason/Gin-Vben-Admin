import type { RouteRecordStringComponent } from '@vben/types';

import { MENU_ENDPOINT } from '@vben/api-client';

import { requestClient } from '#/api/request';

/**
 * 获取用户所有菜单
 */
export async function getAllMenusApi() {
  return requestClient.get<RouteRecordStringComponent[]>(MENU_ENDPOINT);
}
