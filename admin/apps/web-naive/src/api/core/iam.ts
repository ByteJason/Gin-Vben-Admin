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

export type IAMRoleDataScope = 'all' | 'own' | 'org' | 'custom';

export interface IAMRole {
  active: boolean;
  dataScope?: IAMRoleDataScope;
  id: string;
  name: string;
  userIds?: string[];
}

export interface IAMRoleUsersReplaceInput {
  userIds: string[];
}

export interface IAMRoleCreateInput {
  active?: boolean;
  dataScope?: IAMRoleDataScope;
  id: string;
  name: string;
}

export interface IAMUserCreateInput {
  active?: boolean;
  avatar?: string;
  email?: string;
  nickname?: string;
  orgId?: string;
  password: string;
  phone?: string;
  username: string;
}

export interface IAMUserBatchStatusInput {
  items: Array<{ active: boolean; id: string }>;
}

export interface IAMUserPasswordResetInput {
  password: string;
}

export interface IAMUserLoginEvent {
  action: string;
  actorId?: string;
  createdAt: string;
  details?: Record<string, unknown>;
  id: string;
  outcome: string;
  requestId: string;
  resource: string;
}

export interface IAMUserLoginEventPage {
  items: IAMUserLoginEvent[];
  limit: number;
  offset: number;
  total: number;
}

export interface IAMUserLoginEventParams {
  from?: string;
  limit?: number;
  offset?: number;
  to?: string;
}

export type IAMUserBatchStatusResultStatus =
  | 'active'
  | 'disabled'
  | 'error'
  | 'forbidden'
  | 'invalid'
  | 'not_found';

export interface IAMUserBatchStatusResult {
  code: number;
  id: string;
  status: IAMUserBatchStatusResultStatus;
}

export interface IAMUserBatchStatusResponse {
  results: IAMUserBatchStatusResult[];
}

export interface IAMUserUpdateInput {
  active?: boolean;
  avatar?: string;
  email?: string;
  nickname?: string;
  orgId?: string;
  phone?: string;
  username?: string;
}

const userPath = (id: string) =>
  ADMIN_ENDPOINTS.getIAMUser.replace('{id}', encodeURIComponent(id));
const updateUserPath = (id: string) =>
  ADMIN_ENDPOINTS.updateIAMUser.replace('{id}', encodeURIComponent(id));
const deleteUserPath = (id: string) =>
  ADMIN_ENDPOINTS.deleteIAMUser.replace('{id}', encodeURIComponent(id));
const resetUserPasswordPath = (id: string) =>
  ADMIN_ENDPOINTS.resetIAMUserPassword.replace('{id}', encodeURIComponent(id));
const loginEventsPath = (id: string) =>
  ADMIN_ENDPOINTS.listIAMUserLoginEvents.replace(
    '{id}',
    encodeURIComponent(id),
  );
const roleUsersPath = (id: string) =>
  ADMIN_ENDPOINTS.replaceIAMRoleUsers.replace('{id}', encodeURIComponent(id));

const roleAssignmentLimit = 100;

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

export async function batchUpdateIAMUserStatusApi(
  input: IAMUserBatchStatusInput,
) {
  return requestClient.post<IAMUserBatchStatusResponse>(
    ADMIN_ENDPOINTS.batchUpdateIAMUserStatus,
    input,
  );
}

export async function listIAMRolesApi() {
  return requestClient.get<IAMRole[]>(ADMIN_ENDPOINTS.listIAMRoles);
}

export async function createIAMRoleApi(input: IAMRoleCreateInput) {
  const id = input.id.trim();
  const name = input.name.trim();
  if (!id || !name) {
    throw new Error('role id and name are required');
  }
  const dataScope = input.dataScope ?? 'own';
  if (!['all', 'own', 'org', 'custom'].includes(dataScope)) {
    throw new Error('role data scope is invalid');
  }
  return requestClient.post<IAMRole>(ADMIN_ENDPOINTS.createIAMRole, {
    id,
    name,
    active: input.active ?? true,
    dataScope,
  });
}

export async function replaceIAMRoleUsersApi(
  id: string,
  input: IAMRoleUsersReplaceInput,
) {
  const userIds = Array.from(
    new Set(input.userIds.map((userId) => userId.trim()).filter(Boolean)),
  );
  if (userIds.length > roleAssignmentLimit) {
    throw new Error('role assignment exceeds the bounded member limit');
  }
  return requestClient.request<IAMRole>(roleUsersPath(id), {
    data: { userIds },
    method: 'PUT',
  });
}

export async function getIAMUserApi(id: string) {
  return requestClient.get<IAMUser>(userPath(id));
}

export async function createIAMUserApi(input: IAMUserCreateInput) {
  return requestClient.post<IAMUser>(ADMIN_ENDPOINTS.createIAMUser, input);
}

export async function updateIAMUserApi(id: string, input: IAMUserUpdateInput) {
  return requestClient.request<IAMUser>(updateUserPath(id), {
    data: input,
    method: 'PATCH',
  });
}

export async function deleteIAMUserApi(id: string) {
  return requestClient.delete<void>(deleteUserPath(id));
}

export async function resetIAMUserPasswordApi(
  id: string,
  input: IAMUserPasswordResetInput,
) {
  return requestClient.post<void>(resetUserPasswordPath(id), input);
}

export async function listIAMUserLoginEventsApi(
  id: string,
  params: IAMUserLoginEventParams = {},
) {
  const limit = Math.min(Math.max(params.limit ?? 50, 0), 200);
  const offset = Math.max(params.offset ?? 0, 0);
  return requestClient.get<IAMUserLoginEventPage>(loginEventsPath(id), {
    params: {
      from: params.from,
      to: params.to,
      limit,
      offset,
    },
  });
}
