import type { MonitorServerStatus } from '@vben/api-client';

import { ADMIN_ENDPOINTS } from '@vben/api-client';

import { requestClient } from '#/api/request';

export type { MonitorOverview, MonitorServerStatus } from '@vben/api-client';

export function getServerStatusApi() {
  return requestClient.get<MonitorServerStatus>(
    ADMIN_ENDPOINTS.getServerStatus,
  );
}

export function getMonitorOverviewApi() {
  return getServerStatusApi();
}

export const refreshMonitorOverviewApi = getServerStatusApi;
