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
  permissionIds?: string[];
  userIds?: string[];
}

export interface IAMRoleUsersReplaceInput {
  userIds: string[];
}

export interface IAMRolePermissionsReplaceInput {
  permissionIds: string[];
}

export interface IAMRoleDataScopeBinding {
  ids: string[];
  resource: string;
  scope: IAMRoleDataScope;
}

export interface IAMRoleDataScopesReplaceInput {
  scopes: IAMRoleDataScopeBinding[];
}

export interface IAMRoleCreateInput {
  active?: boolean;
  dataScope?: IAMRoleDataScope;
  id: string;
  name: string;
}

export interface IAMMenu {
  active: boolean;
  id: string;
  name: string;
  parentId?: string;
  path: string;
  visible: boolean;
}

export interface IAMPermission {
  active: boolean;
  id: string;
  method: string;
  name: string;
  path: string;
}

export type IAMPolicyEffect = 'allow' | 'deny';

export interface IAMPolicy {
  domain?: string;
  effect: IAMPolicyEffect;
  method: string;
  path: string;
  roleId?: string;
  subject?: string;
}

export type IAMDataScopeType = 'all' | 'own' | 'org' | 'custom';

export interface IAMDataScope {
  domain?: string;
  ids: string[];
  resource: string;
  roleId?: string;
  scope: IAMDataScopeType;
  subject?: string;
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
const rolePermissionsPath = (id: string) =>
  ADMIN_ENDPOINTS.replaceIAMRolePermissions.replace(
    '{id}',
    encodeURIComponent(id),
  );
const roleDataScopesPath = (id: string) =>
  ADMIN_ENDPOINTS.replaceIAMRoleDataScopes.replace(
    '{id}',
    encodeURIComponent(id),
  );

const roleAssignmentLimit = 100;
const rolePermissionLimit = 200;
export const roleDataScopeBindingLimit = 50;
export const roleDataScopeIdsLimit = 200;

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

export async function listIAMMenusApi() {
  return requestClient.get<IAMMenu[]>(ADMIN_ENDPOINTS.listIAMMenus);
}

export async function listIAMPermissionsApi() {
  return requestClient.get<IAMPermission[]>(ADMIN_ENDPOINTS.listIAMPermissions);
}

export async function listIAMPoliciesApi() {
  return requestClient.get<IAMPolicy[]>(ADMIN_ENDPOINTS.listIAMPolicies);
}

export async function listIAMDataScopesApi() {
  return requestClient.get<IAMDataScope[]>(ADMIN_ENDPOINTS.listIAMDataScopes);
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

export async function replaceIAMRolePermissionsApi(
  id: string,
  input: IAMRolePermissionsReplaceInput,
) {
  const permissionIds = Array.from(
    new Set(
      input.permissionIds
        .map((permissionId) => permissionId.trim())
        .filter(Boolean),
    ),
  );
  if (permissionIds.length > rolePermissionLimit) {
    throw new Error('role permissions exceed the bounded relation limit');
  }
  return requestClient.request<IAMRole>(rolePermissionsPath(id), {
    data: { permissionIds },
    method: 'PUT',
  });
}

export async function replaceIAMRoleDataScopesApi(
  id: string,
  input: IAMRoleDataScopesReplaceInput,
) {
  const scopes = input.scopes.map((binding) => {
    const resource = binding.resource.trim();
    if (!resource || resource.length > 191) {
      throw new Error('data-scope resource is invalid');
    }
    if (!['all', 'own', 'org', 'custom'].includes(binding.scope)) {
      throw new Error('data-scope scope is invalid');
    }
    const ids = Array.from(
      new Set(binding.ids.map((value) => value.trim()).filter(Boolean)),
    );
    if (
      ids.length > roleDataScopeIdsLimit ||
      ids.some((value) => value.length > 128)
    ) {
      throw new Error('data-scope ids exceed the bounded relation limit');
    }
    return { ids, resource, scope: binding.scope };
  });
  if (scopes.length > roleDataScopeBindingLimit) {
    throw new Error('data-scope bindings exceed the bounded relation limit');
  }
  if (
    new Set(scopes.map((binding) => binding.resource)).size !== scopes.length
  ) {
    throw new Error('data-scope resources must be unique');
  }
  return requestClient.request<IAMRole>(roleDataScopesPath(id), {
    data: { scopes },
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
