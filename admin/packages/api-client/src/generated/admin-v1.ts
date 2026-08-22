// Generated from contracts/openapi/admin-v1.yaml; DO NOT EDIT.
// CONTRACT_SHA256=b3ab4fb23dc4c2244436c1f059fa351bd8975b3adc235a80e233923b46d118d9

export const ADMIN_API_PREFIX = '/admin/v1' as const;

export const ADMIN_ENDPOINTS = {
  adminPing: '/admin/v1/ping',
  issueAdminAuthCaptcha: '/admin/v1/auth/captcha',
  adminAuthLogin: '/admin/v1/auth/login',
  adminAuthRefresh: '/admin/v1/auth/refresh',
  adminAuthLogout: '/admin/v1/auth/logout',
  adminAuthRegister: '/admin/v1/auth/register',
  requestAdminPasswordReset: '/admin/v1/auth/password/reset/request',
  resetAdminPassword: '/admin/v1/auth/password/reset',
  listAdminAuthSessions: '/admin/v1/auth/sessions',
  revokeAdminAuthSession: '/admin/v1/auth/sessions/{id}',
  getCurrentAdminUser: '/admin/v1/iam/me',
  listIAMUsers: '/admin/v1/iam/users',
  createIAMUser: '/admin/v1/iam/users',
  batchUpdateIAMUserStatus: '/admin/v1/iam/users/batch-status',
  listIAMUserLoginEvents: '/admin/v1/iam/users/{id}/login-events',
  getIAMUser: '/admin/v1/iam/users/{id}',
  updateIAMUser: '/admin/v1/iam/users/{id}',
  deleteIAMUser: '/admin/v1/iam/users/{id}',
  listIAMRoles: '/admin/v1/iam/roles',
  createIAMRole: '/admin/v1/iam/roles',
  listIAMMenus: '/admin/v1/iam/menus',
  listVisibleMenus: '/admin/v1/menu/all',
  listIAMPermissions: '/admin/v1/iam/permissions',
  listIAMPolicies: '/admin/v1/iam/policies',
  createIAMPolicy: '/admin/v1/iam/policies',
  listIAMDataScopes: '/admin/v1/iam/data-scopes',
  createIAMDataScope: '/admin/v1/iam/data-scopes',
  resetIAMUserPassword: '/admin/v1/iam/users/{id}/reset-password',
  replaceIAMRoleUsers: '/admin/v1/iam/roles/{id}/users',
  replaceIAMRolePermissions: '/admin/v1/iam/roles/{id}/permissions',
  listSettingDefinitions: '/admin/v1/settings',
  getSetting: '/admin/v1/settings/{key}',
  updateSetting: '/admin/v1/settings/{key}',
  listSettingHistory: '/admin/v1/settings/{key}/history',
  rollbackSetting: '/admin/v1/settings/{key}/rollback',
  queryAuditEvents: '/admin/v1/audit/events',
} as const;

export const AUTH_API_PREFIX = '/admin/v1/auth' as const;
export const AUTH_ENDPOINTS = {
  captcha: ADMIN_ENDPOINTS.issueAdminAuthCaptcha,
  login: ADMIN_ENDPOINTS.adminAuthLogin,
  logout: ADMIN_ENDPOINTS.adminAuthLogout,
  passwordReset: ADMIN_ENDPOINTS.resetAdminPassword,
  passwordResetRequest: ADMIN_ENDPOINTS.requestAdminPasswordReset,
  register: ADMIN_ENDPOINTS.adminAuthRegister,
  refresh: ADMIN_ENDPOINTS.adminAuthRefresh,
  sessions: ADMIN_ENDPOINTS.listAdminAuthSessions,
} as const;

export const MENU_ENDPOINT = ADMIN_ENDPOINTS.listVisibleMenus;
export const CURRENT_USER_ENDPOINT = ADMIN_ENDPOINTS.getCurrentAdminUser;

export namespace AuthApi {
  export interface LoginParams {
    captcha?: string;
    captchaId?: string;
    identifier?: string;
    identifierType?: "username" | "email";
    password: string;
    username?: string;
  }

  export interface RegisterParams {
    password: string;
    username: string;
  }

  export interface PasswordResetRequestParams {
    password?: string;
    token?: string;
    username?: string;
  }

  export interface SessionInfo {
    createdAt: string;
    deviceId: string;
    deviceName: string;
    expiresAt: string;
    id: string;
    ipAddress: string;
    lastSeenAt: string;
    revoked: boolean;
    userAgent: string;
  }

  export interface LoginResult {
    accessToken: string;
    expiresIn: number;
    tokenType: 'Bearer';
  }

  export type RefreshTokenResult = LoginResult;

  export interface ApiEnvelope<T> {
    code: number;
    data: T;
    message: string;
    meta?: { requestId?: string };
    traceId?: string;
  }

  export interface WireTokenData {
    accessToken?: string;
    access_token?: string;
    expiresIn?: number;
    expires_in?: number;
    tokenType?: 'Bearer' | string;
    token_type?: 'Bearer' | string;
  }
}
