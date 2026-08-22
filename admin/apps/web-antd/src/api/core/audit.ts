import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export interface AuditEvent {
  action: string;
  actorId?: string;
  category: AuditCategory;
  createdAt: string;
  details?: Record<string, unknown>;
  id: string;
  outcome: string;
  requestId: string;
  resource: string;
}

export type AuditCategory = 'login' | 'operation' | 'system';
export type AuditExportFormat = 'csv' | 'json';

export interface AuditPage {
  items: AuditEvent[];
  limit: number;
  offset: number;
  total: number;
}

export interface AuditQuery {
  action?: string;
  actorId?: string;
  category?: AuditCategory;
  from?: string;
  limit?: number;
  offset?: number;
  outcome?: string;
  requestId?: string;
  resource?: string;
  to?: string;
}

export interface AuditRetentionDryRun {
  cutoff: string;
  matchingCount: number;
  retentionDays: number;
}

export async function queryAuditEventsApi(params?: AuditQuery) {
  return requestClient.get<AuditPage>(ADMIN_ENDPOINTS.queryAuditEvents, {
    params,
  });
}

export async function exportAuditEventsApi(
  params: AuditQuery | undefined,
  format: AuditExportFormat,
) {
  return requestClient.download<Blob>(ADMIN_ENDPOINTS.exportAuditEvents, {
    params: { ...params, format },
    responseReturn: 'body',
  });
}

export async function retentionDryRunApi(days = 180) {
  return requestClient.get<AuditRetentionDryRun>(
    ADMIN_ENDPOINTS.auditRetentionDryRun,
    { params: { days } },
  );
}
