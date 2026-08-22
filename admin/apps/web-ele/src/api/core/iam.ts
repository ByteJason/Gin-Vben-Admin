import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type IAMUserStatus = 'active' | 'all' | 'disabled';

export interface IAMUser {
  active: boolean;
  avatar?: string;
  displayName?: string;
  email?: string;
  id: string;
  lastLoginAt?: string;
  lastLoginIp?: string;
  nickname?: string;
  orgId?: string;
  phone?: string;
  roleIds: string[];
  status?: 'active' | 'disabled';
  tenantId?: string;
  username: string;
}

export interface IAMUserPage {
  items: IAMUser[];
  page: number;
  pageSize: number;
  total: number;
}

export interface IAMUserListParams {
  keyword?: string;
  orgId?: string;
  page?: number;
  pageSize?: number;
  roleId?: string;
  sort?: string;
  status?: IAMUserStatus;
}

export interface IAMRole {
  active: boolean;
  dataScope?: string;
  id: string;
  name: string;
  userIds?: string[];
}

export async function listIAMUsersApi(params: IAMUserListParams = {}) {
  return requestClient.get<IAMUserPage>(ADMIN_ENDPOINTS.listIAMUsers, {
    params: {
      page: params.page ?? 1,
      pageSize: params.pageSize ?? 20,
      keyword: params.keyword?.trim() || undefined,
      status: params.status ?? 'all',
      roleId: params.roleId?.trim() || undefined,
      orgId: params.orgId?.trim() || undefined,
      sort: params.sort ?? 'username',
    },
  });
}

export async function listIAMRolesApi() {
  return requestClient.get<IAMRole[]>(ADMIN_ENDPOINTS.listIAMRoles);
}
