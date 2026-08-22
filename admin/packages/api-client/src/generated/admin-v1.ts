// Generated from contracts/openapi/admin-v1.yaml; DO NOT EDIT.
// CONTRACT_SHA256=9bf7fa88e5e37c141ae2a12173711473667fa3de0f1204449f2f170d56950b02

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
  createIAMMenu: '/admin/v1/iam/menus',
  updateIAMMenu: '/admin/v1/iam/menus/{id}',
  getIAMMenu: '/admin/v1/iam/menus/{id}',
  deleteIAMMenu: '/admin/v1/iam/menus/{id}',
  reorderIAMMenus: '/admin/v1/iam/menus/reorder',
  listIAMComponents: '/admin/v1/iam/components',
  listVisibleMenus: '/admin/v1/menu/all',
  listIAMPermissions: '/admin/v1/iam/permissions',
  listIAMPolicies: '/admin/v1/iam/policies',
  createIAMPolicy: '/admin/v1/iam/policies',
  listIAMDataScopes: '/admin/v1/iam/data-scopes',
  createIAMDataScope: '/admin/v1/iam/data-scopes',
  resetIAMUserPassword: '/admin/v1/iam/users/{id}/reset-password',
  replaceIAMRoleUsers: '/admin/v1/iam/roles/{id}/users',
  replaceIAMRolePermissions: '/admin/v1/iam/roles/{id}/permissions',
  replaceIAMRoleDataScopes: '/admin/v1/iam/roles/{id}/data-scopes',
  listSettingDefinitions: '/admin/v1/settings',
  getSetting: '/admin/v1/settings/{key}',
  updateSetting: '/admin/v1/settings/{key}',
  listSettingHistory: '/admin/v1/settings/{key}/history',
  rollbackSetting: '/admin/v1/settings/{key}/rollback',
  testSettingConnection: '/admin/v1/settings/{key}/test',
  queryAuditEvents: '/admin/v1/audit/events',
  exportAuditEvents: '/admin/v1/audit/events/export',
  auditRetentionDryRun: '/admin/v1/audit/retention/dry-run',
  listFiles: '/admin/v1/files',
  uploadFile: '/admin/v1/files/upload',
  getFile: '/admin/v1/files/{id}',
  deleteFile: '/admin/v1/files/{id}',
  downloadFile: '/admin/v1/files/{id}/download',
  previewFile: '/admin/v1/files/{id}/preview',
  signFileURL: '/admin/v1/files/{id}/signed-url',
  fileCleanupDryRun: '/admin/v1/files/cleanup/dry-run',
  listDictionaries: '/admin/v1/dictionaries',
  createDictionary: '/admin/v1/dictionaries',
  updateDictionary: '/admin/v1/dictionaries/types/{code}',
  deleteDictionary: '/admin/v1/dictionaries/types/{code}',
  listDictionaryItems: '/admin/v1/dictionaries/{type}/items',
  createDictionaryItem: '/admin/v1/dictionaries/{type}/items',
  importDictionaryItems: '/admin/v1/dictionaries/{type}/items/import',
  updateDictionaryItem: '/admin/v1/dictionaries/{type}/items/{id}',
  deleteDictionaryItem: '/admin/v1/dictionaries/{type}/items/{id}',
  listTasks: '/admin/v1/tasks',
  createTask: '/admin/v1/tasks',
  updateTask: '/admin/v1/tasks/{id}',
  deleteTask: '/admin/v1/tasks/{id}',
  runTask: '/admin/v1/tasks/{id}/run',
  listTaskRuns: '/admin/v1/tasks/{id}/runs',
  listTaskRunLogs: '/admin/v1/tasks/{id}/runs/{runId}/logs',
  cancelTaskRun: '/admin/v1/tasks/{id}/runs/{runId}/cancel',
  retryTaskRun: '/admin/v1/tasks/{id}/runs/{runId}/retry',
  downloadImportTemplate: '/admin/v1/import-export/templates/{format}',
  previewImport: '/admin/v1/import-export/imports/preview',
  commitImport: '/admin/v1/import-export/imports/commit',
  startExport: '/admin/v1/import-export/exports',
  listImportExportJobs: '/admin/v1/import-export/jobs',
  getImportExportJob: '/admin/v1/import-export/jobs/{id}',
  listImportErrors: '/admin/v1/import-export/jobs/{id}/errors',
  downloadExport: '/admin/v1/import-export/jobs/{id}/download',
  cancelImportExportJob: '/admin/v1/import-export/jobs/{id}/cancel',
  retryImportExportJob: '/admin/v1/import-export/jobs/{id}/retry',
  listSMTPAccounts: '/admin/v1/mail/accounts',
  createSMTPAccount: '/admin/v1/mail/accounts',
  updateSMTPAccount: '/admin/v1/mail/accounts/{id}',
  deleteSMTPAccount: '/admin/v1/mail/accounts/{id}',
  testSMTPAccount: '/admin/v1/mail/accounts/{id}/test',
  listEmailMessages: '/admin/v1/mail/messages',
  sendEmailMessage: '/admin/v1/mail/messages',
  getEmailMessage: '/admin/v1/mail/messages/{id}',
  getMonitorOverview: '/admin/v1/ops/monitor',
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

export interface SMTPAccount {
  id: string;
  name: string;
  enabled: boolean;
  host: string;
  port: number;
  username: string;
  weight: number;
  fromEmail: string;
  fromName?: string;
  implicitTls: boolean;
  passwordConfigured: boolean;
  tenantId?: string;
  orgId?: string;
  createdAt: string;
  updatedAt: string;
}

export interface SMTPAccountInput {
  name: string;
  enabled?: boolean;
  host: string;
  port: number;
  username?: string;
  password?: string;
  weight?: number;
  fromEmail: string;
  fromName?: string;
  implicitTls?: boolean;
}

export interface EmailRecipient { address: string; kind: 'to' | 'cc' | 'bcc'; }
export interface EmailMessage {
  id: string;
  subject: string;
  recipients: EmailRecipient[];
  body?: string;
  bodyDigest: string;
  status: 'pending' | 'sending' | 'retrying' | 'sent' | 'failed';
  attemptCount: number;
  smtpAccountId?: string;
  senderId?: string;
  providerMessageId?: string;
  lastErrorCode?: string;
  sentAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface MonitorMetric {
  status: 'ok' | 'degraded' | 'unavailable';
  cores?: number;
  load1?: number;
  usedBytes?: number;
  totalBytes?: number;
  utilization?: number;
  latencyMs?: number;
  poolOpen?: number;
  poolIdle?: number;
  poolMax?: number;
  keyspace?: number;
  message?: string;
}
export interface MonitorOverview {
  scope: 'process' | 'container';
  uptimeSeconds: number;
  version?: string;
  cpu: MonitorMetric;
  memory: MonitorMetric;
  disk: MonitorMetric;
  database: MonitorMetric;
  redis: MonitorMetric;
  collectedAt: string;
}

export interface TaskDefinition {
  id: string;
  tenantId: string;
  orgId?: string;
  name: string;
  type: 'manual' | 'http' | 'webhook';
  payloadSchema: Record<string, unknown>;
  cron?: string;
  timezone: string;
  enabled: boolean;
  concurrency: number;
  concurrencyPolicy: 'allow' | 'forbid' | 'replace';
  timeoutSeconds: number;
  maxAttempts: number;
  idempotencyKey?: string;
  deletedAt?: string;
  createdAt: string;
  updatedAt: string;
}
export interface TaskDefinitionInput {
  name: string;
  type: 'manual' | 'http' | 'webhook';
  payloadSchema: Record<string, unknown>;
  cron?: string;
  timezone?: string;
  enabled?: boolean;
  concurrency?: number;
  concurrencyPolicy?: 'allow' | 'forbid' | 'replace';
  timeoutSeconds?: number;
  maxAttempts?: number;
  idempotencyKey?: string;
}
export interface TaskRun {
  id: string;
  taskId: string;
  tenantId: string;
  orgId?: string;
  queueTaskId?: string;
  idempotencyKey: string;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'dead_letter' | 'cancelled';
  payloadDigest: string;
  attemptCount: number;
  maxAttempts: number;
  lastErrorCode?: string;
  startedAt?: string;
  finishedAt?: string;
  createdAt: string;
  updatedAt: string;
}
export interface TaskRunLog {
  id: string;
  runId: string;
  attempt: number;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'dead_letter' | 'cancelled';
  errorCode?: string;
  message?: string;
  createdAt: string;
  updatedAt: string;
}

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
