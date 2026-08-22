import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

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

export function getMonitorOverviewApi() {
  return requestClient.get<MonitorOverview>(ADMIN_ENDPOINTS.getMonitorOverview);
}

export const refreshMonitorOverviewApi = getMonitorOverviewApi;
