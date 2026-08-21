import type { UserInfo } from '@vben/types';

import { CURRENT_USER_ENDPOINT } from '@vben/api-client';

import { requestClient } from '#/api/request';

/**
 * 获取用户信息
 */
export async function getUserInfoApi() {
  return requestClient.get<UserInfo>(CURRENT_USER_ENDPOINT);
}
