#!/usr/bin/env node
import { access, mkdir, readFile, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const files = [
  'contracts/openapi/admin-v1.yaml',
  'contracts/openapi/client-v1.yaml',
  'contracts/openapi/install-v1.yaml',
];
for (const file of files) {
  try {
    await access(path.join(root, file), constants.F_OK);
  } catch {
    console.error(`OPENAPI_SOURCE_MISSING=${file}`);
    process.exit(1);
  }
}

const adminContractPath = path.join(root, 'contracts/openapi/admin-v1.yaml');
const adminClientPath = path.join(root, 'admin/packages/api-client/src/generated/admin-v1.ts');
const adminContract = await readFile(adminContractPath, 'utf8');
const contractHash = createHash('sha256').update(adminContract).digest('hex');
const operations = [];
let currentPath = null;
for (const line of adminContract.split(/\r?\n/)) {
  const pathMatch = line.match(/^  (\/[^:]+):$/);
  if (pathMatch) {
    currentPath = pathMatch[1];
    continue;
  }
  const operationMatch = line.match(/^      operationId:\s*(\S+)/);
  if (operationMatch && currentPath?.startsWith('/api/admin/v1/')) {
    operations.push({ id: operationMatch[1], path: currentPath.replace(/^\/api/, '') });
  }
}
if (operations.length === 0) {
  console.error('OPENAPI_ADMIN_OPERATIONS_MISSING');
  process.exit(1);
}

const endpointLines = operations
  .map(({ id, path: endpoint }) => `  ${id}: '${endpoint}',`)
  .join('\n');
const generated = `// Generated from contracts/openapi/admin-v1.yaml; DO NOT EDIT.\n// CONTRACT_SHA256=${contractHash}\n\nexport const ADMIN_API_PREFIX = '/admin/v1' as const;\n\nexport const ADMIN_ENDPOINTS = {\n${endpointLines}\n} as const;\n\nexport const AUTH_API_PREFIX = '/admin/v1/auth' as const;\nexport const AUTH_ENDPOINTS = {\n  captcha: ADMIN_ENDPOINTS.issueAdminAuthCaptcha,\n  codes: ADMIN_ENDPOINTS.getAdminAuthAccessCodes,\n  login: ADMIN_ENDPOINTS.adminAuthLogin,\n  logout: ADMIN_ENDPOINTS.adminAuthLogout,\n  passwordReset: ADMIN_ENDPOINTS.resetAdminPassword,\n  passwordResetRequest: ADMIN_ENDPOINTS.requestAdminPasswordReset,\n  register: ADMIN_ENDPOINTS.adminAuthRegister,\n  refresh: ADMIN_ENDPOINTS.adminAuthRefresh,\n  sessions: ADMIN_ENDPOINTS.listAdminAuthSessions,\n} as const;\n\nexport const MENU_ENDPOINT = ADMIN_ENDPOINTS.listVisibleMenus;\nexport const CURRENT_USER_ENDPOINT = ADMIN_ENDPOINTS.getCurrentAdminUser;

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
  categoryId?: string;
  createdAt: string;
  sha256: string;
}
export interface FilePage { items: FileObject[]; total: number; limit: number; offset: number; }

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

export namespace AuthApi {\n  export interface LoginParams {\n    captcha?: string;\n    captchaId?: string;\n    identifier?: string;\n    identifierType?: "username" | "email";\n    password: string;\n    username?: string;\n  }\n\n  export interface RegisterParams {\n    password: string;\n    username: string;\n  }\n\n  export interface PasswordResetRequestParams {\n    password?: string;\n    token?: string;\n    username?: string;\n  }\n\n  export interface SessionInfo {\n    createdAt: string;\n    deviceId: string;\n    deviceName: string;\n    expiresAt: string;\n    id: string;\n    ipAddress: string;\n    lastSeenAt: string;\n    revoked: boolean;\n    userAgent: string;\n  }\n\n  export interface LoginResult {\n    accessToken: string;\n    expiresIn: number;\n    tokenType: 'Bearer';\n  }\n\n  export type RefreshTokenResult = LoginResult;\n\n  export interface ApiEnvelope<T> {\n    code: number;\n    data: T;\n    message: string;\n    meta?: { requestId?: string };\n    traceId?: string;\n  }\n\n  export interface WireTokenData {\n    accessToken?: string;\n    access_token?: string;\n    expiresIn?: number;\n    expires_in?: number;\n    tokenType?: 'Bearer' | string;\n    token_type?: 'Bearer' | string;\n  }\n}\n`;

const checkOnly = process.argv.includes('--check');
let current = null;
try {
  current = await readFile(adminClientPath, 'utf8');
} catch {
  current = null;
}
if (checkOnly) {
  if (current !== generated) {
    console.error(`OPENAPI_CLIENT_OUT_OF_DATE=${path.relative(root, adminClientPath)}`);
    process.exit(1);
  }
  console.log(`OPENAPI_CLIENT_CHECK_OK=${path.relative(root, adminClientPath)}`);
} else {
  await mkdir(path.dirname(adminClientPath), { recursive: true });
  await writeFile(adminClientPath, generated, 'utf8');
  console.log(`OPENAPI_CLIENT_GENERATE_OK=${path.relative(root, adminClientPath)}`);
}
console.log(`OPENAPI_SOURCES_OK=${files.length}`);
console.log('OPENAPI_GENERATION_MODE=standard');
console.log('OPENAPI_GENERATE_OK');
