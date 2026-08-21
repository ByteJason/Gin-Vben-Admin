import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export interface AuditEvent {
  action: string;
  actorId?: string;
  createdAt: string;
  details?: Record<string, unknown>;
  id: string;
  outcome: string;
  requestId: string;
  resource: string;
}

export interface AuditPage {
  items: AuditEvent[];
  limit: number;
  offset: number;
  total: number;
}

export interface AuditQuery {
  action?: string;
  actorId?: string;
  from?: string;
  limit?: number;
  offset?: number;
  outcome?: string;
  requestId?: string;
  resource?: string;
  to?: string;
}

export async function queryAuditEventsApi(params?: AuditQuery) {
  return requestClient.get<AuditPage>(ADMIN_ENDPOINTS.queryAuditEvents, {
    params,
  });
}
