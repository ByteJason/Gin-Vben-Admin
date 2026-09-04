// Generated from contracts/openapi/admin-v1.yaml; DO NOT EDIT.
// CONTRACT_SHA256=5a3c7428b2c612db5c9fabd206a298058c9a396023244d6d8e12adcfcca7faa4

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
  getAdminAuthAccessCodes: '/admin/v1/auth/codes',
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
  listSettingModules: '/admin/v1/settings/modules',
  getSettingModule: '/admin/v1/settings/modules/{module}',
  updateSettingModule: '/admin/v1/settings/modules/{module}',
  validateSettingModule: '/admin/v1/settings/modules/{module}/validate',
  resetSettingModule: '/admin/v1/settings/modules/{module}/reset',
  clearSettingModuleCredentials: '/admin/v1/settings/modules/{module}/clear-credentials',
  getSetting: '/admin/v1/settings/{key}',
  updateSetting: '/admin/v1/settings/{key}',
  listSettingHistory: '/admin/v1/settings/{key}/history',
  rollbackSetting: '/admin/v1/settings/{key}/rollback',
  testSettingConnection: '/admin/v1/settings/{key}/test',
  getObservabilitySetting: '/admin/v1/observability/settings/{key}',
  updateObservabilitySetting: '/admin/v1/observability/settings/{key}',
  queryAuditEvents: '/admin/v1/audit/events',
  exportAuditEvents: '/admin/v1/audit/events/export',
  auditRetentionDryRun: '/admin/v1/audit/retention/dry-run',
  queryOperationHistory: '/admin/v1/ops/operation-history',
  queryLoginLogs: '/admin/v1/ops/login-logs',
  listFiles: '/admin/v1/files',
  uploadFile: '/admin/v1/files/upload',
  listFileCategories: '/admin/v1/files/categories',
  createFileCategory: '/admin/v1/files/categories',
  updateFileCategory: '/admin/v1/files/categories/{id}',
  patchFileCategory: '/admin/v1/files/categories/{id}',
  deleteFileCategory: '/admin/v1/files/categories/{id}',
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
  listNotificationCallers: '/admin/v1/notification/callers',
  createNotificationCaller: '/admin/v1/notification/callers',
  getNotificationCaller: '/admin/v1/notification/callers/{id}',
  updateNotificationCaller: '/admin/v1/notification/callers/{id}',
  replaceNotificationCaller: '/admin/v1/notification/callers/{id}',
  deleteNotificationCaller: '/admin/v1/notification/callers/{id}',
  listNotificationAccounts: '/admin/v1/notification/accounts',
  createNotificationAccount: '/admin/v1/notification/accounts',
  updateNotificationAccount: '/admin/v1/notification/accounts/{id}',
  replaceNotificationAccount: '/admin/v1/notification/accounts/{id}',
  deleteNotificationAccount: '/admin/v1/notification/accounts/{id}',
  listNotificationTemplates: '/admin/v1/notification/templates',
  createNotificationTemplate: '/admin/v1/notification/templates',
  updateNotificationTemplate: '/admin/v1/notification/templates/{id}',
  replaceNotificationTemplate: '/admin/v1/notification/templates/{id}',
  deleteNotificationTemplate: '/admin/v1/notification/templates/{id}',
  publishNotificationTemplate: '/admin/v1/notification/templates/{id}/publish',
  testNotificationTemplate: '/admin/v1/notification/templates/{id}/test',
  listVerificationPolicies: '/admin/v1/notification/verification-policies',
  updateVerificationPolicy: '/admin/v1/notification/verification-policies/{policy_key}',
  replaceVerificationPolicy: '/admin/v1/notification/verification-policies/{policy_key}',
  issueVerificationChallenge: '/admin/v1/notification/verification/challenges',
  getVerificationChallenge: '/admin/v1/notification/verification/challenges/{id}',
  verifyVerificationChallenge: '/admin/v1/notification/verification/challenges/{id}/verify',
  listMediaLibrary: '/admin/v1/media/library',
  uploadMediaResource: '/admin/v1/media/library',
  updateMediaResource: '/admin/v1/media/library/{id}',
  replaceMediaResource: '/admin/v1/media/library/{id}',
  deleteMediaResource: '/admin/v1/media/library/{id}',
  listMediaResourceUsage: '/admin/v1/media/library/{id}/usage',
  listMediaCategories: '/admin/v1/media/categories',
  createMediaCategory: '/admin/v1/media/categories',
  updateMediaCategory: '/admin/v1/media/categories/{id}',
  deleteMediaCategory: '/admin/v1/media/categories/{id}',
  listMediaUsages: '/admin/v1/media/usages',
  attachMediaUsage: '/admin/v1/media/usages/{id}',
  detachMediaUsage: '/admin/v1/media/usages/{id}',
  getMediaResource: '/admin/v1/media/resources/{id}',
  openMediaResource: '/admin/v1/media/resources/{id}/open',
  signMediaResourceURL: '/admin/v1/media/resources/{id}/signed-url',
  createMediaResourceSignedURL: '/admin/v1/media/resources/{id}/signed-url',
  getDashboardSummary: '/admin/v1/dashboard/summary',
  getDashboardOverview: '/admin/v1/dashboard/overview',
  getMonitorOverview: '/admin/v1/ops/monitor',
  getServerStatus: '/admin/v1/ops/server-status',
} as const;

export const AUTH_API_PREFIX = '/admin/v1/auth' as const;
export const AUTH_ENDPOINTS = {
  captcha: ADMIN_ENDPOINTS.issueAdminAuthCaptcha,
  codes: ADMIN_ENDPOINTS.getAdminAuthAccessCodes,
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

export interface FileCategory {
  id: string;
  name: string;
  parentId?: string;
  tenantId?: string;
  orgId?: string;
  createdAt: string;
  updatedAt: string;
}
export interface FileCategoryInput { name: string; parentId?: string; }
export interface FileObject {
  id: string;
  key: string;
  name: string;
  mime: string;
  size: number;
  ownerId: string;
  tenantId: string;
  orgId: string;
  acl: 'private' | 'public-read';
  status?: 'pending' | 'ready' | 'failed' | 'deleting' | 'deleted' | 'damaged';
  categoryId?: string;
  createdAt: string;
  sha256: string;
  extension?: string;
  etag?: string;
  scanStatus?: string;
  failureReason?: string;
}
export interface FilePage { items: FileObject[]; total: number; limit: number; offset: number; }

export type MediaURLPurpose = 'preview' | 'download';
export interface MediaResource {
  id: string;
  name: string;
  mime: string;
  size: number;
  sha256: string;
  categoryId?: string;
  scopeType: 'system' | 'tenant' | 'org';
  acl: 'private' | 'public-read';
  status: 'pending' | 'ready' | 'failed' | 'deleting' | 'deleted' | 'damaged';
  extension?: string;
  etag?: string;
  scanStatus?: string;
  failureReason?: string;
  selectable: boolean;
  disabledReason?: string;
  metadata?: Record<string, string>;
  urlHints?: Record<string, boolean>;
  reconcileKey?: string;
  createdAt: string;
  updatedAt: string;
}
export interface MediaPage { items: MediaResource[]; total: number; limit: number; offset: number; nextCursor?: string; hasMore: boolean; }
export interface MediaSignedURL { url: string; expiresAt: string; }

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

export type MonitorStatus = 'ok' | 'degraded' | 'unavailable';
export type MonitorMetricScope = 'process' | 'container' | 'host';
export interface MonitorCapability {
  scope: MonitorMetricScope;
  available: boolean;
  source?: string;
}
export type MonitorCapabilities = Record<string, MonitorCapability>;
export interface MonitorHostMetric {
  status: MonitorStatus;
  cores?: number;
  load1?: number;
  load5?: number;
  load15?: number;
  loadPerCore?: number;
  perCoreLoad?: number[];
  rssBytes?: number;
  usedBytes?: number;
  freeBytes?: number;
  totalBytes?: number;
  utilization?: number;
  capabilities: MonitorCapabilities;
  message?: string;
}
export interface MonitorRuntimeMetric {
  status: MonitorStatus;
  goVersion: string;
  os: string;
  arch: string;
  compiler: string;
  applicationVersion?: string;
  commit?: string;
  heapAllocBytes?: number;
  heapSysBytes?: number;
  heapInUseBytes?: number;
  heapObjects?: number;
  nextGcBytes?: number;
  gcCount?: number;
  lastGcPauseNs?: number;
  capabilities: MonitorCapabilities;
}
export interface MonitorDatabasePool {
  open: number;
  inUse: number;
  active: number;
  idle: number;
  max: number;
  waitCount: number;
  waitDurationMs: number;
  maxIdleClosed: number;
  maxIdleTimeClosed: number;
  maxLifetimeClosed: number;
}
export interface MonitorRedisPool {
  max?: number;
  total: number;
  active: number;
  idle: number;
  hits: number;
  misses: number;
  timeouts: number;
  waitCount: number;
  waitDurationMs: number;
  stale: number;
  pending: number;
}
export interface MonitorDatabaseMetric {
  status: MonitorStatus;
  latencyMs: number;
  driver?: 'mysql' | 'postgres';
  mode?: 'single' | 'read_write' | 'cluster_endpoint';
  pool?: MonitorDatabasePool;
  capabilities: MonitorCapabilities;
  message?: string;
}
export interface MonitorRedisMetric {
  status: MonitorStatus;
  latencyMs: number;
  mode?: 'single' | 'sentinel' | 'cluster';
  pool?: MonitorRedisPool;
  keyspace?: number;
  capabilities: MonitorCapabilities;
  message?: string;
}
export interface MonitorOverview {
  status: MonitorStatus;
  scope: MonitorMetricScope;
  uptimeSeconds: number;
  version?: string;
  runtime: MonitorRuntimeMetric;
  cpu: MonitorHostMetric;
  memory: MonitorHostMetric;
  disk: MonitorHostMetric;
  database: MonitorDatabaseMetric;
  redis: MonitorRedisMetric;
  goroutines: MonitorGoroutineMetric;
  backgroundTasks: MonitorBackgroundTaskMetric;
  collectedAt: string;
  timestamp: string;
  refreshIntervalSeconds: number;
  refreshIntervalMs: number;
  dataSource: string;
  isSynthetic: boolean;
}
export interface MonitorGoroutineMetric {
  status: MonitorStatus;
  count?: number;
  capabilities: MonitorCapabilities;
}
export interface MonitorBackgroundTaskMetric {
  status: MonitorStatus;
  queued?: number;
  active?: number;
  scheduled?: number;
  failed?: number;
  capabilities: MonitorCapabilities;
  message?: string;
}
export type MonitorServerStatus = MonitorOverview;

export type DashboardStatus = MonitorStatus;
export interface DashboardCountMetric {
  status: DashboardStatus;
  value?: number;
  message?: string;
}
export interface DashboardCounts {
  users: DashboardCountMetric;
  roles: DashboardCountMetric;
  tasks: DashboardCountMetric;
  importJobs: DashboardCountMetric;
  exportJobs: DashboardCountMetric;
  files: DashboardCountMetric;
  auditEvents: DashboardCountMetric;
  mailAccounts: DashboardCountMetric;
  mailMessages: DashboardCountMetric;
}
export interface DashboardInstanceMetric {
  status: DashboardStatus;
  state?: MonitorStatus;
  scope?: MonitorMetricScope;
  version?: string;
  uptimeSeconds?: number;
}
export interface DashboardHealthMetric {
  status: DashboardStatus;
  state?: MonitorStatus;
}
export interface DashboardHealth {
  runtime: DashboardHealthMetric;
  database: DashboardHealthMetric;
  redis: DashboardHealthMetric;
}
export interface DashboardSummary {
  status: DashboardStatus;
  counts: DashboardCounts;
  instance: DashboardInstanceMetric;
  health: DashboardHealth;
  collectedAt: string;
}
export type DashboardOverviewPreset = 'today' | 'yesterday' | '24h' | '7d' | '14d' | '30d' | 'this_month' | 'last_month' | 'custom';
export type DashboardOverviewGranularity = 'hour' | 'day';
export type DashboardOverviewDataSource = 'live' | 'fixture';
export interface DashboardOverviewTimeRange {
  preset: DashboardOverviewPreset;
  timezone: string;
  from: string;
  to: string;
  granularity: DashboardOverviewGranularity;
}
export interface DashboardOverviewNumberMetric {
  status: DashboardStatus;
  value?: number;
  message?: string;
}
export interface DashboardOverviewCards {
  visitors: DashboardOverviewNumberMetric;
  newUsers: DashboardOverviewNumberMetric;
  paymentAmount: DashboardOverviewNumberMetric;
  paymentOrders: DashboardOverviewNumberMetric;
  averageOrderValue: DashboardOverviewNumberMetric;
}
export interface DashboardOverviewTrendPoint {
  at: string;
  visitors: number;
  newUsers: number;
  orders: number;
  amount: number;
}
export interface DashboardOverviewDistributionItem {
  key: string;
  label: string;
  value: number;
}
export interface DashboardOverviewTopItem {
  rank: number;
  id: string;
  name: string;
  value: number;
  amount: number;
}
export interface DashboardOverviewAnnouncement {
  id: string;
  title: string;
  publishedAt: string;
}
export interface DashboardOverview {
  dataSource: DashboardOverviewDataSource;
  isSynthetic: boolean;
  range: DashboardOverviewTimeRange;
  cards: DashboardOverviewCards;
  trends: DashboardOverviewTrendPoint[];
  distribution: DashboardOverviewDistributionItem[];
  topItems: DashboardOverviewTopItem[];
  regions: DashboardOverviewDistributionItem[];
  announcements: DashboardOverviewAnnouncement[];
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
